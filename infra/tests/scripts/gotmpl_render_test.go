// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
// value of `image.repository` — the value that is actually installed, rather
// than the spelling that happens to be in the file.
//
// # Fidelity, and its declared limit
//
// It renders with `text/template` from the standard library plus a FuncMap of
// exactly two functions. That is measured, not assumed: the overlay this serves
// uses ONE template function, `env`, piped into `default`.
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
// `default` implements the one semantic this overlay needs: an empty string
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
	}).Parse(gotmpl)
	if err != nil {
		return "", fmt.Errorf("parsing overlay template: %w", err)
	}

	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, nil); err != nil {
		return "", fmt.Errorf("rendering overlay template: %w", err)
	}

	var doc struct {
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
	return doc.Image.Repository, nil
}

// TestRenderedImageRepository is the table that makes the overlay contract
// reviewable.
//
// # Why this exists
//
// `TestLoopZeroCloudImageOverlayUsesGHCRCanonical` T3 used to assert
// `strings.Contains(overlay, "ghcr.io/getloopscale/loopzero-cloud")` against a file
// whose repository is BUILT rather than written:
//
//	repository: {{ env "LOOPZERO_PLATFORM_IMAGE_PREFIX" | default "ghcr.io/getloopscale/" }}loopzero-cloud
//
// The rendered value is exactly canonical on the default path. What does not
// exist is the contiguous substring, because a template expression separates
// the prefix from the suffix. So the assertion failed on the CORRECT file, and
// had done so for an indeterminate time — the package it lives in was wired
// into no task target (B-092).
//
// Asserting on the rendered value instead of on the source spelling is the same
// correction B-110 asks of `helm-drift`, which compares file NAMES where it
// means to compare rendered resources. What is installed is not what is written.
func TestRenderedImageRepository(t *testing.T) {
	const canonical = "ghcr.io/getloopscale/loopzero-cloud"

	cases := []struct {
		name    string
		yaml    string
		env     map[string]string
		want    string
		wantErr string
	}{
		{
			name: "prefixo templado com default rende o canonico — a forma real do overlay",
			yaml: "image:\n  repository: {{ env \"LOOPZERO_PLATFORM_IMAGE_PREFIX\" | default \"ghcr.io/getloopscale/\" }}loopzero-cloud\n",
			want: canonical,
		},
		{
			name: "valor literal rende a si mesmo",
			yaml: "image:\n  repository: " + canonical + "\n",
			want: canonical,
		},
		{
			name: "env definida vence o default, que e o proposito do parametro",
			yaml: "image:\n  repository: {{ env \"LOOPZERO_PLATFORM_IMAGE_PREFIX\" | default \"ghcr.io/getloopscale/\" }}loopzero-cloud\n",
			env:  map[string]string{"LOOPZERO_PLATFORM_IMAGE_PREFIX": "registry.example.test/"},
			want: "registry.example.test/loopzero-cloud",
		},
		{
			name: "env vazia cai no default — string vazia conta como ausente",
			yaml: "image:\n  repository: {{ env \"LOOPZERO_PLATFORM_IMAGE_PREFIX\" | default \"ghcr.io/getloopscale/\" }}loopzero-cloud\n",
			env:  map[string]string{"LOOPZERO_PLATFORM_IMAGE_PREFIX": ""},
			want: canonical,
		},
		{
			name: "repositorio nao canonico rende o que declara, e quem julga e o chamador",
			yaml: "image:\n  repository: k3d-loopzero-registry:5000/loopzero-cloud\n",
			want: "k3d-loopzero-registry:5000/loopzero-cloud",
		},
		// As duas negativas abaixo sao o contrato de FALHA ALTA. Um render que
		// devolvesse string vazia num destes casos faria a asercao do chamador
		// reprovar por "nao e canonico" — mensagem que aponta para o arquivo
		// errado. O erro tem de dizer que o RENDER falhou.
		{
			name:    "funcao de template desconhecida falha alto em vez de render parcial",
			yaml:    "image:\n  repository: {{ frobnicate \"x\" }}\n",
			wantErr: "frobnicate",
		},
		{
			name:    "ausencia de image.repository e erro, nao string vazia",
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
					t.Fatalf("esperava erro contendo %q, veio nil com valor %q", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("erro nao nomeia a causa: esperava conter %q, veio %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("render falhou: %v", err)
			}
			if got != tc.want {
				t.Errorf("repositorio renderizado = %q, esperado %q", got, tc.want)
			}
		})
	}
}
