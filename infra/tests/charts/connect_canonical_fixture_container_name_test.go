// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock for the `connect-canonical-fixture.sh` Step 11 canonical
// fix landed on 2026-06-20 during the dev-public Cleanroom-fresh close.
//
// Symptom (caught LIVE during seed-post-cleanroom-all.sh --verify):
//
//	[connect-fixture] ERROR mint_app_jwt: cannot resolve GITHUB_APP_ID
//	                         from loopzero-api pod running env
//	[connect-fixture] WARN: no GitHub App installation found on
//	                         getloopscale/loopzerokit-example
//
// Root cause: the `kubectl exec deployment/loopzero-api -c <container>` call
// was targeting container name `api`, while the canonical container name
// emitted by `infra/helm/loopzero-api/templates/deployment.yaml` is
// `loopzero-api` (matches the Service + chart name). `-c api` selects no
// matching container, kubectl errors to stderr, the script suppresses
// stderr via `2>/dev/null` AND swallows the non-zero exit via `|| true`,
// so `$app_id` ends up empty. The script then reports "cannot resolve
// GITHUB_APP_ID" — the diagnostic blames missing env var when the
// actual cause is wrong container selector.
//
// The bug is silent because the same `kubectl exec deployment/loopzero-api
// -- ... ` invocation WITHOUT the explicit `-c` would have defaulted to
// the (single) first container and succeeded. Adding an EXPLICIT but
// WRONG `-c` selector is strictly worse than no selector at all.
//
// Why this matters for the dev-public Cleanroom canonical close:
// `connect-canonical-fixture.sh` is step 11 of the 17-step canonical
// chain orchestrated by `seed-post-cleanroom-all.sh`. A bug in step 11
// blocks `--verify` from reaching 0 FAIL, which per
// `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL blocks the
// IMPLEMENTATION_COMPLETE promise.
//
// SCOPE DISCIPLINE — read before extending the guard:
//
//	This guard ONLY asserts the kubectl exec selector used by
//	`mint_app_jwt` to read the loopzero-api running env. It does NOT forbid
//	other containers named `api` from existing in the project (e.g.,
//	`loopzero-cloud` may have a container literally named `api`). The
//	selector is wrong here because the deployment is `loopzero-api`, not
//	`loopzero-cloud`. Extending the guard to forbid `-c api` globally would
//	over-reach.
//
// The audit is encoded as a pure function so the negative-control test
// can mutate the in-memory body and PROVE the guard catches the revert.
// A guard that passes the negative control is theatre.
//
// Cross-references:
//   - Script: infra/scripts/local/connect-canonical-fixture.sh § mint_app_jwt
//   - Chart canonical container name: infra/helm/loopzero-api/templates/deployment.yaml
//   - Mandate: .claude/rules/cleanroom-up-live-test-mandate.md INQUEBRÁVEL
//   - Sister F1 guard pattern: vault_bootstrap_san_aligned_host_test.go
//   - Seed orchestrator: infra/scripts/local/seed-post-cleanroom-all.sh § Step 11
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Regex inventory — raw strings so on-file escape sequences map cleanly
// to on-disk bytes. The canonical Step 11 line shipped 2026-06-20 is:
//
//	app_id="$(kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" exec deployment/loopzero-api -c loopzero-api -- \
//	  sh -c 'echo "$GITHUB_APP_ID"' 2>/dev/null | tr -d '\r\n' || true)"
var (
	reKubectlExecDeployLoopZeroApi = regexp.MustCompile(`kubectl --context="\$CONTEXT" -n "\$PG_NAMESPACE" exec deployment/loopzero-api -c loopzero-api --`)
	reMintAppJwtFunc           = regexp.MustCompile(`^mint_app_jwt\(\) \{`)
	reWrongContainerSelector   = regexp.MustCompile(`exec deployment/loopzero-api -c api --`)
)

func loadConnectCanonicalFixture(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	p := filepath.Join(root, "infra/scripts/local/connect-canonical-fixture.sh")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// auditConnectFixtureContainerName encodes the Step 11 regression-lock.
// Returns failure messages (empty slice => guard passes). Pulling the
// assertions into a pure function lets the negative-control test mutate
// the body and prove the guard would catch the regression.
func auditConnectFixtureContainerName(body string) []string {
	var errs []string

	// (a) mint_app_jwt() function MUST exist — if the function is renamed
	//     or removed, this guard cannot meaningfully assert anything about
	//     it, so flag explicitly rather than silently passing.
	if !reMintAppJwtFunc.MatchString(body) && !strings.Contains(body, "mint_app_jwt()") {
		errs = append(errs, "(a) script does NOT define `mint_app_jwt()` — the function this guard locks the container selector for is missing; either it was renamed (update the guard) or removed (re-evaluate Step 11 chain).")
	}

	// (b) The kubectl exec invocation against deployment/loopzero-api MUST use
	//     `-c loopzero-api` as the container selector. The canonical container
	//     name comes from `infra/helm/loopzero-api/templates/deployment.yaml`
	//     and matches the chart name + Service name.
	if !reKubectlExecDeployLoopZeroApi.MatchString(body) {
		errs = append(errs, "(b) script does NOT contain `kubectl --context=\"$CONTEXT\" -n \"$PG_NAMESPACE\" exec deployment/loopzero-api -c loopzero-api --` — the canonical Step 11 container selector for reading GITHUB_APP_ID from the loopzero-api pod running env. A wrong selector (e.g., `-c api`) silently empties $app_id and re-introduces the 2026-06-20 ERROR `cannot resolve GITHUB_APP_ID`.")
	}

	// (c) The wrong selector `-c api` MUST NOT appear in the script body —
	//     this is the exact bug shape the 2026-06-20 fix corrected.
	if reWrongContainerSelector.MatchString(body) {
		errs = append(errs, "(c) script CONTAINS `exec deployment/loopzero-api -c api --` — the bug shape from 2026-06-20. The canonical container name is `loopzero-api` (see infra/helm/loopzero-api/templates/deployment.yaml). `-c api` selects no container, kubectl errors to stderr, the script suppresses stderr via `2>/dev/null` and swallows the non-zero exit via `|| true`, so `$app_id` ends up empty. Result: ERROR `cannot resolve GITHUB_APP_ID` even though the env var IS present in the pod.")
	}

	// (d) Audit-trail comment cites the 2026-06-20 Cleanroom-fresh canonical
	//     close + the canonical container name reasoning. Operators reading
	//     this in 6 months must be able to find the rationale without
	//     spelunking commits.
	hasDate := strings.Contains(body, "2026-06-20") || strings.Contains(body, "2026-06-16")
	hasCanonicalName := strings.Contains(body, "loopzero-api") &&
		(strings.Contains(body, "canonical container") ||
			strings.Contains(body, "Canonical 2026-06-") ||
			strings.Contains(body, "container name"))
	if !hasDate {
		errs = append(errs, "(d) audit-trail comment does NOT cite a 2026-06-{16,20} date — operators reading this in 6 months won't know when/why the canonical container selector was locked.")
	}
	if !hasCanonicalName {
		errs = append(errs, "(d) audit-trail comment does NOT reference the canonical `loopzero-api` container name reasoning — operators reading this in 6 months won't know why `-c loopzero-api` (and not `-c api`) is the right selector.")
	}

	return errs
}

// TestConnectFixtureContainerName runs the Step 11 regression-lock against
// the canonical connect-canonical-fixture.sh on disk. Expected verdict:
// GREEN (no failure messages) after the 2026-06-20 canonical fix.
func TestConnectFixtureContainerName(t *testing.T) {
	body := loadConnectCanonicalFixture(t)
	if errs := auditConnectFixtureContainerName(body); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}
}

// TestConnectFixtureContainerName_NegativeControl proves the guard would
// catch the Step 11 regression. It mutates the in-memory script body to
// revert the container selector from `-c loopzero-api` back to `-c api`
// (the pre-fix bug shape) and asserts auditConnectFixtureContainerName()
// reports at least one error. A guard that passes the negative control
// is theatre.
//
// The mutation is purely in-memory; the on-disk script is NEVER touched.
func TestConnectFixtureContainerName_NegativeControl(t *testing.T) {
	body := loadConnectCanonicalFixture(t)
	mutated := reKubectlExecDeployLoopZeroApi.ReplaceAllString(
		body,
		`kubectl --context="$CONTEXT" -n "$PG_NAMESPACE" exec deployment/loopzero-api -c api --`,
	)
	if mutated == body {
		t.Fatalf("negative-control setup failed: could not find the canonical `exec deployment/loopzero-api -c loopzero-api --` invocation in script body — either the script changed shape or the regex drifted. The GREEN test is vacuous.")
	}
	errs := auditConnectFixtureContainerName(mutated)
	if len(errs) == 0 {
		t.Errorf("negative control: auditConnectFixtureContainerName() PASSED against a script whose kubectl exec selector was reverted to `-c api` — the guard would NOT catch the 2026-06-20 regression (theatre).")
	}
	for _, e := range errs {
		t.Logf("negative-control verdict (expected to fire): %s", e)
	}
}
