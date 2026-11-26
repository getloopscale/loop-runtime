// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func requiredEnv() map[string]string {
	return map[string]string{
		"GITEA_TOKEN":       "tok",
		"BUILD_KUBECONFIG":  "/build",
		"CELL_0_KUBECONFIG": "/cell0",
	}
}

func with(extra map[string]string) map[string]string {
	m := requiredEnv()
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestLoadConfig_AppliesTheDefaults(t *testing.T) {
	cfg, err := loadConfig(env(requiredEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.GiteaURL != constants.DefaultGiteaURL || cfg.GiteaOrg != constants.DefaultGiteaOrg ||
		cfg.ArgoCDNamespace != constants.DefaultArgoCDNamespace || cfg.TenantMaxConcurrent != 0 {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.MinIO != nil {
		t.Fatalf("optional integrations enabled without configuration: %+v", cfg)
	}
}

// Issue #354: unset, GITEA_URL falls back to the Gitea the platform installs
// (loop-runtime-system), the same address the api falls back to.
func TestLoadConfig_DefaultGiteaURLIsThePlatformGitea(t *testing.T) {
	cfg, err := loadConfig(env(requiredEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.GiteaURL != gitearepo.DefaultInternalURL {
		t.Errorf("GiteaURL default = %q, want gitearepo.DefaultInternalURL %q", cfg.GiteaURL, gitearepo.DefaultInternalURL)
	}
}

func TestLoadConfig_NamesEveryMissingRequiredVariable(t *testing.T) {
	_, err := loadConfig(env(nil))
	if err == nil {
		t.Fatal("loadConfig accepted an empty environment")
	}
	for _, k := range []string{"GITEA_TOKEN", "BUILD_KUBECONFIG", "CELL_0_KUBECONFIG"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("error %q does not name %s", err, k)
		}
	}
}

func TestLoadConfig_RefusesAMalformedConcurrency(t *testing.T) {
	if _, err := loadConfig(env(with(map[string]string{"LOOP_RUNTIME_TENANT_MAX_CONCURRENT": "lots"}))); err == nil ||
		!strings.Contains(err.Error(), "LOOP_RUNTIME_TENANT_MAX_CONCURRENT") {
		t.Fatalf("error = %v, want a refusal naming LOOP_RUNTIME_TENANT_MAX_CONCURRENT", err)
	}
}

func TestLoadConfig_ReadsTheConcurrency(t *testing.T) {
	cfg, err := loadConfig(env(with(map[string]string{"LOOP_RUNTIME_TENANT_MAX_CONCURRENT": "8"})))
	if err != nil || cfg.TenantMaxConcurrent != 8 {
		t.Fatalf("got (%d, %v), want 8", cfg.TenantMaxConcurrent, err)
	}
}

func TestLoadConfig_MinIOIsEnabledOnlyWhenFullyConfigured(t *testing.T) {
	cfg, _ := loadConfig(env(with(map[string]string{"MINIO_ENDPOINT": "minio:9000", "MINIO_ACCESS_KEY": "a"})))
	if cfg.MinIO != nil {
		t.Fatal("MinIO enabled without a secret key")
	}
	cfg, err := loadConfig(env(with(map[string]string{"MINIO_ENDPOINT": "minio:9000", "MINIO_ACCESS_KEY": "a", "MINIO_SECRET_KEY": "s"})))
	if err != nil || cfg.MinIO == nil || cfg.MinIO.Endpoint != "minio:9000" {
		t.Fatalf("got (%+v, %v), want MinIO enabled", cfg.MinIO, err)
	}
}

func TestLoadConfig_DOTokenAndRegistryConfigAreExclusive(t *testing.T) {
	if _, err := loadConfig(env(with(map[string]string{"DIGITALOCEAN_TOKEN": "t", "REGISTRY_DOCKER_CONFIG": "{}"}))); err == nil {
		t.Fatal("both registry credential sources were accepted")
	}
}

func TestLoadConfig_SplitsRegistryCIDRs(t *testing.T) {
	cfg, err := loadConfig(env(with(map[string]string{"REGISTRY_CIDRS": " 10.0.0.0/8, ,192.168.0.0/16 "})))
	if err != nil || len(cfg.RegistryCIDRs) != 2 || cfg.RegistryCIDRs[1] != "192.168.0.0/16" {
		t.Fatalf("got (%v, %v)", cfg.RegistryCIDRs, err)
	}
}
