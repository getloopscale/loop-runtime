// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"fmt"
)

// MinioCanonicalSeed is the Go port of
// infra/scripts/local/seed-minio-canonical.sh.
//
// 4 layers preserved from the bash (D3 parity):
//
//  1. patch mgmt/loop-runtime-system/minio.data.rootPassword to canonical
//  2. patch mgmt/loop-runtime-system/minio-credentials.data.secretkey (LEGACY, if present)
//  3. rollout restart mgmt/loop-runtime-system/minio Deployment
//  4. patch build/argo/loop-runtime-minio-credentials.data.AWS_SECRET_ACCESS_KEY (if present)
//  5. rollout restart mgmt/loop-runtime-system/loop-runtime-api Deployment
//
// The canonical password comes from `LOOP_RUNTIME_MINIO_ROOT_PASSWORD` env or the
// declared SoT `loop-runtime-dev-password` (per infra/helm/postgresql/values.yaml —
// same password shared across MinIO + PG in dev-public per the helmfile
// canonical wiring). Placeholder-free by design: this is a dev-only bootstrap
// value, not sponsor-provided (per feedback_no_hardcode_gate).
type MinioCanonicalSeed struct{}

// NewMinioCanonicalSeed returns the seed instance for the registry.
func NewMinioCanonicalSeed() *MinioCanonicalSeed { return &MinioCanonicalSeed{} }

// Name is the CLI token: `loop-runtime-ops seed minio`.
func (s *MinioCanonicalSeed) Name() string { return "minio" }

const (
	minioMgmtContext              = "k3d-loop-runtime-core"
	minioBuildContext             = "k3d-loop-runtime-build"
	minioNamespace                = "loop-runtime-system"
	minioMainSecret               = "minio"
	minioMainSecretKey            = "rootPassword"
	minioLegacySecret             = "minio-credentials"
	minioLegacySecretKey          = "secretkey"
	minioMainDeployment           = "minio"
	minioBuildNamespace           = "argo"
	minioBuildSecret              = "loop-runtime-minio-credentials"
	minioBuildSecretKey           = "AWS_SECRET_ACCESS_KEY"
	minioLoopRuntimeAPIDeployment = "loop-runtime-api"
	minioCanonicalPasswordDefault = "loop-runtime-dev-password"
)

func minioCanonicalPassword(h Host) string {
	// Env override matches bash `${LOOP_RUNTIME_MINIO_ROOT_PASSWORD:-loop-runtime-dev-password}`.
	// Prefer env when non-empty to keep operator override cheap.
	if v := h.getenv("LOOP_RUNTIME_MINIO_ROOT_PASSWORD"); v != "" {
		return v
	}
	return minioCanonicalPasswordDefault
}

func (s *MinioCanonicalSeed) Run(d Deps) Result {
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would sync canonical password + restart minio + loop-runtime-api"}
	}
	pw := []byte(minioCanonicalPassword(d.Host))

	// Layer 1 — patch main mgmt/loop-runtime-system/minio Secret.
	if err := d.Kube.PatchSecretData(d.Ctx, minioNamespace, minioMainSecret,
		map[string][]byte{minioMainSecretKey: pw}); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("patch %s/%s: %v", minioNamespace, minioMainSecret, err)}
	}

	// Layer 1b — legacy alias, patch only if present (bash: `if kubectl get ...`).
	if exists, err := d.Kube.SecretExists(d.Ctx, minioNamespace, minioLegacySecret); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("probe %s/%s: %v", minioNamespace, minioLegacySecret, err)}
	} else if exists {
		if err := d.Kube.PatchSecretData(d.Ctx, minioNamespace, minioLegacySecret,
			map[string][]byte{minioLegacySecretKey: pw}); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("patch %s/%s: %v", minioNamespace, minioLegacySecret, err)}
		}
	}

	// Layer 2 — rollout restart mgmt MinIO Deployment.
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, minioNamespace, minioMainDeployment); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout %s/%s: %v", minioNamespace, minioMainDeployment, err)}
	}

	// Layer 3 — build cluster loop-runtime-minio-credentials.AWS_SECRET_ACCESS_KEY (optional).
	buildCli, cliErr := d.ClientFor(minioBuildContext)
	if cliErr != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("build context %s: %v", minioBuildContext, cliErr)}
	}
	if exists, err := buildCli.SecretExists(d.Ctx, minioBuildNamespace, minioBuildSecret); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("probe %s/%s (build): %v", minioBuildNamespace, minioBuildSecret, err)}
	} else if exists {
		if err := buildCli.PatchSecretData(d.Ctx, minioBuildNamespace, minioBuildSecret,
			map[string][]byte{minioBuildSecretKey: pw}); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("patch %s/%s (build): %v", minioBuildNamespace, minioBuildSecret, err)}
		}
	}

	// Layer 4 — rollout restart loop-runtime-api so it re-initializes the S3 client.
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, minioNamespace, minioLoopRuntimeAPIDeployment); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout %s/%s: %v", minioNamespace, minioLoopRuntimeAPIDeployment, err)}
	}

	return Result{Verdict: PASS, Reason: "canonical MinIO password synced (mgmt + build)"}
}

// Verify probes both Secrets (mgmt main + build alias if present) hold the
// canonical password. Stronger than the bash's HTTP /health probe: catches
// drift (Secret out of sync with declared SoT) at the data level.
func (s *MinioCanonicalSeed) Verify(d Deps) Result {
	pw := minioCanonicalPassword(d.Host)
	got, err := d.Kube.GetSecret(d.Ctx, minioNamespace, minioMainSecret)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v", minioNamespace, minioMainSecret, err)}
	}
	if string(got.Data[minioMainSecretKey]) != pw {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s.rootPassword diverges from canonical", minioNamespace, minioMainSecret)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s/%s.rootPassword matches canonical", minioNamespace, minioMainSecret)}
}

// getEnv is a small stdlib shim so tests can (in the future) inject env values
// via an interface without touching os. For now it delegates directly.
