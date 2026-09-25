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
	case errors.Is(err, skillsuites.ErrSkillsRootMissing):
		return reportKitNotInstalled("skill-suites", err)
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

// reportKitNotInstalled is the one verdict a gate over the installed kit gives
// on a machine that does not have it. `.claude/` is gitignored, so this is every
// fresh clone: the gate verified nothing, and exit 5 keeps that apart from both
// a pass and a violation found in the kit.
func reportKitNotInstalled(gate string, err error) int {
	fmt.Fprintf(os.Stderr, "%s: NOT VERIFIED — %v\n", gate, err)
	fmt.Fprintf(os.Stderr, "  %s checks the installed kit under .claude/, which .gitignore excludes;\n"+
		"  it can only be verified on a machine where the kit is installed.\n", gate)
	return 5
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
		return reportKitNotInstalled("skill-script-paths", err)
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
	// "document(s)", not "SKILL.md": the scan reaches rules/, commands/ and the ecosystem root
	// too, so the count stopped being SKILL.md the moment it was widened. It read 98 where 49
	// SKILL.md exist on disk — the number was right and the noun was not, which reads as
	// double-counting to anyone checking.
	fmt.Printf("skill-script-paths: PASS — %d document(s) across %d skill(s) plus rules/, commands/ and the ecosystem root; every script invocation resolves in both layouts\n",
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
	if len(out.NotVerified) > 0 {
		fmt.Printf("docs-contract-refs: PASS (PARTIAL) — %d normative reference(s) resolve (%d allowlisted with a sunset); "+
			"%d reference(s) into the ecosystem kit NOT verified: the kit is not installed on this checkout\n",
			out.Checked, out.Allowlisted, len(out.NotVerified))
		return 0
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

func runEcosystemValidators(args []string) int {
	fs := flag.NewFlagSet("ecosystem-validators", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := ecosystemvalidators.Check(ecosystemvalidators.Config{RepoRoot: *root})
	if out.Advisory {
		fmt.Printf("ecosystem-validators: ADVISORY — nothing was concluded: %s\n", out.AdvisoryReason)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ecosystem-validators: FAIL — %v\n", err)
		for _, r := range out.Failed() {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", r.Name, r.Tail)
		}
		return 1
	}
	// FAIL and WARN are reported separately: adding them up would let the warning
	// erase the failure, and a validator that always warns teaches people to ignore it.
	fmt.Printf("ecosystem-validators: PASS — %d validator(s) green, %d warning(s)\n",
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
