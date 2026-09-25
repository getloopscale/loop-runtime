// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"errors"
	"strings"
	"testing"
)

// fakeRenderer returns canned manifests per chart directory, so the comparison
// can be pinned without `helm` on the machine running the test.
//
// The gate must not require the toolchain it audits in order to be tested — that
// is the whole reason rendering sits behind an interface (DIP). A test that
// shelled out to helm would fail on a machine without it and teach the reader
// that the failure is environmental.
type fakeRenderer map[string]string

// `sets` ignored: the canned manifests are keyed by chart dir alone, because the
// tests below pin decoding and set arithmetic, neither of which varies with values.
func (f fakeRenderer) Render(chartDir string, _ []string) (string, error) {
	out, ok := f[chartDir]
	if !ok {
		return "", errors.New("fakeRenderer: no manifest for " + chartDir)
	}
	return out, nil
}

const manifestWithHelmTest = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: t-app
---
apiVersion: v1
kind: Pod
metadata:
  name: t-app-test-connection
  annotations:
    helm.sh/hook: test
`

// TestRenderedKinds is the measurement the gate rests on.
func TestRenderedKinds(t *testing.T) {
	t.Run("collects the kinds produced", func(t *testing.T) {
		got, err := renderedKinds(fakeRenderer{"c": "kind: Deployment\n---\nkind: Service\n"}, "c")
		if err != nil {
			t.Fatalf("renderedKinds: %v", err)
		}
		if len(got) != 2 || !got["Deployment"] || !got["Service"] {
			t.Errorf("kinds = %v, want Deployment and Service", got)
		}
	})

	t.Run("excludes what `helm install` does not install", func(t *testing.T) {
		// A Pod annotated `helm.sh/hook: test` only runs under `helm test`. Counting
		// it would report an asymmetry with no effect on what stays running — and,
		// measured on the real charts, it is exactly the difference that showed up.
		got, err := renderedKinds(fakeRenderer{"c": manifestWithHelmTest}, "c")
		if err != nil {
			t.Fatalf("renderedKinds: %v", err)
		}
		if got["Pod"] {
			t.Error("the Helm test Pod was counted as an installed resource")
		}
		if !got["Deployment"] {
			t.Error("the Deployment disappeared with it")
		}
	})

	t.Run("an empty document between separators does not become a kind", func(t *testing.T) {
		// `helm template` emits a stray `---` when a conditional block does not
		// render. A nil in the middle of the stream must not become an entry.
		got, err := renderedKinds(fakeRenderer{"c": "---\n\n---\nkind: Service\n---\n"}, "c")
		if err != nil {
			t.Fatalf("renderedKinds: %v", err)
		}
		if len(got) != 1 || !got["Service"] {
			t.Errorf("kinds = %v, want only Service", got)
		}
	})

	t.Run("a renderer failure propagates instead of becoming an empty set", func(t *testing.T) {
		// A render that fails and a chart that produces nothing are different facts,
		// and flattening the first into the second would make the gate report total
		// drift as if it had been measured.
		if _, err := renderedKinds(fakeRenderer{}, "does-not-exist"); err == nil {
			t.Fatal("the renderer failure was swallowed")
		}
	})
}

// TestDiffKinds pins the comparison itself.
func TestDiffKinds(t *testing.T) {
	pub := map[string]bool{"Deployment": true, "Secret": true, "Service": true}
	boot := map[string]bool{"Deployment": true, "IngressRoute": true, "Service": true}

	soPub, soBoot := diffKinds(pub, boot)
	if strings.Join(soPub, ",") != "Secret" {
		t.Errorf("only in published = %v, want [Secret]", soPub)
	}
	if strings.Join(soBoot, ",") != "IngressRoute" {
		t.Errorf("only in bootstrap = %v, want [IngressRoute]", soBoot)
	}

	t.Run("equal sets produce no difference", func(t *testing.T) {
		a, b := diffKinds(pub, pub)
		if len(a) != 0 || len(b) != 0 {
			t.Errorf("equal sets produced a difference: %v / %v", a, b)
		}
	})

	t.Run("order is deterministic", func(t *testing.T) {
		// Without a stable order the gate's output changes on every run and the
		// diff of two reports becomes noise.
		many := map[string]bool{"Zeta": true, "Alpha": true, "Middle": true}
		a, _ := diffKinds(many, map[string]bool{})
		if strings.Join(a, ",") != "Alpha,Middle,Zeta" {
			t.Errorf("order = %v, want alphabetical", a)
		}
	})
}
