// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Platform services extracted from monolithic mgmt-bootstrap.ts.
// Contains: ArgoCD, Gitea, MinIO, Gitea bootstrap, ArgoCD repo credentials.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as fs from "fs";
import * as path from "path";
import {
  PLATFORM_NAMESPACE,
  HELM_VERSIONS,
} from "./bootstrap-shared";
/**
 * Gitea bootstrap — creates admin token, orgs, and stores credentials.
 */
export function bootstrapGitea(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  gitea: k8s.helm.v3.Release,
  config: PlatformStackConfig,
): void {
  // ServiceAccount for the bootstrap Job — needs permission to create/update Secrets
  const bootstrapSa = new k8s.core.v1.ServiceAccount(
    "gitea-bootstrap-sa",
    {
      metadata: {
        name: "gitea-bootstrap",
        namespace: PLATFORM_NAMESPACE,
      },
    },
    { provider, dependsOn: [ns] },
  );

  const bootstrapRole = new k8s.rbac.v1.Role(
    "gitea-bootstrap-role",
    {
      metadata: {
        name: "gitea-bootstrap",
        namespace: PLATFORM_NAMESPACE,
      },
      rules: [
        {
          apiGroups: [""],
          resources: ["secrets"],
          verbs: ["get", "create", "update", "patch"],
        },
      ],
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.rbac.v1.RoleBinding(
    "gitea-bootstrap-binding",
    {
      metadata: {
        name: "gitea-bootstrap",
        namespace: PLATFORM_NAMESPACE,
      },
      subjects: [
        {
          kind: "ServiceAccount",
          name: "gitea-bootstrap",
          namespace: PLATFORM_NAMESPACE,
        },
      ],
      roleRef: {
        kind: "Role",
        name: "gitea-bootstrap",
        apiGroup: "rbac.authorization.k8s.io",
      },
    },
    { provider, dependsOn: [bootstrapRole, bootstrapSa] },
  );

  new k8s.batch.v1.Job(
    "gitea-bootstrap",
    {
      metadata: {
        name: "gitea-bootstrap",
        namespace: PLATFORM_NAMESPACE,
        annotations: {
          "pulumi.com/skipAwait": "true",
          // Force SSA field ownership AND force replacement.
          // patchForce resolves field manager conflicts between Pulumi sessions.
          // replaceUnready forces Pulumi to delete+recreate (Jobs have immutable spec.template).
          "pulumi.com/patchForce": "true",
          "pulumi.com/replaceUnready": "true",
          // REVIEW-2026-03-30: Fixes MED-07 — was new Date().toISOString() which
          // forced Job recreation on every pulumi up. Now uses environment+version
          // so the Job only re-runs when config actually changes.
          "loopzero.io/run-id": `${config.environment}-${config.infraVersion}`,
        },
      },
      spec: {
        backoffLimit: 10,
        ttlSecondsAfterFinished: 300,
        template: {
          spec: {
            serviceAccountName: "gitea-bootstrap",
            restartPolicy: "OnFailure",
            containers: [
              {
                name: "bootstrap",
                image: "bitnami/kubectl:latest", // Bitnami removes versioned tags; latest is the only available tag
                env: [
                  {
                    name: "GITEA_ADMIN_PASS_ENV",
                    value: config.giteaAdminPassword
                      ? pulumi.interpolate`${config.giteaAdminPassword}`
                      : GITEA_ADMIN_PASSWORD_DEFAULT,
                  },
                ],
                command: ["bash", "-c"],
                args: [
                  `set -e
GITEA_URL="http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000"
GITEA_USER="gitea_admin"
GITEA_PASS="\${GITEA_ADMIN_PASS_ENV}"
NS="${PLATFORM_NAMESPACE}"

echo "=== Step 1: Wait for Gitea to be ready ==="
for i in $(seq 1 120); do
  if curl -sf "$GITEA_URL/api/v1/settings/api" > /dev/null 2>&1; then
    echo "Gitea is ready after $i attempts"
    break
  fi
  if [ "$i" = "120" ]; then
    echo "ERROR: Gitea not ready after 120 attempts"
    exit 1
  fi
  sleep 5
done

echo "=== Step 2: Check if existing token is valid ==="
EXISTING_TOKEN=""
if kubectl get secret loopzero-platform-tokens -n "$NS" -o jsonpath='{.data.GITEA_TOKEN}' 2>/dev/null | base64 -d > /tmp/existing_token 2>/dev/null; then
  EXISTING_TOKEN=$(cat /tmp/existing_token)
fi

if [ -n "$EXISTING_TOKEN" ] && [ "$EXISTING_TOKEN" != "placeholder-set-after-first-deploy" ]; then
  if curl -sf "$GITEA_URL/api/v1/user" -H "Authorization: token $EXISTING_TOKEN" > /dev/null 2>&1; then
    echo "Existing Gitea token is valid, skipping token generation"
    GITEA_TOKEN="$EXISTING_TOKEN"
  fi
fi

echo "=== Step 3: Generate Gitea API token ==="
if [ -z "\${GITEA_TOKEN:-}" ]; then
  # Delete old token if it exists (idempotent)
  curl -sf -X DELETE "$GITEA_URL/api/v1/users/$GITEA_USER/tokens/loopzero-platform" \
    -u "$GITEA_USER:$GITEA_PASS" > /dev/null 2>&1 || true

  # Generate new token with all scopes
  GITEA_TOKEN=$(curl -sf -X POST "$GITEA_URL/api/v1/users/$GITEA_USER/tokens" \
    -u "$GITEA_USER:$GITEA_PASS" \
    -H "Content-Type: application/json" \
    -d '{"name":"loopzero-platform","scopes":["write:repository","write:organization","read:user"]}' | grep -o '"sha1":"[^"]*"' | cut -d'"' -f4)

  if [ -z "$GITEA_TOKEN" ]; then
    echo "ERROR: Failed to generate Gitea token"
    exit 1
  fi
  echo "Gitea token generated successfully"
fi

echo "=== Step 4: Create orgs ==="
for ORG in loopzero loopzero-platform; do
  if curl -sf "$GITEA_URL/api/v1/orgs/$ORG" -H "Authorization: token $GITEA_TOKEN" > /dev/null 2>&1; then
    echo "Org $ORG already exists"
  else
    curl -sf -X POST "$GITEA_URL/api/v1/orgs" \
      -H "Authorization: token $GITEA_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\\"username\\":\\"$ORG\\",\\"visibility\\":\\"private\\"}" > /dev/null 2>&1 && \
      echo "Created org $ORG" || echo "WARN: Could not create org $ORG"
  fi
done

echo "=== Step 5: Generate ArgoCD apiKey token for loopzero-api account ==="
# MEET-2026-03-28-013: generate a proper apiKey JWT, NOT store the admin password.
# The loopzero-api account is configured in ArgoCD Helm values with capability: apiKey.
# This token has RBAC limited to sync+get on applications (least privilege).

# MEET-2026-04-09-045: Wait for ArgoCD to create admin secret.
# The Helm Release is "deployed" before all subresources are ready.
# Bounded wait: 60 attempts × 5s = 5min max. Exit non-zero on timeout.
echo "Waiting for ArgoCD admin secret..."
ARGOCD_SECRET_READY=false
for wait_attempt in $(seq 1 60); do
  if kubectl get secret argocd-initial-admin-secret -n "$NS" >/dev/null 2>&1; then
    ARGOCD_SECRET_READY=true
    echo "ArgoCD admin secret found (attempt $wait_attempt)"
    break
  fi
  sleep 5
done
if [ "$ARGOCD_SECRET_READY" = "false" ]; then
  echo "ERROR: ArgoCD admin secret not found after 5 minutes — ArgoCD may not be healthy"
  exit 1
fi

ARGOCD_PASS=""
ARGOCD_TOKEN=""
if kubectl get secret argocd-initial-admin-secret -n "$NS" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d > /tmp/argocd_pass 2>/dev/null; then
  ARGOCD_PASS=$(cat /tmp/argocd_pass)
  rm -f /tmp/argocd_pass
  echo "ArgoCD admin password read successfully"

  # Login as admin to get session token
  ARGOCD_URL="http://argocd-server.\${NS}.svc.cluster.local:80"
  for attempt in $(seq 1 30); do
    ADMIN_SESSION=$(curl -sf "$ARGOCD_URL/api/v1/session" \
      -d "{\\"username\\":\\"admin\\",\\"password\\":\\"$ARGOCD_PASS\\"}" 2>/dev/null | \
      sed -n 's/.*"token":"\\([^"]*\\)".*/\\1/p' 2>/dev/null || true)
    if [ -n "$ADMIN_SESSION" ]; then
      echo "ArgoCD admin session obtained"
      break
    fi
    echo "Waiting for ArgoCD server... (attempt $attempt/30)"
    sleep 5
  done

  if [ -n "$ADMIN_SESSION" ]; then
    # Generate apiKey token for loopzero-api account (non-expiring)
    ARGOCD_TOKEN=$(curl -sf "$ARGOCD_URL/api/v1/account/loopzero-api/token" \
      -H "Authorization: Bearer $ADMIN_SESSION" \
      -d '{"name":"platform-deploy"}' 2>/dev/null | \
      sed -n 's/.*"token":"\\([^"]*\\)".*/\\1/p' 2>/dev/null || true)

    if [ -n "$ARGOCD_TOKEN" ]; then
      echo "ArgoCD apiKey token generated for loopzero-api (JWT, non-expiring)"
    else
      # REVIEW-2026-03-30: Fixes MED-05 — was WARN-only, now fails the Job.
      echo "ERROR: Could not generate ArgoCD apiKey token — account loopzero-api may not exist"
      echo "ERROR: Ensure Helm values have: accounts.loopzero-api: apiKey"
      exit 1
    fi
  else
    echo "ERROR: Could not login to ArgoCD — cannot generate API token"
    exit 1
  fi
else
  echo "ERROR: Could not read ArgoCD admin password"
  exit 1
fi

echo "=== Step 6: Update loopzero-platform-tokens Secret ==="
kubectl create secret generic loopzero-platform-tokens \
  --namespace="$NS" \
  --from-literal="GITEA_TOKEN=$GITEA_TOKEN" \
  --from-literal="ARGOCD_TOKEN=$ARGOCD_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "=== Step 7: Update ArgoCD repo credentials ==="
kubectl create secret generic gitea-repo-creds \
  --namespace="$NS" \
  --from-literal="type=git" \
  --from-literal="url=http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000" \
  --from-literal="username=$GITEA_USER" \
  --from-literal="password=$GITEA_TOKEN" \
  --dry-run=client -o yaml | kubectl label -f - --dry-run=client -o yaml --local "argocd.argoproj.io/secret-type=repo-creds" | kubectl apply -f -

echo "=== Step 8: Restart ArgoCD to pick up new credentials ==="
kubectl rollout restart deployment/argocd-repo-server -n "$NS" 2>/dev/null || true

echo "=== Step 9: Call API bootstrap endpoint ==="
API_URL="http://loopzero-api.${PLATFORM_NAMESPACE}.svc.cluster.local:8080"
for i in $(seq 1 30); do
  if curl -sf "$API_URL/api/v1/health" > /dev/null 2>&1; then
    echo "API is healthy"
    curl -sf -X POST "$API_URL/internal/bootstrap" \
      -H "Content-Type: application/json" \
      -d '{"admin_github_id":"1","admin_login":"admin","tenant_slug":"loopzero","tenant_display_name":"loopzero","plan":"starter"}' && \
      echo "Bootstrap endpoint called successfully" || echo "WARN: Bootstrap endpoint returned error (may already be seeded)"
    break
  fi
  if [ "$i" = "30" ]; then
    echo "WARN: API not ready after 30 attempts, skipping bootstrap endpoint"
  fi
  sleep 5
done

echo "=== Bootstrap complete ==="
`,
                ],
                resources: {
                  requests: { cpu: "50m", memory: "64Mi" },
                  limits: { cpu: "200m", memory: "128Mi" },
                },
              },
            ],
          },
        },
      },
    },
    {
      provider,
      dependsOn: [ns, gitea, bootstrapSa],
      // The Job has ttlSecondsAfterFinished: 300 — Kubernetes auto-deletes it
      // after completion. On next `pulumi up`, the run-id annotation forces a diff,
      // but the old Job is gone. deleteBeforeReplace + replaceOnChanges ensures
      // Pulumi creates a fresh Job instead of trying to update a deleted resource.
      retainOnDelete: true,
      deleteBeforeReplace: true,
      // Jobs have immutable spec.template — any change must be delete+recreate.
      // metadata.annotations changes (run-id) also force replacement to re-run the bootstrap.
      replaceOnChanges: ["metadata.annotations", "spec"],
    },
  );

  // Create the platform tokens Secret with initial placeholders.
  // The bootstrap Job above will update this with real values once Gitea is ready.
  // Pulumi creates it first so the API deployment can reference it without errors.
  new k8s.core.v1.Secret(
    "loopzero-platform-tokens",
    {
      metadata: {
        name: "loopzero-platform-tokens",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        // Placeholder — will be overwritten by the bootstrap Job with a real token.
        GITEA_TOKEN: "placeholder-set-after-first-deploy",
        // REVIEW-2026-03-30: Fixes HIGH-09 — placeholder was "admin" which matches
        // the ArgoCD admin password. Use clearly-invalid placeholder instead.
        ARGOCD_TOKEN: "PLACEHOLDER_NOT_A_TOKEN",
      },
    },
    { provider, dependsOn: [ns] },
  );
}

/**
 * ArgoCD repo credentials — no-op, created by CI bootstrap step.
 */
export function createArgoCDRepoCredentials(
  _provider: k8s.Provider,
  _ns: k8s.core.v1.Namespace,
  _argocd: k8s.helm.v3.Release,
  _gitea: k8s.helm.v3.Release,
): void {
  // No-op: credentials created by Bootstrap step with real token.
  // See .github/workflows/infra-deploy.yml section "3. UPDATE K8s SECRETS".
}
