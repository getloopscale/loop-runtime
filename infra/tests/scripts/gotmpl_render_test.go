// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"
)

// renderedImageRepository renders a helmfile values overlay and returns the
// image repository that is actually pulled — the value that is installed,
// rather than the spelling that happens to be in the file.
//
// The overlays feed the published charts, whose image helper
// (loopruntime-common.image) pulls `<global.registry>/<image.repository>` when a
// registry is set and `<image.repository>` alone otherwise. This function
// composes the two the same way, so an overlay that moves the registry prefix
// from image.repository to global.registry renders the same answer.
//
// # Fidelity, and its declared limit
//
// It renders with `text/template` from the standard library plus a FuncMap of
// exactly three functions — `env`, `default` and `trimSuffix`, the ones the
// overlays this serves use. That is measured, not assumed.
//
// Helmfile's own template dialect is far larger. Rendering the real artifact
// end-to-end would mean shelling out to `helmfile template`, which turns a
// millisecond unit test into one that depends on the binary being installed and
// on dependency resolution succeeding. That trade was refused deliberately.
//
// The limit is therefore load-bearing and is handled by FAILING LOUD: a
// template function this FuncMap does not carry makes `Parse` return an error
// naming it. It never silently renders a partial value — which would surface at
// the caller as "not canonical" and point the reader at the wrong file.
//
// `default` implements the one semantic these overlays need: an empty string
// counts as absent. Sprig's `default` also treats 0, nil and empty collections
// as absent; that generality is not reimplemented here because the only input
// is `os.Getenv`, which returns a string.
func renderedImageRepository(gotmpl string) (string, error) {
	tpl, err := template.New("overlay").Funcs(template.FuncMap{
		"env": os.Getenv,
		"default": func(fallback, given string) string {
			if given == "" {
				return fallback
			}
			return given
		},
		// Sprig's argument order: the suffix first, the string piped in last.
		"trimSuffix": func(suffix, s string) string {
			return strings.TrimSuffix(s, suffix)
		},
	}).Parse(gotmpl)
	if err != nil {
		return "", fmt.Errorf("parsing overlay template: %w", err)
	}

	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, nil); err != nil {
		return "", fmt.Errorf("rendering overlay template: %w", err)
	}

	var doc struct {
		Global struct {
			Registry string `yaml:"registry"`
		} `yaml:"global"`
		Image struct {
			Repository string `yaml:"repository"`
		} `yaml:"image"`
	}
	if err := yaml.Unmarshal(rendered.Bytes(), &doc); err != nil {
		return "", fmt.Errorf("parsing rendered overlay as YAML: %w", err)
	}
	if doc.Image.Repository == "" {
		return "", fmt.Errorf("rendered overlay declares no image.repository")
	}
	if doc.Global.Registry != "" {
		return doc.Global.Registry + "/" + doc.Image.Repository, nil
	}
	return doc.Image.Repository, nil
}

// TestRenderedImageRepository is the table that makes the overlay contract
// reviewable.
//
// # Why this exists
//
// `TestLoopRuntimeCloudImageOverlayUsesGHCRCanonical` T3 used to assert
// `strings.Contains(overlay, "ghcr.io/getloopscale/loopruntime-cloud")` against a file
// whose repository is BUILT rather than written:
//
//	repository: {{ env "LOOPRUNTIME_PLATFORM_IMAGE_PREFIX" | default "ghcr.io/getloopscale/" }}loopruntime-cloud
//
// The rendered value is exactly canonical on the default path. What does not
// exist is the contiguous substring, because a template expression separates
// the prefix from the suffix. So the assertion failed on the CORRECT file, and
// had done so for an indeterminate time — the package it lives in was wired
// into no task target (B-092).
//
// Asserting on the rendered value instead of on the source spelling is the same
// correction B-110 asked of `helm-drift`, which compared file NAMES where it
// meant to compare rendered resources. What is installed is not what is written.
func TestRenderedImageRepository(t *testing.T) {
	const canonical = "ghcr.io/getloopscale/loopruntime-cloud"

	cases := []struct {
		name    string
		yaml    string
		env     map[string]string
		want    string
		wantErr string
	}{
		{
			name: "templated registry with a default renders the canonical repository — the published-chart overlay shape",
			yaml: "global:\n  registry: {{ env \"LOOPRUNTIME_PLATFORM_IMAGE_PREFIX\" | default \"ghcr.io/getloopscale/\" | trimSuffix \"/\" }}\nimage:\n  repository: loopruntime-cloud\n",
			want: canonical,
		},
		{
			name: "templated prefix inside image.repository renders the canonical repository",
			yaml: "image:\n  repository: {{ env \"LOOPRUNTIME_PLATFORM_IMAGE_PREFIX\" | default \"ghcr.io/getloopscale/\" }}loopruntime-cloud\n",
			want: canonical,
		},
		{
			name: "a literal value renders itself",
			yaml: "image:\n  repository: " + canonical + "\n",
			want: canonical,
		},
		{
			name: "a set variable wins over the default, which is the point of the parameter",
			yaml: "global:\n  registry: {{ env \"LOOPRUNTIME_PLATFORM_IMAGE_PREFIX\" | default \"ghcr.io/getloopscale/\" | trimSuffix \"/\" }}\nimage:\n  repository: loopruntime-cloud\n",
			env:  map[string]string{"LOOPRUNTIME_PLATFORM_IMAGE_PREFIX": "registry.example.test/"},
			want: "registry.example.test/loopruntime-cloud",
		},
		{
			name: "an empty variable falls back to the default — the empty string counts as absent",
			yaml: "global:\n  registry: {{ env \"LOOPRUNTIME_PLATFORM_IMAGE_PREFIX\" | default \"ghcr.io/getloopscale/\" | trimSuffix \"/\" }}\nimage:\n  repository: loopruntime-cloud\n",
			env:  map[string]string{"LOOPRUNTIME_PLATFORM_IMAGE_PREFIX": ""},
			want: canonical,
		},
		{
			name: "a non-canonical repository renders what it declares, and the caller judges it",
			yaml: "image:\n  repository: k3d-loopruntime-registry:5000/loopruntime-cloud\n",
			want: "k3d-loopruntime-registry:5000/loopruntime-cloud",
		},
		// The two negatives below are the FAIL-LOUD contract. A render returning
		// an empty string here would make the caller's assertion fail with "not
		// canonical" — a message that points at the wrong file. The error has to
		// say that the RENDER failed.
		{
			name:    "an unknown template function fails loud instead of rendering partially",
			yaml:    "image:\n  repository: {{ frobnicate \"x\" }}\n",
			wantErr: "frobnicate",
		},
		{
			name:    "a missing image.repository is an error, not an empty string",
			yaml:    "imagePullSecrets:\n  - name: ghcr-pull\n",
			wantErr: "image.repository",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got, err := renderedImageRepository(tc.yaml)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got nil with value %q", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("the error does not name the cause: expected it to contain %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("render failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("rendered repository = %q, want %q", got, tc.want)
			}
		})
	}
}
