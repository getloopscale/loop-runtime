// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unhomedlogic

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/shellscript"
)

// fixtureAllowlist mimics the shell-scripts-allowlist.md table structure.
const fixtureAllowlist = `# Shell Scripts Allowlist

| Script | LOC | Justification |
|---|---:|---|
| .claude/hooks/task-completed.sh | 102 | grandfathered — harness hook |
| infra/scripts/break-glass.sh | 145 | KEEP-OPERATOR — live emergency, D4 operator-time |
| infra/kubeconform-schemas/validate-helmfile-templates.sh | 74 | KEEP-CI-ORCHESTRATOR — pipes two CLIs |
| infra/scripts/quality/coverage-cli.sh | 58 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/scripts/dr/preflight.sh | 175 | grandfathered 2026-05-22 — REVIEW-NEEDED |
| infra/tests/parity/validate-matrix.sh | 304 | grandfathered 2026-05-22 — MIGRATE-GINKGO |
| infra/helmfile/scripts/generate-secrets.sh | 370 | grandfathered 2026-05-22 — MIGRATE-GENERATOR |
| infra/tests/scripts/foo_test.sh | 50 | grandfathered 2026-05-22 — AUTO-DIE |
`

func TestClassify_KeepIsHomed(t *testing.T) {
	cases := []struct {
		path, justif string
		want         Disposition
	}{
		{".claude/hooks/task-completed.sh", "grandfathered — harness hook", Homed},
		{"infra/scripts/break-glass.sh", "KEEP-OPERATOR — live emergency", Homed},
		{"infra/kubeconform-schemas/validate-helmfile-templates.sh", "KEEP-CI-ORCHESTRATOR", Homed},
		// KEEP-CI-WIRED is the marker used by every canonical hermetic guard
		// wired into `task quality:gates` (all `infra/tests/check-*-syntax_test.sh`
		// pairs shipped since 2026-05). It is functionally equivalent to
		// KEEP-CI-ORCHESTRATOR — the script IS the CI gate itself, not
		// reimplementable operational logic. Recognizing it prevents the
		// canonical hermetic pattern from silently ratcheting the baseline
		// on every Red Team closure (baseline drift 121→146 since 2026-06-30
		// which triggered the un-homed-logic gate to block PRs on legitimate
		// CI-wired guards).
		{"infra/tests/helm/ory-kratos-lifespan_test.sh", "KEEP-CI-WIRED — hermetic guard wired in task quality:gates", Homed},
		{"infra/scripts/quality/coverage-cli.sh", "REVIEW-NEEDED", Unhomed},
		{"infra/tests/parity/validate-matrix.sh", "MIGRATE-GINKGO", Unhomed},
		{"infra/helmfile/scripts/generate-secrets.sh", "MIGRATE-GENERATOR", Unhomed},
		{"infra/tests/scripts/foo_test.sh", "AUTO-DIE", Unhomed},
	}
	for _, c := range cases {
		if got := classify(c.path, c.justif); got != c.want {
			t.Errorf("classify(%q, %q) = %v, want %v", c.path, c.justif, got, c.want)
		}
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"infra/scripts/quality/coverage-cli.sh":      "quality",
		"infra/scripts/governance/bias-check.sh":     "governance",
		"infra/scripts/dr/preflight.sh":              "drill",
		"infra/scripts/drill-mint-jwt.sh":            "drill",
		"infra/scripts/chaos/run-full.sh":            "chaos",
		"infra/helmfile/scripts/generate-secrets.sh": "bootstrap",
		"infra/scripts/bootstrap-platform.sh":        "bootstrap",
		"infra/tests/parity/validate-matrix.sh":      "test",
		".claude/hooks/task-completed.sh":            "harness",
	}
	for path, want := range cases {
		if got := domainOf(path); got != want {
			t.Errorf("domainOf(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestCount_UnhomedPerDomain(t *testing.T) {
	c := Count(fixtureAllowlist, nil)
	// Homed: 3 (1 hook + break-glass KEEP-OPERATOR + validate-helmfile KEEP-CI).
	// Unhomed: 5 (coverage-cli quality, dr/preflight drill, validate-matrix test,
	//             generate-secrets bootstrap, foo_test test).
	if c.Total != 5 {
		t.Fatalf("Total un-homed = %d, want 5\nbyDomain=%v", c.Total, c.ByDomain)
	}
	if c.ByDomain["quality"] != 1 || c.ByDomain["drill"] != 1 ||
		c.ByDomain["bootstrap"] != 1 || c.ByDomain["test"] != 2 {
		t.Fatalf("per-domain wrong: %v", c.ByDomain)
	}
}

// Anti-bias content verification: a KEEP-tagged script that embeds a Python
// heredoc is a program masquerading as break-glass → counted un-homed.
func TestCount_SuspectKeep_PythonHeredocIsUnhomed(t *testing.T) {
	al := "| Script | LOC | Justification |\n|---|---:|---|\n" +
		"| infra/scripts/governance/scan-decay.sh | 168 | KEEP-OPERATOR — audit tool |\n" +
		"| infra/scripts/break-glass.sh | 145 | KEEP-OPERATOR — emergency steps |\n"
	bodies := map[string]string{
		"infra/scripts/governance/scan-decay.sh": "#!/usr/bin/env bash\npython3 - <<'PY'\nimport yaml\nPY\n",
		"infra/scripts/break-glass.sh":           "#!/usr/bin/env bash\nkubectl delete pod x\nhelm rollback y\n",
	}
	read := func(p string) (string, bool) { b, ok := bodies[p]; return b, ok }
	c := Count(al, read)
	if c.Total != 1 {
		t.Fatalf("suspect KEEP (python heredoc) must count un-homed; legit break-glass stays homed. Total=%d suspects=%v", c.Total, c.SuspectKeeps)
	}
	if len(c.SuspectKeeps) != 1 || c.SuspectKeeps[0] != "infra/scripts/governance/scan-decay.sh" {
		t.Fatalf("scan-decay must be flagged suspect, break-glass must not: %v", c.SuspectKeeps)
	}
}

func TestCount_LegitKeepNoContent_StaysHomed(t *testing.T) {
	al := "| Script | LOC | Justification |\n|---|---:|---|\n" +
		"| infra/scripts/break-glass.sh | 145 | KEEP-OPERATOR — emergency |\n"
	read := func(p string) (string, bool) { return "#!/usr/bin/env bash\nkubectl get pods\n", true }
	if c := Count(al, read); c.Total != 0 || len(c.SuspectKeeps) != 0 {
		t.Fatalf("legit KEEP (no python) must stay homed: Total=%d suspects=%v", c.Total, c.SuspectKeeps)
	}
}

func TestLedger_EvidenceAndDisposition(t *testing.T) {
	al := "| Script | LOC | Justification |\n|---|---:|---|\n" +
		"| infra/scripts/governance/scan-decay.sh | 168 | REVIEW-NEEDED |\n" +
		"| infra/scripts/dr/preflight.sh | 175 | REVIEW-NEEDED |\n" +
		"| infra/scripts/break-glass.sh | 145 | REVIEW-NEEDED |\n" +
		"| infra/tests/scripts/foo_test.sh | 50 | AUTO-DIE |\n"
	bodies := map[string]string{
		"infra/scripts/governance/scan-decay.sh": "#!/usr/bin/env bash\npython3 - <<'PY'\nPY\n",
		"infra/scripts/dr/preflight.sh":          "#!/usr/bin/env bash\nkubectl get nodes\n",
		"infra/scripts/break-glass.sh":           "#!/usr/bin/env bash\nkubectl delete pod x\n",
		"infra/tests/scripts/foo_test.sh":        "#!/usr/bin/env bash\necho test\n",
	}
	led := Ledger(al, func(p string) (string, bool) { b, ok := bodies[p]; return b, ok })
	byPath := map[string]Entry{}
	for _, e := range led {
		byPath[e.Path] = e
	}
	if d := byPath["infra/scripts/governance/scan-decay.sh"]; !d.HasPython || d.Disposition != shellscript.MigrateGoPureFn {
		t.Errorf("python-wrapper mis-dispositioned: %+v", d)
	}
	if d := byPath["infra/scripts/dr/preflight.sh"]; !d.ShellsLive || d.Disposition != shellscript.MigrateLoopZeroOpsDrill {
		t.Errorf("drill mis-dispositioned: %+v", d)
	}
	if d := byPath["infra/scripts/break-glass.sh"]; !d.ShellsLive || d.Disposition != shellscript.KeepOperator {
		t.Errorf("live-tool mis-dispositioned: %+v", d)
	}
	if d := byPath["infra/tests/scripts/foo_test.sh"]; d.Disposition != shellscript.AutoDie {
		t.Errorf("_test.sh mis-dispositioned: %+v", d)
	}
}

// RED-honest: the gate must FAIL if a PR increases un-homed logic.
func TestRatchet_FailsOnIncrease(t *testing.T) {
	baseline := Counts{Total: 5, ByDomain: map[string]int{"test": 2}}
	current := Counts{Total: 6, ByDomain: map[string]int{"test": 3}}
	ok, regressions := RatchetCheck(current, baseline)
	if ok {
		t.Fatal("ratchet must FAIL when un-homed increases (5→6)")
	}
	if len(regressions) == 0 {
		t.Fatal("must report the regressing domain")
	}
}

func TestRatchet_OKOnDecrease(t *testing.T) {
	baseline := Counts{Total: 5, ByDomain: map[string]int{"test": 2}}
	current := Counts{Total: 3, ByDomain: map[string]int{"test": 1}}
	if ok, _ := RatchetCheck(current, baseline); !ok {
		t.Fatal("ratchet must PASS when un-homed decreases (5→3)")
	}
}

func TestRatchet_OKOnEqual(t *testing.T) {
	b := Counts{Total: 5, ByDomain: map[string]int{"test": 2}}
	if ok, _ := RatchetCheck(b, b); !ok {
		t.Fatal("ratchet must PASS when unchanged")
	}
}

// Anti-bias: per-domain ratchet — total can hold but a domain regressing fails.
func TestRatchet_FailsOnPerDomainIncreaseEvenIfTotalHolds(t *testing.T) {
	baseline := Counts{Total: 5, ByDomain: map[string]int{"test": 2, "drill": 3}}
	current := Counts{Total: 5, ByDomain: map[string]int{"test": 4, "drill": 1}}
	if ok, regressions := RatchetCheck(current, baseline); ok || len(regressions) == 0 {
		t.Fatalf("must FAIL: test domain regressed 2→4 even though total held (ok=%v regs=%v)", ok, regressions)
	}
}
