// Package esooverlay is a hermetic structural test for
// infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl.
//
// Locks the post-T1.2 (issue #111 / Approach A) K8s-auth workload-identity
// contract for the dev-public ESO overlay:
//   - openbaoTransit.enabled defaults to false (safe — anti-foot-shooting)
//   - server retains canonical intra-cluster DNS (preserves cert SAN)
//   - auth.kind=kubernetes (NOT appRole) — workload identity, no static cred
//   - No roleId/secretRef fields (legacy AppRole shape removed)
//   - Traceability: issue #111 / Approach A / EC-1 / EC-2 cited
//
// Migrated from infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh
// (M-Migrate2 continuation 2026-07-07). Hermetic — no kubectl, no helm.
package esooverlay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const overlayPath = "infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl"

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Failures []string
}

// ErrViolations means at least one assertion failed.
var ErrViolations = errors.New("dev-public loopzero-external-secrets overlay contract violated")

var uuidRE = regexp.MustCompile(`"[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}"`)

// Check runs the 18 assertions from the bash guard.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, overlayPath)
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("overlay missing at %s: %v", overlayPath, err)}}, ErrViolations
	}
	text := string(body)

	var failures []string
	assertRE := func(desc, pattern string) {
		if !regexp.MustCompile(pattern).MatchString(text) {
			failures = append(failures, fmt.Sprintf("MISSING: %s (regex `%s`)", desc, pattern))
		}
	}
	forbidRE := func(desc, pattern string) {
		if regexp.MustCompile(pattern).MatchString(text) {
			failures = append(failures, fmt.Sprintf("FORBIDDEN: %s (regex `%s`)", desc, pattern))
		}
	}
	assertContains := func(desc, needle string) {
		if !strings.Contains(text, needle) {
			failures = append(failures, fmt.Sprintf("MISSING: %s (needle `%s`)", desc, needle))
		}
	}
	forbidContains := func(desc, needle string) {
		if strings.Contains(text, needle) {
			failures = append(failures, fmt.Sprintf("FORBIDDEN: %s (needle `%s`)", desc, needle))
		}
	}

	// B: SAFE DEFAULT — enabled false + never enabled true.
	assertRE("B1 openbaoTransit.enabled defaults to false (safe)", `(?m)^  enabled: false`)
	forbidRE("B2 NO line setting enabled: true (anti-foot-shooting)", `(?m)^  enabled: true`)

	// C: canonical service DNS.
	assertContains("C1 server is canonical core-cluster in-cluster DNS", "openbao.secrets-system.svc.cluster.local:8200")
	assertContains(`C2 server is https (NOT http)`, `server: "https://`)

	// D: auth.kind=kubernetes + workload identity fields.
	assertContains(`D1 auth.kind set to kubernetes (workload identity)`, `kind: "kubernetes"`)
	assertRE("D2 auth.kubernetes.role declared", `(?m)^      role: `)
	assertContains(`D3 auth.kubernetes.serviceAccountRef.name declared`, `name: "external-secrets"`)
	assertRE("D4 auth.kubernetes.serviceAccountRef.audiences declared (EC-5 contract)", `(?m)^        audiences:`)

	// E: anti-hardcode guard — legacy AppRole shape removed + caBundle empty.
	forbidRE("E1 NO roleId field (legacy AppRole shape forbidden post-T1.2)", `(?m)^  roleId:`)
	forbidRE("E2 NO secretRef block (legacy AppRole shape forbidden post-T1.2)", `(?m)^  secretRef:`)
	if uuidRE.MatchString(text) {
		failures = append(failures, "FORBIDDEN: E3 fabricated UUID literal hardcoded (regex `\"[a-f0-9]{8}-…-[a-f0-9]{12}\"`)")
	}
	assertRE(`E4 caBundle placeholder is empty string`, `(?m)^  caBundle: ""`)

	// F: documentation traceability.
	assertRE("F1 doc cites issue #111 / Approach A / dev-public-cross-cluster-secrets",
		`(issue #111|Approach A|dev-public-cross-cluster-secrets)`)
	// The needle was `openbao-init-runbook.md`, and `24d05df4e` consolidated that file
	// into `docs/runbooks.md` — the ceremony lives there under `## OpenBao init ceremony`,
	// and `docs/README.md:61` even records the `git show` that recovers the original. A
	// needle that demands a deleted filename asks the overlay to cite what nobody can
	// open; this one demands the path that resolves.
	assertContains("F2 doc cites OpenBao init runbook", "docs/runbooks.md")
	assertContains("F3 doc cites EC-1", "EC-1")
	assertContains("F4 doc cites EC-2", "EC-2")

	// G: anti-patterns.
	forbidRE("G1 no Co-Authored-By trailer in comments", `(?i)Co-Authored-By`)

	// H: cross-link to chart schema.
	assertRE("H1 overlay uses 'openbaoTransit:' top-level key", `(?m)^openbaoTransit:`)

	// I: cross-cluster contract awareness.
	assertContains("I1 doc cites the separate loopzero-secrets cluster topology", "loopzero-secrets")

	// silence unused for future when we add regex-only assertions again
	_ = forbidContains

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}
