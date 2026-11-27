// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package securitycontext

import (
	"os"
	"path/filepath"
	"testing"
)

// chart writes files under infra/helm/<name>/ in a temp root.
func chart(t *testing.T, root, name string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, DefaultHelmDir, name, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func find(o *Outcome, name string) *ChartResult {
	for i := range o.Charts {
		if o.Charts[i].Name == name {
			return &o.Charts[i]
		}
	}
	return nil
}

const conformantTmpl = `kind: Deployment
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
      containers:
        - securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
`

func TestConformantChart(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "good", map[string]string{"templates/deploy.yaml": conformantTmpl})
	o, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	c := find(o, "good")
	if c == nil || c.Status != "CONFORMANT" {
		t.Errorf("expected CONFORMANT, got %+v", c)
	}
	if !o.Pass {
		t.Errorf("conformant chart should pass, violations=%v", o.Violations)
	}
}

func TestNonConformantWithoutADRFails(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "bad", map[string]string{
		"templates/deploy.yaml": "kind: Deployment\nspec:\n  template:\n    spec:\n      containers: []\n",
	})
	o, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	c := find(o, "bad")
	if c == nil || c.Status != "NON-CONFORMANT" {
		t.Errorf("expected NON-CONFORMANT, got %+v", c)
	}
	if o.Pass || len(o.Violations) != 1 || o.Violations[0] != "bad" {
		t.Errorf("expected violation [bad], got %v", o.Violations)
	}
}

func TestNonConformantWithADRPasses(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "privileged", map[string]string{
		"templates/deploy.yaml": "kind: DaemonSet\nspec: {}\n",
		adrExceptionFile:        "# ADR: this chart needs host privilege.\n",
	})
	o, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	c := find(o, "privileged")
	if c == nil || !c.HasADR {
		t.Errorf("expected HasADR, got %+v", c)
	}
	if !o.Pass {
		t.Errorf("NON-CONFORMANT with ADR should pass, violations=%v", o.Violations)
	}
}

func TestPartialChart(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "partial", map[string]string{
		"templates/deploy.yaml": "kind: Deployment\nspec:\n  template:\n    spec:\n      securityContext:\n        runAsNonRoot: true\n      containers:\n        - securityContext:\n            readOnlyRootFilesystem: true\n",
	})
	o, _ := Check(Config{Root: root})
	c := find(o, "partial")
	if c == nil || c.Status != "PARTIAL" {
		t.Errorf("expected PARTIAL (2/4), got %+v", c)
	}
	// PARTIAL is not a violation (only NON-CONFORMANT without ADR is).
	if !o.Pass {
		t.Errorf("PARTIAL should not fail the gate, violations=%v", o.Violations)
	}
}

func TestNonPodBearingChartIsNA(t *testing.T) {
	root := t.TempDir()
	// Only a NetworkPolicy + no Chart.yaml deps + no pod fields in values → N/A.
	chart(t, root, "netpol", map[string]string{
		"templates/np.yaml": "kind: NetworkPolicy\nspec: {}\n",
		"values.yaml":       "enabled: true\n",
	})
	o, _ := Check(Config{Root: root})
	c := find(o, "netpol")
	if c == nil || c.PodBearing || c.Status != "N/A" {
		t.Errorf("expected N/A non-pod-bearing, got %+v", c)
	}
}

func TestChartYamlDependenciesIsPodBearing(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "upstream", map[string]string{
		"Chart.yaml": "name: upstream\ndependencies:\n  - name: redis\n",
	})
	o, _ := Check(Config{Root: root})
	c := find(o, "upstream")
	if c == nil || !c.PodBearing {
		t.Errorf("Chart.yaml with dependencies should be pod-bearing, got %+v", c)
	}
}

func TestValuesOverlayIsPodBearing(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "overlay", map[string]string{
		"values.yaml": "image:\n  repository: foo\nreplicas: 2\n",
	})
	o, _ := Check(Config{Root: root})
	c := find(o, "overlay")
	if c == nil || !c.PodBearing {
		t.Errorf("values overlay with pod fields should be pod-bearing, got %+v", c)
	}
}

func TestDropAllVariants(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "drops", map[string]string{
		"templates/deploy.yaml": "kind: Deployment\ncapabilities:\n  drop: [ALL]\n",
	})
	o, _ := Check(Config{Root: root})
	c := find(o, "drops")
	if c == nil || !c.DropAll {
		t.Errorf("drop: [ALL] should match, got %+v", c)
	}
}

func TestMissingHelmDirErrors(t *testing.T) {
	root := t.TempDir() // no infra/helm
	if _, err := Check(Config{Root: root}); err == nil {
		t.Error("expected error for missing helm dir")
	}
}

func TestSortedAndMultipleCharts(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "zeta", map[string]string{"templates/d.yaml": conformantTmpl})
	chart(t, root, "alpha", map[string]string{"templates/d.yaml": conformantTmpl})
	o, _ := Check(Config{Root: root})
	if len(o.Charts) != 2 || o.Charts[0].Name != "alpha" || o.Charts[1].Name != "zeta" {
		t.Errorf("charts should be sorted, got %v", o.Charts)
	}
}

// A template the gate cannot read used to be skipped by a walk whose error was
// discarded, so an unreadable file could hide the securityContext it holds and
// the chart would be judged on the rest.
func TestUnreadableTemplateIsAnError(t *testing.T) {
	root := t.TempDir()
	chart(t, root, "c", map[string]string{"templates/deploy.yaml": conformantTmpl})
	// Named to sort before deploy.yaml: once a file matches, the answer is known
	// and later files are not needed, so the unread one must come first.
	link := filepath.Join(root, DefaultHelmDir, "c", "templates", "a-unreadable.yaml")
	if err := os.Symlink(filepath.Join(root, "missing-target"), link); err != nil {
		t.Fatal(err)
	}

	if _, err := Check(Config{Root: root}); err == nil {
		t.Fatal("an unreadable template must be an error")
	}
}
