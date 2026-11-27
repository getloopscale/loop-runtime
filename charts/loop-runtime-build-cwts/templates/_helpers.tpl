{{/*
Shell fragments shared by the cache and clone ClusterWorkflowTemplates. They are
emitted inside a `sh -c` block, so each one is plain shell; the caller indents it.
*/}}

{{/*
loop-runtime-build-cwts.mcAlias — configure the `storage` mc alias for MinIO.
MOD-6: --insecure is a Helm value, never hardcoded.
*/}}
{{- define "loop-runtime-build-cwts.mcAlias" -}}
{{- if eq (.Values.objectStorage.credentials | default "secret") "irsa" }}
# mc exchanges the pod's IRSA token for credentials at STS (its MC_*_<alias> variables).
# The alias is part of a shell variable name, so it carries no hyphen.
export MC_STS_ENDPOINT_storage="${AWS_ENDPOINT_URL_STS:-https://sts.${AWS_REGION}.amazonaws.com}"
export MC_WEB_IDENTITY_TOKEN_FILE_storage="${AWS_WEB_IDENTITY_TOKEN_FILE:?no IRSA token: the workflow ServiceAccount has no eks.amazonaws.com/role-arn}"
export MC_ROLE_ARN_storage="${AWS_ROLE_ARN:?no IRSA role: the workflow ServiceAccount has no eks.amazonaws.com/role-arn}"
mc alias set storage "${AWS_ENDPOINT_URL_S3:-${AWS_ENDPOINT_URL:-https://s3.${AWS_REGION}.amazonaws.com}}" "" "" \
{{- else }}
mc alias set storage "$MINIO_ENDPOINT" "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY" \
{{- end }}
{{- if .Values.minio.insecure }}
  --insecure \
{{- end }}
  --api S3v4 >/dev/null 2>&1
{{- end }}

{{/*
loop-runtime-build-cwts.secretEnv — the envFrom of a step, when the object store keys come
from a Secret. An IRSA step reads none.
*/}}
{{- define "loop-runtime-build-cwts.secretEnv" -}}
{{- if ne (.Values.objectStorage.credentials | default "secret") "irsa" }}
envFrom:
  - secretRef:
      name: {{ .Values.minio.existingSecret | default "loop-runtime-minio-credentials" }}
{{- end }}
{{- end }}

{{/*
loop-runtime-build-cwts.cacheKey — compute CACHE_KEY and CACHE_PATH.

cache-restore and cache-save MUST derive the same key, or every restore misses and
the cache silently does nothing. They used to carry two copies of this block (the
save copy said "mirrors cache-restore logic"); this is the one both include.

The key is tenant/project/cache-key, suffixed with the explicit cache-key-suffix
parameter or, when that is empty, with the first 12 hex digits of the sha256 of the
first lockfile found in /workspace — so the cache invalidates when dependencies
change.
*/}}
{{- define "loop-runtime-build-cwts.cacheKey" -}}
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
CACHE_PATH="storage/{{ .Values.cacheBucket | default .Values.sourceBucket }}/{{ "{{inputs.parameters.tenant-id}}" }}/cache/deps/${CACHE_KEY}.tar.gz"
{{- end }}
