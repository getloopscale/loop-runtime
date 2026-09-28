// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Harbor was retired from the engine on 2026-09-25 (docs/decisions.md, "Retire
// Harbor; the registry is managed"). The release it replaced installed the upstream
// chart with a values file nested under the wrong key, so every environment ran the
// vendor default admin password, an in-cluster database and Redis, and an Ingress —
// with zero consumers (#381).
//
// A release that is merely `installed: false` is still one flag away from coming
// back, so this asserts the stronger thing: no platform helmfile DECLARES a Harbor
// release in any environment, installed or not. `helmfile list` is the proof
// rather than a grep of the templates, because it is what Helmfile itself resolves
// after the gotmpl renders.

// platformHelmfiles are the helmfiles that include environments.yaml; the secrets
// helmfile is a separate composition with its own environments.
var platformHelmfiles = []string{
	"core.helmfile.yaml.gotmpl",
	"cell.helmfile.yaml.gotmpl",
	"build.helmfile.yaml.gotmpl",
}

type listedRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Chart     string `json:"chart"`
	Installed bool   `json:"installed"`
}

func TestNoPlatformHelmfileDeclaresAHarborRelease(t *testing.T) {
	for _, env := range declaredPlatformEnvironments(t) {
		for _, helmfile := range platformHelmfiles {
			t.Run(env+"/"+helmfile, func(t *testing.T) {
				t.Parallel()
				out := helmfileRun(t, []string{"LOOPZERO_DB_PASSWORD=unused", "LOOPZERO_CELL_CONTEXT=k3d-loopzero-cell-0"},
					"-f", "infra/helmfile/"+helmfile, "-e", env, "list", "--output=json")
				var releases []listedRelease
				if err := json.Unmarshal([]byte(out), &releases); err != nil {
					t.Fatalf("decode helmfile list output: %v\n%s", err, truncate(out, 500))
				}
				if len(releases) == 0 {
					t.Fatalf("helmfile list returned no releases for %s/%s; the assertion below would pass vacuously", env, helmfile)
				}
				for _, r := range releases {
					if strings.Contains(strings.ToLower(r.Name), "harbor") || strings.Contains(strings.ToLower(r.Chart), "harbor") {
						t.Errorf("release %s/%s (chart %s, installed=%t) is declared; Harbor was retired — "+
							"the registry is managed (docs/decisions.md, \"Retire Harbor\")", r.Namespace, r.Name, r.Chart, r.Installed)
					}
				}
			})
		}
	}
}

// declaredPlatformEnvironments reads the environment names from
// infra/helmfile/environments.yaml, the one declaration core, cell and build share,
// so a new environment is covered the day it is added.
func declaredPlatformEnvironments(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "infra", "helmfile", "environments.yaml"))
	if err != nil {
		t.Fatalf("read environments.yaml: %v", err)
	}
	var doc struct {
		Environments map[string]any `yaml:"environments"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("decode environments.yaml: %v", err)
	}
	if len(doc.Environments) == 0 {
		t.Fatal("environments.yaml declares no environment")
	}
	names := make([]string, 0, len(doc.Environments))
	for name := range doc.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
