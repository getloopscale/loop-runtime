// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package operatortest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvtestBinaryDir_PrefersKubebuilderAssets(t *testing.T) {
	home := t.TempDir()
	installVersion(t, home, "1.36.2", true)

	if got := envtestBinaryDir("/opt/assets", home); got != "/opt/assets" {
		t.Fatalf("envtestBinaryDir = %q, want KUBEBUILDER_ASSETS", got)
	}
}

func TestEnvtestBinaryDir_PicksTheNewestInstallThatHasEtcd(t *testing.T) {
	home := t.TempDir()
	installVersion(t, home, "1.35.0", true)
	installVersion(t, home, "1.36.2", false) // incomplete install: no etcd

	want := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s", "1.35.0")
	if got := envtestBinaryDir("", home); got != want {
		t.Fatalf("envtestBinaryDir = %q, want %q", got, want)
	}
}

func TestEnvtestBinaryDir_IsEmptyWithoutAnyInstall(t *testing.T) {
	if got := envtestBinaryDir("", t.TempDir()); got != "" {
		t.Fatalf("envtestBinaryDir = %q, want empty so envtest reports the missing binaries", got)
	}
}

func TestEnvtestBinaryDir_IsEmptyWithoutAHome(t *testing.T) {
	if got := envtestBinaryDir("", ""); got != "" {
		t.Fatalf("envtestBinaryDir = %q, want empty", got)
	}
}

func TestCRDPathOrDefault_DefaultsToTheOperatorsCRDBases(t *testing.T) {
	if got, want := crdPathOrDefault(""), filepath.Join("..", "..", "config", "crd", "bases"); got != want {
		t.Fatalf("crdPathOrDefault(\"\") = %q, want %q", got, want)
	}
	if got := crdPathOrDefault("custom/crds"); got != "custom/crds" {
		t.Fatalf("crdPathOrDefault kept %q, want the caller's path", got)
	}
}

func TestBaseDomainOrDefault_DefaultsToTheDevDomain(t *testing.T) {
	if got := baseDomainOrDefault(""); got != "dev.getloopscale.com" {
		t.Fatalf("baseDomainOrDefault(\"\") = %q", got)
	}
	if got := baseDomainOrDefault("example.test"); got != "example.test" {
		t.Fatalf("baseDomainOrDefault kept %q, want the caller's domain", got)
	}
}

func TestSetupControllers_WiresNothingWhenNoGroupIsEnabled(t *testing.T) {
	// A handle with no manager proves no setup ran: any enabled group would
	// dereference it.
	if err := setupControllers(&EnvtestHandle{}, EnvtestConfig{}); err != nil {
		t.Fatalf("setupControllers with no groups: %v", err)
	}
}

func TestFakeRuntimeClientFactory_RecordsEveryCellItRoutes(t *testing.T) {
	f := NewFakeRuntimeClientFactory(nil)
	for _, cell := range []string{"cell-1", "", "cell-1"} {
		if _, err := f.For(context.Background(), cell); err != nil {
			t.Fatalf("For(%q): %v", cell, err)
		}
	}

	got := f.Calls()
	if len(got) != 3 || got[0] != "cell-1" || got[1] != "" || got[2] != "cell-1" {
		t.Fatalf("Calls() = %q, want every lookup in order", got)
	}
	f.Reset()
	if len(f.Calls()) != 0 {
		t.Fatal("Reset must clear recorded calls")
	}
}

func installVersion(t *testing.T, home, version string, withEtcd bool) {
	t.Helper()
	dir := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s", version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if withEtcd {
		if err := os.WriteFile(filepath.Join(dir, "etcd"), nil, 0o755); err != nil {
			t.Fatalf("write etcd: %v", err)
		}
	}
}
