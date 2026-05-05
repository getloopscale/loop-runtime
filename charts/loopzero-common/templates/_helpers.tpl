{{/*
loopzero-common — Shared Helm template helpers for the LoopZero PaaS platform.
All charts depend on this library chart for consistent labels, security,
and operational templates.
*/}}

{{/*
----------------------------------------------------------------------
NAMING
----------------------------------------------------------------------
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "loopzero-common.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Truncated at 63 chars because some Kubernetes name fields are limited.
*/}}
{{- define "loopzero-common.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
LABELS
----------------------------------------------------------------------
*/}}

{{/*
Standard Kubernetes labels (CNCF best practice).
*/}}
{{- define "loopzero-common.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "loopzero-common.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/part-of: loopzero-platform
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Operator component label — applied to all 4 split operator deployments
(loopzero-tenant-operator, loopzero-app-operator, loopzero-preview-operator,
loopzero-platform-operator). Enables fan-out queries like:
  kubectl logs -l app.kubernetes.io/component=loopzero-operator -n loopzero-system \
    --all-containers --max-log-requests=4 -f
ServiceMonitor + NetworkPolicy + alert rule selectors use this label
for split-binary topology compatibility.

drift port for ADR-0023 / plan T1.5 / dx-engineer condition #1.

Usage in operator chart deployment.yaml:
  metadata:
    labels:
      {{- include "loopzero-common.operatorLabels" . | nindent 4 }}
*/}}
{{- define "loopzero-common.operatorLabels" -}}
{{ include "loopzero-common.labels" . }}
app.kubernetes.io/component: loopzero-operator
{{- end }}

{{/*
Selector labels — subset used in matchLabels.
*/}}
{{- define "loopzero-common.selectorLabels" -}}
app.kubernetes.io/name: {{ include "loopzero-common.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
----------------------------------------------------------------------
IMAGE
----------------------------------------------------------------------
*/}}

{{/*
Build container image reference from global registry + image config.
Usage: {{ include "loopzero-common.image" . }}
*/}}
{{- define "loopzero-common.image" -}}
{{- $registry := "" }}
{{- if .Values.global }}
{{- $registry = .Values.global.registry | default "" }}
{{- end }}
{{- $repository := .Values.image.repository }}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- if $registry }}
{{- printf "%s/%s:%s" $registry $repository $tag }}
{{- else }}
{{- printf "%s:%s" $repository $tag }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
SECURITY CONTEXT
----------------------------------------------------------------------
*/}}

{{/*
Pod security context — PSA restricted compliant.
*/}}
{{- define "loopzero-common.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: 1000
runAsGroup: 1000
fsGroup: 1000
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{/*
Container security context — hardened by default.
*/}}
{{- define "loopzero-common.containerSecurityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
runAsNonRoot: true
runAsUser: 1000
capabilities:
  drop:
    - ALL
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{/*
----------------------------------------------------------------------
SERVICE ACCOUNT
----------------------------------------------------------------------
*/}}

{{/*
Resolve ServiceAccount name.
*/}}
{{- define "loopzero-common.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "loopzero-common.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
automountServiceAccountToken — default false, override for K8s API consumers.
*/}}
{{- define "loopzero-common.automountServiceAccountToken" -}}
{{- if .Values.serviceAccount }}
{{- default false .Values.serviceAccount.automountServiceAccountToken }}
{{- else }}
false
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
PRIORITY CLASS
----------------------------------------------------------------------
*/}}

{{/*
priorityClassName for platform components.
Default: system-cluster-critical for platform, empty for user-facing (dashboard).
*/}}
{{- define "loopzero-common.priorityClassName" -}}
{{- if .Values.priorityClassName }}
priorityClassName: {{ .Values.priorityClassName | quote }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
PROBES
----------------------------------------------------------------------
*/}}

{{/*
Startup probe — prevents liveness from killing slow-starting pods.
*/}}
{{- define "loopzero-common.startupProbe" -}}
{{- if and .Values.startupProbe .Values.startupProbe.enabled }}
startupProbe:
  httpGet:
    path: {{ .Values.startupProbe.path | default "/healthz" }}
    port: {{ .Values.startupProbe.port | default "http" }}
  failureThreshold: {{ .Values.startupProbe.failureThreshold | default 30 }}
  periodSeconds: {{ .Values.startupProbe.periodSeconds | default 2 }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
TOPOLOGY / AFFINITY
----------------------------------------------------------------------
*/}}

{{/*
podAntiAffinity — auto-enabled when replicaCount > 1.
Prefer spread across nodes (soft, not hard).
*/}}
{{- define "loopzero-common.affinity" -}}
{{- $antiAffinityEnabled := false }}
{{- if and .Values.affinity .Values.affinity.podAntiAffinity .Values.affinity.podAntiAffinity.enabled }}
  {{- $antiAffinityEnabled = true }}
{{- else if and (gt (int (default 1 .Values.replicaCount)) 1) (not .Values.affinity) }}
  {{- $antiAffinityEnabled = true }}
{{- end }}
{{- if $antiAffinityEnabled }}
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              {{- include "loopzero-common.selectorLabels" . | nindent 14 }}
          topologyKey: kubernetes.io/hostname
{{- end }}
{{- end }}

{{/*
Topology spread constraints — conditional on .Values.topologySpreadConstraints.enabled
*/}}
{{- define "loopzero-common.topologySpreadConstraints" -}}
{{- if and .Values.topologySpreadConstraints .Values.topologySpreadConstraints.enabled }}
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        {{- include "loopzero-common.selectorLabels" . | nindent 8 }}
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        {{- include "loopzero-common.selectorLabels" . | nindent 8 }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
SERVICE MONITOR (Prometheus Operator)
----------------------------------------------------------------------
*/}}

{{/*
ServiceMonitor — conditional on metrics.serviceMonitor.enabled.
Renders only when the value is true.
*/}}
{{- define "loopzero-common.servicemonitor" -}}
{{- if and .Values.metrics .Values.metrics.serviceMonitor .Values.metrics.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "loopzero-common.fullname" . }}
  labels:
    {{- include "loopzero-common.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "loopzero-common.selectorLabels" . | nindent 6 }}
  endpoints:
    - port: {{ .Values.metrics.serviceMonitor.port | default "metrics" }}
      interval: {{ .Values.metrics.serviceMonitor.interval | default "30s" }}
      scrapeTimeout: {{ .Values.metrics.serviceMonitor.scrapeTimeout | default "10s" }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
POD DISRUPTION BUDGET
----------------------------------------------------------------------
*/}}

{{/*
PDB — conditional when replicaCount > 1.
*/}}
{{- define "loopzero-common.pdb" -}}
{{- if gt (int (default 1 .Values.replicaCount)) 1 }}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "loopzero-common.fullname" . }}
  labels:
    {{- include "loopzero-common.labels" . | nindent 4 }}
spec:
  minAvailable: 1
  selector:
    matchLabels:
      {{- include "loopzero-common.selectorLabels" . | nindent 6 }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
NETWORK POLICY
----------------------------------------------------------------------
*/}}

{{/*
----------------------------------------------------------------------
TPLVALUES.RENDER (charts-sota T2.1 / ADR-0037)
----------------------------------------------------------------------
*/}}

{{/*
Render values that may contain Go template syntax. Bitnami common pattern.
Usage:
  {{ include "loopzero-common.tplvalues.render" (dict "value" .Values.podAnnotations "context" $) | nindent 8 }}
*/}}
{{- define "loopzero-common.tplvalues.render" -}}
{{- $value := typeIs "string" .value | ternary .value (.value | toYaml) }}
{{- if contains "{{" (toJson .value) }}
  {{- if .scope }}
    {{- tpl (cat "{{- with $.RelativeScope -}}" $value "{{- end }}") (merge (dict "RelativeScope" .scope) .context) }}
  {{- else }}
    {{- tpl $value .context }}
  {{- end }}
{{- else }}
{{- $value }}
{{- end }}
{{- end -}}

{{/*
----------------------------------------------------------------------
KUBE VERSION GUARD (charts-sota T2.2 / ADR-0037)
----------------------------------------------------------------------
*/}}

{{/*
Return target Kubernetes version. Allows .Values.kubeVersion override for
test fixtures + matrix CI; otherwise uses cluster's actual version.
Usage:
  {{ if semverCompare ">=1.30" (include "loopzero-common.kubeVersion" .) }}...{{ end }}
*/}}
{{- define "loopzero-common.kubeVersion" -}}
{{- default .Capabilities.KubeVersion.Version .Values.kubeVersion -}}
{{- end -}}

{{/*
----------------------------------------------------------------------
CHECKSUM ANNOTATIONS (charts-sota T2.3 — eliminates 5x duplication)
----------------------------------------------------------------------
*/}}

{{/*
ConfigMap + Secret checksum annotations for auto-restart on config change.
Eliminates 5x duplication across operator deployments.
Caller is responsible for ensuring configmap.yaml and secret.yaml exist in
$.Template.BasePath. For charts without secret.yaml, use checksumAnnotationsConfigOnly.

Usage in deployment.yaml:
  metadata:
    annotations:
      {{- include "loopzero-common.checksumAnnotations" . | nindent 8 }}
*/}}
{{- define "loopzero-common.checksumAnnotations" -}}
checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
checksum/secret: {{ include (print $.Template.BasePath "/secret.yaml") . | sha256sum }}
{{- end -}}

{{/*
Variant for charts without a secret.yaml template.
*/}}
{{- define "loopzero-common.checksumAnnotationsConfigOnly" -}}
checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
{{- end -}}

{{/*
----------------------------------------------------------------------
IMAGE PULL SECRETS (charts-sota T2.4)
----------------------------------------------------------------------
*/}}

{{/*
Render imagePullSecrets list combining global + chart-local with dedup.
Usage:
  spec:
    {{- include "loopzero-common.imagePullSecrets" . | nindent 6 }}
*/}}
{{- define "loopzero-common.imagePullSecrets" -}}
{{- $globalSecrets := list -}}
{{- if and .Values.global .Values.global.imagePullSecrets -}}
  {{- $globalSecrets = .Values.global.imagePullSecrets -}}
{{- end -}}
{{- $localSecrets := default (list) .Values.imagePullSecrets -}}
{{- $combined := concat $globalSecrets $localSecrets | uniq -}}
{{- if $combined -}}
imagePullSecrets:
{{- toYaml $combined | nindent 0 }}
{{- end -}}
{{- end -}}

{{/*
----------------------------------------------------------------------
SECRETS NAME (charts-sota T2.5 — existingSecret pattern)
----------------------------------------------------------------------
*/}}

{{/*
Resolve Secret name: use .Values.secrets.existingSecret if set, else fall back
to chart fullname. Bitnami pattern.
Usage:
  envFrom:
    - secretRef:
        name: {{ include "loopzero-common.secrets.name" . }}
*/}}
{{- define "loopzero-common.secrets.name" -}}
{{- if and .Values.secrets .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "loopzero-common.fullname" . -}}
{{- end -}}
{{- end -}}

{{/*
----------------------------------------------------------------------
TMP VOLUME (charts-sota T2.6 — readOnlyRootFilesystem compat)
----------------------------------------------------------------------
*/}}

{{/*
emptyDir tmp volume mount for ROFS-enabled containers. Eliminates 5x duplication.
Usage in deployment containers[]:
  volumeMounts:
    {{- include "loopzero-common.tmpVolumeMount" . | nindent 12 }}
And in deployment.spec.template.spec.volumes:
  {{- include "loopzero-common.tmpVolume" . | nindent 8 }}
*/}}
{{- define "loopzero-common.tmpVolumeMount" -}}
- name: tmp
  mountPath: /tmp
{{- end -}}

{{- define "loopzero-common.tmpVolume" -}}
- name: tmp
  emptyDir:
    sizeLimit: {{ .Values.tmpVolume.sizeLimit | default "64Mi" }}
{{- end -}}

{{/*
----------------------------------------------------------------------
NetworkPolicy base — provides common egress rules (DNS, K8s API, PostgreSQL).
Each chart adds custom ingress and extra egress via values.

Usage in chart template:
  {{ include "loopzero-common.networkpolicy" (dict "root" . "ingress" $ingress "extraEgress" $extraEgress) }}

Or simpler: use .Values.networkPolicy.extraEgress in the chart template.
*/}}
{{- define "loopzero-common.networkpolicy" -}}
{{- if and .Values.networkPolicy .Values.networkPolicy.enabled }}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "loopzero-common.fullname" . }}
  labels:
    {{- include "loopzero-common.labels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      {{- include "loopzero-common.selectorLabels" . | nindent 6 }}
  policyTypes:
    - Ingress
    - Egress
  egress:
    # DNS resolution (required for all pods)
    - ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
    {{- if .Values.networkPolicy.extraEgress }}
    {{- toYaml .Values.networkPolicy.extraEgress | nindent 4 }}
    {{- end }}
  {{- if .Values.networkPolicy.ingress }}
  ingress:
    {{- toYaml .Values.networkPolicy.ingress | nindent 4 }}
  {{- end }}
{{- end }}
{{- end }}
