// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE } from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Gitea Backup CronJob — daily dump to DO Spaces
// ---------------------------------------------------------------------------

export function createGiteaBackupCronJob(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  gitea: k8s.helm.v3.Release,
): void {
  // Backup script: gitea dump → upload to S3 → prune old backups (retain 7).
  // Uses the Gitea container image (has `gitea` binary) + aws-cli for S3 upload.
  const backupBucket = infra.bucketName;
  const backupPrefix = "gitea-backups";
  const retainDays = 7;

  new k8s.batch.v1.CronJob(
    "gitea-backup",
    {
      metadata: {
        name: "gitea-backup",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "gitea-backup", "app.kubernetes.io/part-of": "loopzero-platform" },
      },
      spec: {
        schedule: "0 3 * * *", // Daily at 3 AM UTC
        concurrencyPolicy: "Forbid",
        successfulJobsHistoryLimit: 3,
        failedJobsHistoryLimit: 3,
        jobTemplate: {
          spec: {
            activeDeadlineSeconds: 600, // 10 min max
            backoffLimit: 2,
            template: {
              spec: {
                restartPolicy: "OnFailure",
                securityContext: {
                  runAsNonRoot: true,
                  runAsUser: 1000,
                  runAsGroup: 1000,
                  fsGroup: 1000,
                },
                containers: [
                  {
                    name: "backup",
                    image: "gitea/gitea:1.22.3",
                    command: ["/bin/sh", "-c"],
                    args: [pulumi.interpolate`
set -e
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
DUMP_DIR=/tmp/gitea-dump
mkdir -p $DUMP_DIR

echo "Starting Gitea backup $TIMESTAMP..."
cd $DUMP_DIR
gitea dump --config /data/gitea/conf/app.ini --type tar.gz --file gitea-dump-$TIMESTAMP.tar.gz 2>&1 || true

if [ -f "$DUMP_DIR/gitea-dump-$TIMESTAMP.tar.gz" ]; then
  echo "Uploading to S3..."
  # Install aws-cli (lightweight approach via python)
  pip3 install --quiet awscli 2>/dev/null || apk add --no-cache aws-cli 2>/dev/null || true

  export AWS_ACCESS_KEY_ID="$SPACES_KEY"
  export AWS_SECRET_ACCESS_KEY="$SPACES_SECRET"

  aws s3 cp "$DUMP_DIR/gitea-dump-$TIMESTAMP.tar.gz" \
    "s3://${backupBucket}/${backupPrefix}/gitea-dump-$TIMESTAMP.tar.gz" \
    --endpoint-url "https://${infra.spacesEndpoint}" 2>&1

  echo "Pruning backups older than ${retainDays} days..."
  aws s3 ls "s3://${backupBucket}/${backupPrefix}/" \
    --endpoint-url "https://${infra.spacesEndpoint}" 2>&1 | \
    while read -r line; do
      FILE_DATE=$(echo "$line" | awk '{print $1}')
      FILE_NAME=$(echo "$line" | awk '{print $4}')
      if [ -n "$FILE_DATE" ] && [ -n "$FILE_NAME" ]; then
        FILE_AGE=$(( ($(date +%s) - $(date -d "$FILE_DATE" +%s 2>/dev/null || echo 0)) / 86400 ))
        if [ "$FILE_AGE" -gt "${retainDays}" ]; then
          echo "Deleting old backup: $FILE_NAME (age: $FILE_AGE days)"
          aws s3 rm "s3://${backupBucket}/${backupPrefix}/$FILE_NAME" \
            --endpoint-url "https://${infra.spacesEndpoint}" 2>&1
        fi
      fi
    done

  echo "Backup complete: gitea-dump-$TIMESTAMP.tar.gz"
else
  echo "ERROR: gitea dump failed, no archive produced"
  exit 1
fi
`],
                    env: [
                      {
                        name: "SPACES_KEY",
                        value: config.spacesAccessKeyId,
                      },
                      {
                        name: "SPACES_SECRET",
                        value: config.spacesSecretAccessKey,
                      },
                    ],
                    volumeMounts: [
                      {
                        name: "gitea-data",
                        mountPath: "/data",
                        readOnly: true,
                      },
                    ],
                    resources: {
                      requests: { cpu: "50m", memory: "128Mi" },
                      limits: { cpu: "500m", memory: "512Mi" },
                    },
                    securityContext: {
                      allowPrivilegeEscalation: false,
                      capabilities: { drop: ["ALL"] },
                    },
                  },
                ],
                volumes: [
                  {
                    name: "gitea-data",
                    persistentVolumeClaim: {
                      claimName: "gitea-shared-storage",
                      readOnly: true,
                    },
                  },
                ],
              },
            },
          },
        },
      },
    },
    { provider, dependsOn: [ns, gitea] },
  );
}
