// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/operatorboot"
)

// config is everything the tenant-operator reads from its environment,
// loaded and validated once before anything is constructed.
type config struct {
	GiteaURL        string
	GiteaToken      string
	GiteaOrg        string
	ArgoCDNamespace string

	BuildKubeconfig string
	Cell0Kubeconfig string

	MinIO *minIOConfig // nil: per-tenant MinIO credentials disabled

	RegistryCIDRs        []string
	DOToken              string
	RegistryDockerConfig string
	SpacesAccessKeyID    string
	SpacesSecretKey      string
	WorkloadClusterURL   string

	// TenantMaxConcurrent is 0 when unset, which lets the controller default.
	TenantMaxConcurrent int
}

type minIOConfig struct {
	Endpoint, AccessKey, SecretKey string
}

// loadConfig reads the environment through getenv. Every problem is
// collected, so one start names every missing or malformed variable.
func loadConfig(getenv func(string) string) (config, error) {
	var problems []error
	required := func(key string) string {
		v := getenv(key)
		if v == "" {
			problems = append(problems, fmt.Errorf("%s is required", key))
		}
		return v
	}
	orDefault := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}

	cfg := config{
		GiteaURL:   orDefault("GITEA_URL", constants.DefaultGiteaURL),
		GiteaToken: required("GITEA_TOKEN"),
		// B-069: the organisation is configuration, as on the api side.
		GiteaOrg:             orDefault("GITEA_ORG", constants.DefaultGiteaOrg),
		ArgoCDNamespace:      orDefault("ARGOCD_NAMESPACE", constants.DefaultArgoCDNamespace),
		BuildKubeconfig:      required("BUILD_KUBECONFIG"),
		Cell0Kubeconfig:      required("CELL_0_KUBECONFIG"),
		RegistryCIDRs:        splitList(getenv("REGISTRY_CIDRS")),
		DOToken:              getenv("DIGITALOCEAN_TOKEN"),
		RegistryDockerConfig: getenv("REGISTRY_DOCKER_CONFIG"),
		SpacesAccessKeyID:    getenv("SPACES_ACCESS_KEY_ID"),
		SpacesSecretKey:      getenv("SPACES_SECRET_ACCESS_KEY"),
		WorkloadClusterURL:   getenv("WORKLOAD_CLUSTER_URL"),
	}

	maxConcurrent, err := operatorboot.PositiveInt(getenv, "LOOPZERO_TENANT_MAX_CONCURRENT", 0)
	if err != nil {
		problems = append(problems, err)
	}
	cfg.TenantMaxConcurrent = maxConcurrent

	if cfg.DOToken != "" && cfg.RegistryDockerConfig != "" {
		problems = append(problems, errors.New("DIGITALOCEAN_TOKEN and REGISTRY_DOCKER_CONFIG are mutually exclusive"))
	}

	minio := minIOConfig{Endpoint: getenv("MINIO_ENDPOINT"), AccessKey: getenv("MINIO_ACCESS_KEY"), SecretKey: getenv("MINIO_SECRET_KEY")}
	if minio.Endpoint != "" && minio.AccessKey != "" && minio.SecretKey != "" {
		cfg.MinIO = &minio
	}

	return cfg, errors.Join(problems...)
}

// splitList splits a comma-separated value, dropping blank entries.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
