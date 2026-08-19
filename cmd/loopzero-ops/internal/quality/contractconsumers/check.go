// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package contractconsumers provides the `contract-consumers` quality gate.
//
// Detects when a backend response shape NARROWS — a Go struct field with a
// `json:"<tag>"` annotation is REMOVED in the PR diff — AND at least one
// consumer in the dashboard / CLI / API still references that field. Forces
// the contract-shift to be visible at PR review time instead of surfacing as
// a silent UX regression during sponsor LIVE walkthrough.
//
// Scope: Trivial-tier MVP. Covers the regression shape observed in the
// 2026-06-08 dashboard-screens-review-fixes T5.3 walkthrough (3 of 5
// LIVE-only bugs were of this exact class: T2.1 SQL refactor dropped legacy
// fields from the listing response, and the detail page + list page
// silently inherited the regression because their consumers still read
// those fields).
//
// What this gate detects:
//   - Removed `<Field> <Type> `json:"<tag>"“ lines in tracked Go files under:
//     api/internal/domain/types.go
//     api/internal/routes/*.go
//     api/internal/db/generated/*.go
//   - For each removed field, greps consumers in:
//     cloud/dashboard/src/  (TS/TSX)
//     cli/src/              (TS)
//     api/internal/         (Go)
//     for `.<tag>` references (e.g., `p.apps_count`, `b.apps`,
//     `project.last_deploy`).
//   - Findings reported with file:line + the dropped field's JSON tag.
//
// What this gate does NOT detect (out-of-scope, by design):
//   - CSS cascade / visual regressions (needs headless browser; sponsor walkthrough)
//   - Empty-state UX honesty (needs cross-data-state semantic check)
//   - SQL SELECT-list drops without corresponding Go struct change (the
//     sqlc-sync gate covers this indirectly via generated/* diff)
//
// Plan ref: post-2026-06-08 dashboard-screens-review-fixes T5.3 walkthrough
// closure. Sponsor verbatim mandate "Sponsor walkthrough manual continua
// sendo o gate final" — this gate REDUCES the bug rate that reaches sponsor,
// NOT replaces the sponsor as the final visual gate.
package contractconsumers

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Config controls the gate behavior.
type Config struct {
	// RepoRoot is the repository root. Defaults to "." in callers.
	RepoRoot string
	// BaseRef is the git ref the PR branches from (e.g., origin/develop).
	// When empty, defaults to "origin/develop".
	BaseRef string
	// HeadRef is the git ref of the PR HEAD. When empty, defaults to "HEAD".
	HeadRef string
}

// Finding represents one consumer that still reads a dropped field.
type Finding struct {
	// DroppedTag is the JSON tag of the removed struct field (e.g., "apps_count").
	DroppedTag string
	// ConsumerFile is the file where the field is still read.
	ConsumerFile string
	// ConsumerLine is the line number in ConsumerFile.
	ConsumerLine int
	// ConsumerSnippet is the line content with the field reference.
	ConsumerSnippet string
}

// Outcome reports the gate verdict.
type Outcome struct {
	Pass        bool
	Findings    []Finding
	DroppedTags []string // sorted list of dropped JSON tags detected
	ScanRoots   []string // consumer scan roots used
	// MissingRoots sao as raizes declaradas que NAO existem na arvore. `git grep`
	// ignora pathspec que nao casa nada e devolve os matches dos que casam, entao
	// uma raiz que sumiu vira cobertura perdida em silencio (B-100, B-040).
	MissingRoots []string
	CommandError string // populated if git or grep failed
}

// trackedFiles enumerates source files whose struct field drops trigger the gate.
// Restricted to the backend API contract surface — see package doc.
var trackedFiles = []string{
	"api/internal/domain/types.go",
	"api/internal/db/generated/", // any *.go under this dir
	"api/internal/routes/",       // any *.go under this dir
}

// consumerRoots enumerates directories grepped for dropped-field references.
var consumerRoots = []string{
	"cloud/dashboard/src/",
	"cli/src/",
	"api/internal/",
}

// jsonFieldRegex matches `Name Type `json:"tag[,opts]"“ style declarations.
// Captures the JSON tag (group 1) before any comma options like `,omitempty`.
var jsonFieldRegex = regexp.MustCompile("json:\"([^\",]+)")

// Check runs the gate. Config.RepoRoot must point at a git checkout. Returns
// an Outcome describing any consumers found that still read dropped fields.
// A nil outcome is returned only on git command failure (still reported via
// the returned Outcome.CommandError when convertible).
func Check(cfg Config) (*Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	base := cfg.BaseRef
	if base == "" {
		base = "origin/develop"
	}
	head := cfg.HeadRef
	if head == "" {
		head = "HEAD"
	}

	// Calculado ANTES do primeiro retorno: no caminho de erro do git a cobertura
	// tambem some, e e' o caminho em que ninguem olha.
	ausentes := raizesAusentes(root)

	droppedTags, err := collectDroppedJSONTags(root, base, head)
	if err != nil {
		return &Outcome{
			CommandError: err.Error(),
			ScanRoots:    consumerRoots,
			MissingRoots: ausentes,
		}, nil
	}

	out := &Outcome{
		DroppedTags:  droppedTags,
		ScanRoots:    consumerRoots,
		MissingRoots: ausentes,
		Pass:         true,
	}
	// Uma raiz declarada que nao existe e' cobertura perdida, e precisa aparecer
	// mesmo quando nenhuma tag caiu: e' justamente no PASS que ela some sem deixar
	// rastro. Nao derruba o gate sozinha — a decisao de para onde apontar esta' no
	// B-100 — mas deixa de ser invisivel.
	if len(droppedTags) == 0 {
		return out, nil
	}

	for _, tag := range droppedTags {
		findings, err := grepConsumersForTag(root, tag)
		if err != nil {
			out.CommandError = err.Error()
			out.Pass = false
			return out, nil
		}
		out.Findings = append(out.Findings, findings...)
	}

	if len(out.Findings) > 0 {
		out.Pass = false
	}
	return out, nil
}

// collectDroppedJSONTags runs `git diff <base>..<head>` over the tracked
// files and extracts JSON tags from REMOVED lines (lines starting with "-"
// in unified diff) that match `json:"<tag>"`. Returns the unique sorted list.
func collectDroppedJSONTags(root, base, head string) ([]string, error) {
	args := []string{"-C", root, "diff", "--unified=0", base + ".." + head, "--"}
	args = append(args, trackedFiles...)
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff failed: %v (stderr: %s)", err, stderr.String())
	}

	seen := map[string]struct{}{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		// Removed line in unified diff starts with "-" (but skip "---" header).
		if !strings.HasPrefix(line, "-") || strings.HasPrefix(line, "---") {
			continue
		}
		matches := jsonFieldRegex.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}
		tag := strings.TrimSpace(matches[1])
		if tag == "" || tag == "-" {
			continue // empty tag OR explicit-skip `json:"-"`
		}
		seen[tag] = struct{}{}
	}

	// A removed LINE is not a removed TAG. The diff says the line changed; only
	// HEAD says whether the field still exists. Any edit that rewrites the line
	// — gofmt collapsing padding before a trailing comment, a renamed Go field,
	// a reworded comment — puts the tag on the "-" side while the contract is
	// untouched.
	//
	// Measured on the routes split (B-080): `gofmt -w` turned
	//   -\tRepo string `json:"repo"`            // "owner/repo"
	//   +\tRepo string `json:"repo"` // "owner/repo"
	// into a reported drop of `repo`, with nine named "consumers", and blocked
	// a push that changed no contract at all. A gate that blames whitespace for
	// a wire break is a gate people learn to push past.
	tags := make([]string, 0, len(seen))
	for t := range seen {
		antes, err := structsDeclaringTagAt(root, base, t)
		if err != nil {
			return nil, err
		}
		depois, err := structsDeclaringTagAt(root, head, t)
		if err != nil {
			return nil, err
		}
		// Uma declaracao a MENOS e' a queda. Igual ou mais significa que a tag
		// mudou de nome de struct, de arquivo ou de pacote — nada disso mexe no
		// contrato de fio.
		if depois >= antes {
			continue
		}
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags, nil
}

// structsDeclaringTagAt conta QUANTAS structs declaram `json:"<tag>"` nos
// arquivos rastreados, na revisao dada.
//
// A versao anterior perguntava apenas se a tag existia em ALGUMA struct, e isso
// tinha um furo medido: `repositories.go` declarava `json:"repo"` DUAS vezes, e
// tirar de uma ficava invisivel porque a outra respondia "ainda existe" (B-100).
//
// Contar, e nao identificar a struct pelo NOME, e' deliberado. Escopar por nome
// pegaria o furo, mas criaria um falso positivo novo: renomear a struct — ou
// move-la de pacote, que o desenho original protege explicitamente — nao muda o
// contrato de fio e passaria a ser reportado como quebra. Um gate que acusa
// renomeacao e' um gate que as pessoas aprendem a contornar, que e' o custo que
// este proprio item registra. A contagem cai quando uma declaracao some e nao se
// mexe quando uma declaracao muda de nome ou de lugar.
//
// LIMITE QUE FICA, declarado: se um mesmo commit TIRA a tag de uma struct e a
// ACRESCENTA noutra, a contagem nao se move e a troca passa. E' mais estreito que
// o furo anterior e nao tem cura sem informacao de tipo nos dois lados.
func structsDeclaringTagAt(root, rev, tag string) (int, error) {
	pattern := `json:"` + regexp.QuoteMeta(tag) + `[",]`

	args := []string{"-C", root, "grep", "-cE", pattern, rev, "--"}
	args = append(args, trackedFiles...)
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// `git grep -c` exits 1 sem nenhuma ocorrencia, que e' a resposta e nao falha.
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return 0, fmt.Errorf("git grep for %q at %s failed: %v (stderr: %s)", tag, rev, err, stderr.String())
	}

	total := 0
	for _, linha := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if linha == "" {
			continue
		}
		// formato: `<rev>:<arquivo>:<n>`
		idx := strings.LastIndex(linha, ":")
		if idx < 0 {
			continue
		}
		n, cerr := strconv.Atoi(strings.TrimSpace(linha[idx+1:]))
		if cerr != nil {
			continue
		}
		total += n
	}
	return total, nil
}

// raizesAusentes devolve as raizes declaradas que nao existem sob root.
//
// Existe porque `git grep -- a b c` ignora em silencio o pathspec que nao casa
// nada. Duas das tres raizes declaradas sairam deste repositorio na decomposicao
// (`cloud/dashboard/src/` -> ../loopzero-cloud, `cli/src/` -> ../loopzero-cli), e sao
// justamente as de TypeScript — as que carregam o consumidor de FIO, que le' o
// campo pelo nome do JSON e que o compilador nao ve'. O que sobrou e' Go, onde um
// campo removido quebra a compilacao. Medido 2026-08-19.
func raizesAusentes(root string) []string {
	var ausentes []string
	for _, r := range consumerRoots {
		if _, err := os.Stat(filepath.Join(root, r)); err != nil {
			ausentes = append(ausentes, r)
		}
	}
	return ausentes
}

// grepConsumersForTag runs `git grep` for the dropped tag across consumer
// roots, looking for `.tag\b` access patterns (struct field references or
// JS object property access).
func grepConsumersForTag(root, tag string) ([]Finding, error) {
	pattern := `\.` + regexp.QuoteMeta(tag) + `\b`

	args := []string{"-C", root, "grep", "-nE", pattern, "--"}
	args = append(args, consumerRoots...)
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// `git grep` exits 1 when no matches — that's not an error for us.
	_ = cmd.Run()
	if stderr.Len() > 0 {
		stderrStr := strings.TrimSpace(stderr.String())
		// "exit status 1" is implicit no-match. Anything else is a real error.
		if !strings.Contains(stderrStr, "fatal: ambiguous argument") &&
			!strings.Contains(stderrStr, "did not match any") &&
			stderrStr != "" {
			// Pass-through stderr only when it's a true error.
			// (No-match cases produce empty stderr + exit 1.)
			// We keep noise low by NOT failing on benign stderr.
			_ = stderrStr // intentionally tolerant
		}
	}

	var findings []Finding
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line == "" {
			continue
		}
		// git grep format: file:lineno:content
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		fileRel := parts[0]
		var lineno int
		fmt.Sscanf(parts[1], "%d", &lineno)
		snippet := strings.TrimSpace(parts[2])
		// Skip the struct field declaration site itself — `json:"<tag>"`
		// literal IS a match of `.<tag>` after the `:"`. Filter on the
		// json: declaration pattern.
		if strings.Contains(snippet, fmt.Sprintf(`json:"%s"`, tag)) ||
			strings.Contains(snippet, fmt.Sprintf(`json:"%s,`, tag)) {
			continue
		}
		// Skip test files — they reference fields for assertion fixtures, not
		// real consumer reads that would surface as UX bugs.
		if strings.HasSuffix(fileRel, "_test.go") ||
			strings.HasSuffix(fileRel, ".test.ts") ||
			strings.HasSuffix(fileRel, ".test.tsx") ||
			strings.HasSuffix(fileRel, ".spec.ts") ||
			strings.HasSuffix(fileRel, ".spec.tsx") {
			continue
		}
		findings = append(findings, Finding{
			DroppedTag:      tag,
			ConsumerFile:    fileRel,
			ConsumerLine:    lineno,
			ConsumerSnippet: snippet,
		})
	}
	return findings, nil
}

// ActionableFix returns the recommended remediation surface for FAIL output.
func ActionableFix() string {
	return strings.Join([]string{
		"Audit each consumer file listed above. For each:",
		"  (a) Update the consumer to use the new canonical field exposed by the contract change, OR",
		"  (b) Add the dropped field back to the struct (backward-compat additive completion).",
		"If the drop is intentional + consumers were already migrated in the same PR,",
		"verify the dashboard renders correctly via sponsor walkthrough BEFORE merging.",
	}, "\n  ")
}

// FormatFindings renders findings in a stable order for human + audit output.
func FormatFindings(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].DroppedTag != findings[j].DroppedTag {
			return findings[i].DroppedTag < findings[j].DroppedTag
		}
		if findings[i].ConsumerFile != findings[j].ConsumerFile {
			return findings[i].ConsumerFile < findings[j].ConsumerFile
		}
		return findings[i].ConsumerLine < findings[j].ConsumerLine
	})
	var buf strings.Builder
	currentTag := ""
	for _, f := range findings {
		if f.DroppedTag != currentTag {
			fmt.Fprintf(&buf, "\nDropped JSON tag: %q\n", f.DroppedTag)
			currentTag = f.DroppedTag
		}
		fmt.Fprintf(&buf, "  %s:%d  %s\n",
			filepath.ToSlash(f.ConsumerFile), f.ConsumerLine, f.ConsumerSnippet)
	}
	return buf.String()
}
