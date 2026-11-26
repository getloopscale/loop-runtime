// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// k3d-cross-cluster-heal.sh repairs drift a k3d restart causes. The steps
// below repaired drift the SOURCE had already fixed, and each one did it by
// overwriting what the source renders (principle_violations#243):
//
//   - H6 recreated buildkit-registry-config with config.json only; the build
//     install (taskfiles/infra.yml infra:install:build) creates it with the
//     local registry's buildkitd.toml as well, which H6's apply dropped.
//   - H8 set LOOPRUNTIME_*_IMAGE on loopruntime-api to :latest; the dev-public overlay
//     renders the cosign and syft images at the tag it pins, standalone's
//     loopruntime-api is Tilt's, and LOOPRUNTIME_CLONE_IMAGE has no reader in the API.
//   - H9 ran its own `helm upgrade` of loopruntime-build-cwts; build.helmfile now
//     renders the local clone runner wherever profile.localRegistry is set.
//   - H13 and H14 (mode A) rewrote a cell's kubeconfig by hand: H13 with the
//     CORE cluster's in-cluster address (its guard label `bootstrap-cell-0-cc`
//     is not the `bootstrap-cell-cc` the bootstrap writes, so it always ran),
//     H14 with a kube-system/default token that holds none of the scoped
//     loopruntime-ctrl-runtime role. A disconnected cell is re-bootstrapped instead.
func readHeal(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "local", "k3d-cross-cluster-heal.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHeal_OverwritesNothingTheInstallRenders(t *testing.T) {
	heal := readHeal(t)
	for name, pattern := range map[string]string{
		"H6 buildkit-registry-config":       `create secret generic buildkit-registry-config`,
		"H8 loopruntime-api image env":      `set env deploy/loopruntime-api`,
		"H9 loopruntime-build-cwts upgrade": `helm upgrade[^\n]*loopruntime-build-cwts`,
	} {
		if regexp.MustCompile(pattern).MatchString(heal) {
			t.Errorf("the heal still overwrites what the install renders: %s", name)
		}
	}
}

func TestHeal_WritesNoCellKubeconfigByHand(t *testing.T) {
	heal := readHeal(t)
	if regexp.MustCompile(`patch secret cell-[0-9]-kubeconfig`).MatchString(heal) {
		t.Error("the heal patches a cell kubeconfig Secret by hand instead of re-running bootstrap-cell-cc.sh")
	}
	if regexp.MustCompile(`(?m)^kind: Config$`).MatchString(heal) {
		t.Error("the heal builds a kubeconfig in a heredoc")
	}
}

// The identity bootstrap-cell-cc.sh writes is the scoped loopruntime-ctrl-runtime
// account, labelled bootstrap-cell-cc — what a re-bootstrap restores.
func TestHeal_ReBootstrapRestoresTheScopedIdentity(t *testing.T) {
	lib, err := os.ReadFile(filepath.Join("..", "..", "scripts", "lib", "bootstrap-cell-cc-common.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`MANAGED_BY="bootstrap-cell-cc"`).Match(lib) {
		t.Error("bootstrap-cell-cc-common.sh no longer labels what it writes bootstrap-cell-cc")
	}
	scoped, err := os.ReadFile(filepath.Join("..", "..", "helmfile", "scripts", "lib", "scoped-kubeconfig.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`SA_NAME="loopruntime-ctrl-runtime"`).Match(scoped) {
		t.Error("scoped-kubeconfig.sh no longer mints the loopruntime-ctrl-runtime identity")
	}
}
