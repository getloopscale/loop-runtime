{{/*
Shell fragments shared by the cache and clone ClusterWorkflowTemplates. They are
emitted inside a `sh -c` block, so each one is plain shell; the caller indents it.
*/}}

{{/*
loopzero-build-cwts.mcAlias — configure the `loopzero` mc alias for MinIO.
MOD-6: --insecure is a Helm value, never hardcoded.
*/}}
{{- define "loopzero-build-cwts.mcAlias" -}}
mc alias set loopzero "$MINIO_ENDPOINT" "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY" \
{{- if .Values.minio.insecure }}
  --insecure \
{{- end }}
  --api S3v4 >/dev/null 2>&1
{{- end }}

{{/*
loopzero-build-cwts.cacheKey — compute CACHE_KEY and CACHE_PATH.

cache-restore and cache-save MUST derive the same key, or every restore misses and
the cache silently does nothing. They used to carry two copies of this block (the
save copy said "mirrors cache-restore logic"); this is the one both include.

The key is tenant/project/cache-key, suffixed with the explicit cache-key-suffix
parameter or, when that is empty, with the first 12 hex digits of the sha256 of the
first lockfile found in /workspace — so the cache invalidates when dependencies
change.
*/}}
{{- define "loopzero-build-cwts.cacheKey" -}}
SUFFIX="{{ "{{inputs.parameters.cache-key-suffix}}" }}"
if [ -z "$SUFFIX" ]; then
  LOCKFILE=""
  for f in /workspace/package-lock.json /workspace/yarn.lock /workspace/pnpm-lock.yaml \
           /workspace/go.sum /workspace/requirements.txt /workspace/poetry.lock; do
    if [ -f "$f" ]; then
      LOCKFILE="$f"
      break
    fi
  done
  if [ -n "$LOCKFILE" ]; then
    SUFFIX=$(sha256sum "$LOCKFILE" | cut -c1-12)
    echo "Computed cache suffix from ${LOCKFILE##*/}: ${SUFFIX}"
  fi
fi

if [ -n "$SUFFIX" ]; then
  CACHE_KEY="{{ "{{inputs.parameters.tenant-id}}" }}/{{ "{{inputs.parameters.project-id}}" }}/{{ "{{inputs.parameters.cache-key}}" }}-${SUFFIX}"
else
  CACHE_KEY="{{ "{{inputs.parameters.tenant-id}}" }}/{{ "{{inputs.parameters.project-id}}" }}/{{ "{{inputs.parameters.cache-key}}" }}"
fi
CACHE_PATH="loopzero/{{ .Values.sourceBucket }}/{{ "{{inputs.parameters.tenant-id}}" }}/cache/deps/${CACHE_KEY}.tar.gz"
{{- end }}
