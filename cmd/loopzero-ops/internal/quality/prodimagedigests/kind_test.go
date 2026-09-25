// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package prodimagedigests

import (
	"strings"
	"testing"
)

// The gate reports two populations with different owners and different
// feasibility, and for three months one boolean decided for both at once — which
// is why nobody could say which of them was keeping it off.
//
// MEET-ASYNC-AMENDMENT-2026-08-17-digest-enforcement-has-two-populations.

func TestLiteralTagIsEnforcedByDefault(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopzero-api/templates/sidecar.yaml": "    image: hashicorp/vault:1.18.0\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a literal :tag in a published chart template must FAIL by default — it is fixable today, by the person who wrote the chart")
	}
	if len(out.Violations) != 1 || out.Violations[0].Kind != KindLiteralTag {
		t.Fatalf("want one literal_tag violation; got %+v", out.Violations)
	}
}

func TestMissingDigestOnlyWarnsByDefault(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopzero-api/templates/deployment.yaml": "    image: {{ include \"loopzero-common.image\" . }}\n",
		"charts/loopzero-api/values.yaml":               "image:\n  repository: registry/loopzero-api\n  tag: dev\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 1 || out.Violations[0].Kind != KindMissingDigest {
		t.Fatalf("want one missing_digest violation; got %+v", out.Violations)
	}
	if !out.Pass {
		t.Fatal("missing_digest must NOT fail by default: the value is produced by the image publisher, and a hand-pinned digest becomes a lie at the next publish — the gate's own message says so")
	}
}

// The escape from warn-mode still exists; it just stopped being the only setting.
func TestEnforceAllAlsoFailsOnMissingDigest(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopzero-api/templates/deployment.yaml": "    image: {{ include \"loopzero-common.image\" . }}\n",
		"charts/loopzero-api/values.yaml":               "image:\n  repository: registry/loopzero-api\n  tag: dev\n",
	})
	out, err := Check(Config{Root: root, Enforce: EnforceAll()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("with every kind enforced, a missing digest must fail")
	}
}

// The separation is only worth anything if a caller can act on it — one number
// covering both populations is what made three fixable findings invisible behind
// seven that nobody could fix.
func TestEnforcedAndWarnedAreSeparable(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopzero-api/templates/deployment.yaml": "    image: {{ include \"loopzero-common.image\" . }}\n",
		"charts/loopzero-api/templates/test-pod.yaml":   "    image: busybox:1.36\n",
		"charts/loopzero-api/values.yaml":               "image:\n  repository: registry/loopzero-api\n  tag: dev\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got := len(out.Enforced()); got != 1 {
		t.Fatalf("want 1 enforced violation; got %d (%+v)", got, out.Enforced())
	}
	if got := len(out.Warned()); got != 1 {
		t.Fatalf("want 1 warned violation; got %d (%+v)", got, out.Warned())
	}
	if out.Pass {
		t.Fatal("an enforced violation fails the gate even when a warned one is present")
	}
}

// A digest reference is the fix, not an exemption — asserting it keeps the fix
// from regressing into `:tag` on the next edit.
func TestDigestReferenceIsNotAViolation(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopzero-api/templates/test-pod.yaml": "    image: busybox@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 0 {
		t.Fatalf("a digest reference is the fix; got %+v", out.Violations)
	}
}

// The rendering the operator reads must survive the type change. It is the only
// output of this gate, and a struct that prints as `{literal_tag foo 3 bar}`
// would make the gate useless while every test stayed green.
func TestViolationRendersFileLineAndText(t *testing.T) {
	v := Violation{Kind: KindLiteralTag, File: "charts/x/templates/a.yaml", Line: 20, Text: "image: busybox:1.36"}
	got := v.String()
	for _, want := range []string{"charts/x/templates/a.yaml", "20", "image: busybox:1.36"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendering must carry %q; got %q", want, got)
		}
	}

	fileLevel := Violation{Kind: KindMissingDigest, File: "charts/x/values.yaml", Text: "image.digest missing"}
	if got := fileLevel.String(); strings.Contains(got, ":0:") {
		t.Errorf("a file-level violation has no line number and must not print one; got %q", got)
	}
}

// The population the gate exists for (B-109). A synthetic mutant proves the
// detector fires; only this proves the repository is actually clean of the kind
// that is now enforced — which is what makes the flip safe rather than hopeful.
func TestRealTreeHasNoEnforcedViolations(t *testing.T) {
	out, err := Check(Config{Root: repoRoot})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.ScannedFiles == 0 {
		t.Fatal("zero files scanned — finding nothing is not the same as finding nothing wrong")
	}
	if enforced := out.Enforced(); len(enforced) > 0 {
		for _, v := range enforced {
			t.Errorf("%s", v)
		}
		t.Fatalf("%d enforced violation(s) in the real tree", len(enforced))
	}
}
