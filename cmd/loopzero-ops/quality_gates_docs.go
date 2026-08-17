// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// quality_gates_docs.go holds the Run entry points for gates whose subject is
// the repository's own normative documentation — the contracts and decision
// records that code and CLAUDE.md tell a reader to consult.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/docsrefs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/ecosystemvalidators"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/envprefixparity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/publishhygiene"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skillscriptpaths"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skillsuites"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testtargetcoverage"
)

// runSkillScriptPaths refuses a SKILL.md that tells an agent to run a script by a path
// which resolves in only one of the two ecosystem layouts (`<project>/.claude/` for a
// plugin install, `<repo>/` for the standalone kit). B-120.
// runSkillSuites runs the ecosystem's own pytest suites, which no task ran before B-124 —
// three of fifteen were red, and the defect two of them caught had already shipped.
func runSkillSuites(args []string) int {
	fs := flag.NewFlagSet("skill-suites", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	skills := fs.String("skills", "", "skills dir relative to root (default: .claude/skills)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := skillsuites.Check(skillsuites.Config{RepoRoot: *root, SkillsRel: *skills})
	switch {
	case out.Advisory:
		fmt.Println("skill-suites: ADVISORY — python3/pytest absent; nothing was concluded")
		return 0
	case errors.Is(err, skillsuites.ErrNoSuitesFound):
		fmt.Fprintf(os.Stderr, "skill-suites: FAIL — %v\n", err)
		return 1
	case errors.Is(err, skillsuites.ErrSuitesFailed):
		failed := out.Failed()
		fmt.Fprintf(os.Stderr, "skill-suites: FAIL — %d of %d suite(s) red:\n", len(failed), len(out.Results))
		for _, r := range failed {
			fmt.Fprintf(os.Stderr, "  %s\n      %s\n", r.Skill, r.Tail)
		}
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "skill-suites: FAIL — %v\n", err)
		return 1
	}
	fmt.Printf("skill-suites: PASS — %d skill suite(s) green\n", len(out.Results))
	return 0
}

func runSkillScriptPaths(args []string) int {
	fs := flag.NewFlagSet("skill-script-paths", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	skills := fs.String("skills", "", "skills dir relative to root (default: .claude/skills)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := skillscriptpaths.Check(skillscriptpaths.Config{RepoRoot: *root, SkillsRel: *skills})
	switch {
	case errors.Is(err, skillscriptpaths.ErrSkillsRootMissing):
		fmt.Fprintf(os.Stderr, "skill-script-paths: FAIL — %v\n", err)
		return 1
	case errors.Is(err, skillscriptpaths.ErrUnreachableInvocation):
		fmt.Fprintf(os.Stderr, "skill-script-paths: FAIL — %d invocation(s) resolve in only one layout:\n", len(out.Findings))
		for _, f := range out.Findings {
			fmt.Fprintf(os.Stderr, "  %s:%d  %s\n      %s\n", f.File, f.Line, f.Path, f.Text)
		}
		fmt.Fprintln(os.Stderr, "\nUse a prefix that resolves in both:\n"+
			"  ECO=$([ -d .claude/skills ] && echo .claude || echo .)\n"+
			"  python3 \"$ECO/skills/<skill>/scripts/<script>.py\"")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "skill-script-paths: FAIL — %v\n", err)
		return 1
	}
	fmt.Printf("skill-script-paths: PASS — %d SKILL.md across %d skill(s); every script invocation resolves in both layouts\n",
		out.FilesScanned, out.SkillsScanned)
	return 0
}

func runDocsContractRefs(args []string) int {
	fs := flag.NewFlagSet("docs-contract-refs", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", "", "path to the allowlist (default: <root>/"+docsrefs.DefaultAllowlistRelPath+")")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := docsrefs.Check(docsrefs.Config{RepoRoot: *root, AllowlistPath: *allowlist})
	switch {
	case errors.Is(err, docsrefs.ErrMalformedAllowlist):
		// A line that reads as an exemption and exempts nothing is the defect
		// this gate exists to remove, so a bad allowlist is louder than a bad
		// reference — never a skipped line.
		fmt.Fprintf(os.Stderr, "docs-contract-refs: FAIL — %v\n", err)
		return 2
	case errors.Is(err, docsrefs.ErrUnresolvedReferences):
		fmt.Fprintf(os.Stderr, "docs-contract-refs: FAIL — %d normative reference(s) do not resolve:\n", len(out.Unresolved))
		for _, r := range out.Unresolved {
			fmt.Fprintf(os.Stderr, "  %s\n", r)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "A reference that does not resolve is worse than no reference: it is followed.")
		fmt.Fprintf(os.Stderr, "Fix: create the document, correct the reference, or add a DATED entry to %s.\n", docsrefs.DefaultAllowlistRelPath)
		fmt.Fprintln(os.Stderr, "Do NOT recreate a missing contract from memory — invented documentation is worse than an absent one.")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "docs-contract-refs: %v\n", err)
		return 2
	}
	fmt.Printf("docs-contract-refs: PASS — %d normative reference(s) resolve (%d allowlisted with a sunset)\n",
		out.Checked, out.Allowlisted)
	return 0
}

func runTestTargetCoverage(args []string) int {
	fs := flag.NewFlagSet("test-target-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := testtargetcoverage.Check(testtargetcoverage.Config{RepoRoot: *root})
	switch {
	case errors.Is(err, testtargetcoverage.ErrModuleUncovered):
		fmt.Fprintf(os.Stderr, "test-target-coverage: FAIL — %d module(s) with no Taskfile target running their tests:\n", len(out.Uncovered))
		for _, m := range out.Uncovered {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Add `cd <module> && go test ./...` to taskfiles/test.yml (GOWORK=off for")
		fmt.Fprintln(os.Stderr, "modules outside go.work), OR add a NAMED exemption with a reason in")
		fmt.Fprintln(os.Stderr, "internal/quality/testtargetcoverage. `go build` does not count: that is")
		fmt.Fprintln(os.Stderr, "exactly how cmd/loopzero-ops looked covered while no test of it ever ran (B-088).")
		return 1
	case errors.Is(err, testtargetcoverage.ErrPackageUncovered):
		fmt.Fprintf(os.Stderr, "test-target-coverage: FAIL — %d pacote(s) com teste que nenhum alvo roda:\n", len(out.UncoveredPackages))
		for _, p := range out.UncoveredPackages {
			fmt.Fprintf(os.Stderr, "  %s\n", p)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "O MÓDULO tem alvo; ele é que não alcança estas subárvores. Alargue o padrão do")
		fmt.Fprintln(os.Stderr, "alvo, dê um alvo próprio a elas, ou declare uma isenção de PACOTE com razão e")
		fmt.Fprintln(os.Stderr, "sunset. Foi exatamente assim que 41 arquivos ficaram invisíveis num módulo que")
		fmt.Fprintln(os.Stderr, "o gate reportava coberto (B-092/B-114).")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "test-target-coverage: %v\n", err)
		return 2
	}
	fmt.Printf("test-target-coverage: PASS — %d módulo(s), %d cobertos, %d isento(s) com razão datada. "+
		"A granularidade é o PACOTE: um módulo com alvo ainda reprova se o padrão dele não alcançar "+
		"uma subárvore que tem teste (B-114).\n",
		len(out.Modules), len(out.Covered), len(out.Exempt))
	return 0
}

func runEnvPrefixParity(args []string) int {
	fs := flag.NewFlagSet("env-prefix-parity", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := envprefixparity.Check(envprefixparity.Config{RepoRoot: *root})
	switch {
	case errors.Is(err, envprefixparity.ErrCaseFoldingInMatch):
		fmt.Fprintln(os.Stderr, "env-prefix-parity: FAIL — a reserved-prefix comparison folds case:")
		fmt.Fprint(os.Stderr, envprefixparity.FormatCaseFindings(out.CaseFindings))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Matching is exact per ADR 2026-08-12-reserved-env-case-matching: env vars are")
		fmt.Fprintln(os.Stderr, "case-sensitive on Linux and every name LoopZero injects is uppercase, so a lowercase")
		fmt.Fprintln(os.Stderr, "key cannot shadow one. Folding rejects builds over a collision the OS forbids.")
		fmt.Fprintln(os.Stderr, "The INSTRUCTION keyword (ARG/ENV) may still be folded — this looks only inside")
		fmt.Fprintln(os.Stderr, "the loop over the prefix slice.")
		return 1
	case errors.Is(err, envprefixparity.ErrPrefixSetsDiverge):
		fmt.Fprintln(os.Stderr, "env-prefix-parity: FAIL — the two reservedEnvKeyPrefixes declarations hold different sets:")
		for _, d := range out.Declarations {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", d.File, d.Prefixes)
		}
		for file, only := range out.OnlyIn {
			fmt.Fprintf(os.Stderr, "  only in %s: %v\n", file, only)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "This gate proves the two PREFIX SETS match, and nothing else. The same rule also")
		fmt.Fprintln(os.Stderr, "diverges on severity (B-069: silent discard vs hard build failure) and on case")
		fmt.Fprintln(os.Stderr, "matching (B-070). Green here does not mean the rule is unified.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "If this divergence is a KNOWN gap, it belongs in")
		fmt.Fprintln(os.Stderr, "cmd/loopzero-ops/internal/quality/envprefixparity/allowlist.txt with a sunset —")
		fmt.Fprintln(os.Stderr, "an entry there may also have EXPIRED, which is why a gap can reappear.")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "env-prefix-parity: %v\n", err)
		return 2
	}
	// An exemption is PRINTED on the green path, not swallowed by it. A gap
	// nobody sees on a passing run is a gap nobody retires — which is how the
	// 21 inherited approvals of helm-drift (B-076) became invisible.
	if len(out.Exempted) > 0 {
		fmt.Printf("env-prefix-parity: PASS — the %d find_sec_004 copies agree on case matching; "+
			"%d prefix(es) exempt with a sunset: %v. This compares the two copies to each other and "+
			"says nothing about whether the rule is reachable (B-094).\n",
			len(out.Declarations), len(out.Exempted), out.Exempted)
		return 0
	}
	fmt.Printf("env-prefix-parity: PASS — the %d find_sec_004 copies hold the same prefix set and "+
		"the same case matching. This compares them to each other and says nothing about whether "+
		"the rule is reachable (B-094).\n", len(out.Declarations))
	return 0
}

// runEcosystemValidators roda os dois validadores que o protocolo de mudança de
// golden rule exige — e que nenhum gate rodava.
//
// `rules/cycle-rule-schema.md § Golden Rule Change Protocol` pede que
// `check_xrefs.py` e `test_e2e_smoke.py` estejam verdes em toda mudança numa regra
// LOCKED. Medido no B-127: nenhuma task, gate ou hook os invocava, e ao serem
// rodados pela primeira vez o `check_xrefs` saiu FAIL com 4 achados do mesmo dia.
func runEcosystemValidators(args []string) int {
	fs := flag.NewFlagSet("ecosystem-validators", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := ecosystemvalidators.Check(ecosystemvalidators.Config{RepoRoot: *root})
	if out.Advisory {
		fmt.Println("ecosystem-validators: ADVISORY — python3 ausente; nada foi concluído")
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ecosystem-validators: FAIL — %v\n", err)
		for _, r := range out.Failed() {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", r.Name, r.Tail)
		}
		return 1
	}
	// FAIL e WARN reportados separados: somá-los faria o aviso apagar a falha, e um
	// validador que sempre avisa ensina a ignorá-lo.
	fmt.Printf("ecosystem-validators: PASS — %d validador(es) verdes, %d aviso(s)\n",
		len(out.Results), out.Warnings())
	return 0
}

// runPublishHygiene recusa arquivo versionado VIVO que carregue caminho de workstation.
//
// Dos 20 repositórios irmãos medidos em 2026-08-17, UM tinha esta trava (o loopzero-rag) —
// e foi por isso que o vazamento do kit apareceu lá. Nos outros 19 ele envelhecia em
// silêncio. Este repositório publica charts OCI e imagens, e história publicada é
// pública RETROATIVAMENTE.
func runPublishHygiene(args []string) int {
	fs := flag.NewFlagSet("publish-hygiene", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := publishhygiene.Check(publishhygiene.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "publish-hygiene: FAIL — %v\n", err)
		for _, f := range out.Findings {
			fmt.Fprintf(os.Stderr, "  %s:%d — %s\n", f.File, f.Line, f.Text)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Troque o caminho absoluto por `~`, `<repo>` ou `<workspace>`, ou declare uma")
		fmt.Fprintln(os.Stderr, "isenção com razão E sunset em internal/quality/publishhygiene. Registro datado")
		fmt.Fprintln(os.Stderr, "(docs/wiki, plans/completed, baselines, CHANGELOG) já é isento por desenho:")
		fmt.Fprintln(os.Stderr, "reescrevê-lo destruiria a evidência do que era verdade então.")
		return 1
	}
	fmt.Printf("publish-hygiene: PASS — %d arquivo(s) vivo(s) sem caminho de workstation, %d isento(s) com razão datada\n",
		out.Scanned, out.Exempt)
	return 0
}
