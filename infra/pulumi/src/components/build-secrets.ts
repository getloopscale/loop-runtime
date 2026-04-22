// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Build cluster secrets: cosign, MinIO/Spaces, BuildKit registry, DOCR pull secret.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { createDockerRegistryPullSecret } from "./bootstrap-shared";

export interface BuildSecretsOutputs {
  readonly buildRegistryPullSecret: k8s.core.v1.Secret;
  readonly buildSA: k8s.core.v1.ServiceAccount;
}

export function createBuildSecrets(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): BuildSecretsOutputs {
  // Cosign signing key
  const cosignKey = config.cosignPrivateKeyB64
    ? Buffer.from(config.cosignPrivateKeyB64, "base64").toString("utf-8")
    : "PLACEHOLDER_RUN_PULUMI_CONFIG_SET_COSIGN";
  new k8s.core.v1.Secret(
    "cosign-signing-key",
    {
      metadata: { name: "cosign-signing-key", namespace: "argo" },
      stringData: {
        "cosign.key": cosignKey,
        "cosign.password": config.cosignPassword ?? "loopzero-cosign-dev",
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Object storage credentials (DO Spaces, S3-compatible)
  new k8s.core.v1.Secret(
    "minio-credentials-build",
    {
      metadata: { name: "loopzero-minio-credentials", namespace: "argo" },
      stringData: {
        AWS_ACCESS_KEY_ID: config.spacesAccessKeyId,
        AWS_SECRET_ACCESS_KEY: config.spacesSecretAccessKey,
        MINIO_ENDPOINT: infra.spacesEndpoint,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // BuildKit registry credentials (push access)
  const registryAuth = config.digitaloceanToken.apply(
    (token: string) => Buffer.from(`${token}:${token}`).toString("base64"),
  );
  new k8s.core.v1.Secret(
    "buildkit-registry-config",
    {
      metadata: { name: "buildkit-registry-config", namespace: "argo" },
      stringData: {
        "config.json": pulumi.interpolate`{"auths":{"registry.digitalocean.com":{"auth":"${registryAuth}"}}}`,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // DOCR pull secret (write=true for BuildKit push)
  const buildRegistryPullSecret = createDockerRegistryPullSecret(
    "build-docr-pull-secret",
    infra.registryName,
    "argo",
    provider,
    { write: true, dependsOn: [ns] },
  );

  // Workflow ServiceAccount with Helm annotations
  const registrySecretName = "loopzero-infra-registry";
  void buildRegistryPullSecret;

  const buildSA = new k8s.core.v1.ServiceAccount(
    "loopzero-build-sa",
    {
      metadata: {
        name: "loopzero-build-sa",
        namespace: "argo",
        labels: { "app.kubernetes.io/managed-by": "Helm" },
        annotations: {
          "meta.helm.sh/release-name": "argo-workflows",
          "meta.helm.sh/release-namespace": "argo",
        },
      },
      imagePullSecrets: [{ name: registrySecretName }],
    },
    { provider, dependsOn: [ns] },
  );

  return { buildRegistryPullSecret, buildSA };
}
