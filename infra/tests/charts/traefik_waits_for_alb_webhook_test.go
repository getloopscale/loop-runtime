// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const albController = "kube-system/aws-load-balancer-controller"

// builtNeeds returns, for one helmfile and environment, what each release waits for.
// helmfile writes a need as <kube context>/<namespace>/<name>; the context is dropped.
func builtNeeds(t *testing.T, helmfile, env string, extraEnv ...string) map[string][]string {
	t.Helper()
	out := helmfileRun(t, extraEnv, "-f", "infra/helmfile/"+helmfile, "-e", env, "build")
	var built struct {
		Releases []struct {
			Name      string   `yaml:"name"`
			Namespace string   `yaml:"namespace"`
			Needs     []string `yaml:"needs"`
		} `yaml:"releases"`
	}
	if err := yaml.Unmarshal([]byte(out), &built); err != nil {
		t.Fatalf("decode helmfile build output: %v\n%s", err, truncate(out, 500))
	}
	needs := map[string][]string{}
	for _, r := range built.Releases {
		waitsFor := []string{}
		for _, need := range r.Needs {
			if parts := strings.Split(need, "/"); len(parts) == 3 {
				need = parts[1] + "/" + parts[2]
			}
			waitsFor = append(waitsFor, need)
		}
		needs[r.Namespace+"/"+r.Name] = waitsFor
	}
	if len(needs) == 0 {
		t.Fatalf("helmfile build returned no releases for %s/%s", env, helmfile)
	}
	return needs
}

// The controller registers a validating webhook for every Ingress the moment its
// chart is applied, and refuses them until its pod serves. Traefik's ALB Ingress,
// installed beside the controller instead of after it, stopped the staging rebuild
// of 2026-11-26 with `failed calling webhook "vingress.elbv2.k8s.aws": connection
// refused`.
func TestStagingTraefikWaitsForTheLoadBalancerController(t *testing.T) {
	for _, helmfile := range []string{"cell.helmfile.yaml.gotmpl", "core.helmfile.yaml.gotmpl"} {
		t.Run(helmfile, func(t *testing.T) {
			needs := builtNeeds(t, helmfile, "staging", stagingOutputs...)

			if _, installed := needs[albController]; !installed {
				t.Fatalf("staging does not list %s in %s", albController, helmfile)
			}
			if !slices.Contains(needs["traefik/traefik"], albController) {
				t.Errorf("traefik needs %v, want it to wait for %s", needs["traefik/traefik"], albController)
			}
		})
	}
}

// Where the controller is not installed, Traefik must not wait for it: helmfile
// refuses a release that needs one it does not have.
func TestDevPublicTraefikDoesNotWaitForAControllerItLacks(t *testing.T) {
	needs := builtNeeds(t, "cell.helmfile.yaml.gotmpl", "dev-public", "LOOPZERO_CELL_CONTEXT=cell-0")

	if slices.Contains(needs["traefik/traefik"], albController) {
		t.Errorf("traefik needs %v on dev-public, which installs no %s", needs["traefik/traefik"], albController)
	}
}
