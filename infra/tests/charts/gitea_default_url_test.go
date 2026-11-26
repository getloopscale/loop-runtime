// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
)

// Issue #354. Gitea is installed into loop-runtime-system in every environment, and the
// default address components fall back to used to name a namespace called
// gitea that no environment creates. The Go side now reads one constant,
// gitearepo.DefaultInternalURL. The places below cannot import it — a chart
// default and the CRD schema default are YAML and JSON — so each is pinned to
// it here, on the RENDERED or published artifact rather than on the source.

// A loop-runtime-api installed from the published chart with its defaults must be told
// the address the platform installs Gitea at.
func TestLoopRuntimeAPIChartDefaultGiteaURLIsTheDefaultInternalURL(t *testing.T) {
	rendered := renderLoopRuntimeAPI(t)

	var found []any
	for _, cm := range renderedObjectsOfKind(t, rendered, "ConfigMap") {
		if v, ok := dig(cm, "data", "GITEA_URL").(string); ok {
			found = append(found, v)
		}
	}

	if len(found) != 1 {
		t.Fatalf("loop-runtime-api default render carries GITEA_URL in %d ConfigMaps, want exactly 1: %v", len(found), found)
	}
	if found[0] != gitearepo.DefaultInternalURL {
		t.Errorf("loop-runtime-api chart defaults GITEA_URL to %q, want gitearepo.DefaultInternalURL %q",
			found[0], gitearepo.DefaultInternalURL)
	}
}

// The CRD the loop-runtime-crds chart publishes is what an install that never builds
// the operators applies, so its PlatformBootstrap default is the one a new CR
// actually receives.
func TestLoopRuntimeCRDsChartDefaultsPlatformBootstrapGiteaURLToTheDefaultInternalURL(t *testing.T) {
	rendered := renderChart(t, publishedChart(t, "loop-runtime-crds"), "crds", nil)

	crd := renderedObject(t, rendered, "CustomResourceDefinition", "platformbootstraps.runtime.getloopscale.com")
	if crd == nil {
		t.Fatal("loop-runtime-crds renders no platformbootstraps.runtime.getloopscale.com CustomResourceDefinition")
	}

	got := dig(crd, "spec", "versions", 0, "schema", "openAPIV3Schema",
		"properties", "spec", "properties", "gitea", "properties", "internalURL", "default")
	if got != gitearepo.DefaultInternalURL {
		t.Errorf("loop-runtime-crds defaults PlatformBootstrap spec.gitea.internalURL to %v, want gitearepo.DefaultInternalURL %q",
			got, gitearepo.DefaultInternalURL)
	}
}

// The strict kubeconform schema validates rendered manifests offline; a default
// it states differently from the CRD documents a value the cluster never sets.
func TestKubeconformPlatformBootstrapSchemaDefaultsGiteaURLToTheDefaultInternalURL(t *testing.T) {
	path := filepath.Join(repoRoot(t), "infra", "kubeconform-schemas", "master-standalone-strict",
		"platformbootstrap-runtime.getloopscale.com-v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read kubeconform schema: %v", err)
	}
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode kubeconform schema %s: %v", path, err)
	}

	got := dig(schema, "properties", "spec", "properties", "gitea", "properties", "internalURL", "default")
	if got != gitearepo.DefaultInternalURL {
		t.Errorf("kubeconform schema defaults PlatformBootstrap spec.gitea.internalURL to %v, want gitearepo.DefaultInternalURL %q",
			got, gitearepo.DefaultInternalURL)
	}
}
