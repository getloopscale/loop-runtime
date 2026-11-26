{{/*
loopruntime-common — Shared Helm template helpers for the LoopRuntime PaaS platform.
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
{{- define "loopruntime-common.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Truncated at 63 chars because some Kubernetes name fields are limited.
*/}}
{{- define "loopruntime-common.fullname" -}}
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
{{- define "loopruntime-common.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "loopruntime-common.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/part-of: loopruntime-platform
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Operator component label — applied to all 4 split operator deployments
(loopruntime-tenant-operator, loopruntime-app-operator, loopruntime-preview-operator,
loopruntime-platform-operator). Enables fan-out queries like:
  kubectl logs -l app.kubernetes.io/component=loopruntime-operator -n loopruntime-system \
    --all-containers --max-log-requests=4 -f
ServiceMonitor + NetworkPolicy + alert rule selectors use this label
for split-binary topology compatibility.

drift port for ADR-0023 / plan T1.5 / dx-engineer condition #1.

Usage in operator chart deployment.yaml:
  metadata:
    labels:
      {{- include "loopruntime-common.operatorLabels" . | nindent 4 }}
*/}}
{{- define "loopruntime-common.operatorLabels" -}}
{{ include "loopruntime-common.labels" . }}
app.kubernetes.io/component: loopruntime-operator
{{- end }}

{{/*
Selector labels — subset used in matchLabels.
*/}}
{{- define "loopruntime-common.selectorLabels" -}}
app.kubernetes.io/name: {{ include "loopruntime-common.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
----------------------------------------------------------------------
IMAGE
----------------------------------------------------------------------
*/}}

{{/*
Build container image reference from global registry + image config.
Usage: {{ include "loopruntime-common.image" . }}

T2.3 of maturity-3-62-to-4-0-roadmap plan: prefer image@digest over image:tag
when digest is set. Digest references are immutable — the same content hash
always points at the same image, defeating tag re-pin attacks. Build pipeline
emits the digest into image.digest at deploy time.

Resolution order:
  1. If image.digest is set + non-empty, use repository@digest (TAG IGNORED)
  2. Otherwise, fall back to repository:tag (legacy / dev)
Registry prefix is applied uniformly.
*/}}
{{- define "loopruntime-common.image" -}}
{{- $registry := "" }}
{{- if .Values.global }}
{{- $registry = .Values.global.registry | default "" }}
{{- end }}
{{- $repository := .Values.image.repository }}
{{- $digest := .Values.image.digest | default "" }}
{{- if $digest }}
  {{- if $registry }}
    {{- printf "%s/%s@%s" $registry $repository $digest }}
  {{- else }}
    {{- printf "%s@%s" $repository $digest }}
  {{- end }}
{{- else }}
  {{- $tag := .Values.image.tag | default .Chart.AppVersion }}
  {{- if $registry }}
    {{- printf "%s/%s:%s" $registry $repository $tag }}
  {{- else }}
    {{- printf "%s:%s" $repository $tag }}
  {{- end }}
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
{{- define "loopruntime-common.podSecurityContext" -}}
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
{{- define "loopruntime-common.containerSecurityContext" -}}
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
{{- define "loopruntime-common.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "loopruntime-common.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
automountServiceAccountToken — default false, override for K8s API consumers.
*/}}
{{- define "loopruntime-common.automountServiceAccountToken" -}}
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
{{- define "loopruntime-common.priorityClassName" -}}
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
{{- define "loopruntime-common.startupProbe" -}}
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
{{- define "loopruntime-common.affinity" -}}
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
              {{- include "loopruntime-common.selectorLabels" . | nindent 14 }}
          topologyKey: kubernetes.io/hostname
{{- end }}
{{- end }}

{{/*
Topology spread constraints — conditional on .Values.topologySpreadConstraints.enabled
*/}}
{{- define "loopruntime-common.topologySpreadConstraints" -}}
{{- if and .Values.topologySpreadConstraints .Values.topologySpreadConstraints.enabled }}
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        {{- include "loopruntime-common.selectorLabels" . | nindent 8 }}
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        {{- include "loopruntime-common.selectorLabels" . | nindent 8 }}
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
{{- define "loopruntime-common.servicemonitor" -}}
{{- if and .Values.metrics .Values.metrics.serviceMonitor .Values.metrics.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "loopruntime-common.fullname" . }}
  labels:
    {{- include "loopruntime-common.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "loopruntime-common.selectorLabels" . | nindent 6 }}
  endpoints:
    - port: {{ .Values.metrics.serviceMonitor.port | default "metrics" }}
      interval: {{ .Values.metrics.serviceMonitor.interval | default "30s" }}
      scrapeTimeout: {{ .Values.metrics.serviceMonitor.scrapeTimeout | default "10s" }}
      {{- if .Values.metrics.serviceMonitor.honorLabels }}
      # Keep the labels the target exposes instead of renaming a clash to
      # exported_<label> — for a binary that emits canonical labels of its own.
      honorLabels: true
      {{- end }}
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
{{- define "loopruntime-common.pdb" -}}
{{- if gt (int (default 1 .Values.replicaCount)) 1 }}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "loopruntime-common.fullname" . }}
  labels:
    {{- include "loopruntime-common.labels" . | nindent 4 }}
spec:
  minAvailable: 1
  selector:
    matchLabels:
      {{- include "loopruntime-common.selectorLabels" . | nindent 6 }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
NETWORK POLICY
----------------------------------------------------------------------
*/}}

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
      {{- include "loopruntime-common.checksumAnnotations" . | nindent 8 }}
*/}}
{{- define "loopruntime-common.checksumAnnotations" -}}
checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
checksum/secret: {{ include (print $.Template.BasePath "/secret.yaml") . | sha256sum }}
{{- end -}}

{{/*
Variant for charts without a secret.yaml template.
*/}}
{{- define "loopruntime-common.checksumAnnotationsConfigOnly" -}}
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
    {{- include "loopruntime-common.imagePullSecrets" . | nindent 6 }}
*/}}
{{- define "loopruntime-common.imagePullSecrets" -}}
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
        name: {{ include "loopruntime-common.secrets.name" . }}
*/}}
{{- define "loopruntime-common.secrets.name" -}}
{{- if and .Values.secrets .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- include "loopruntime-common.fullname" . -}}
{{- end -}}
{{- end -}}

{{/*
----------------------------------------------------------------------
SECRETS CREATED OUTSIDE THE CHART
----------------------------------------------------------------------
*/}}

{{/*
envFrom entries for Secrets the chart does not create — provisioned by ESO,
a bootstrap task or an operator. Each entry is typed and schema-validated in
the consuming chart:

  secrets:
    additionalSources:
      - name: loopruntime-openbao-approle
        optional: true
        reason: "..."   # required when optional is true

`optional: true` lets the kubelet start the container with the Secret absent,
which is exactly the mechanism by which a dependency disappears without the
startup saying anything (B-104). So an optional source must carry the reason
it may be absent, and the consuming chart's schema refuses one without it.
Render after the chart's own sources: a later envFrom source wins a key both
define.

Usage in a container:
  envFrom:
    ...
    {{- include "loopruntime-common.additionalSecretSources" . | nindent 12 }}
*/}}
{{- define "loopruntime-common.additionalSecretSources" -}}
{{- range ((.Values.secrets | default dict).additionalSources | default list) }}
- secretRef:
    name: {{ .name | quote }}
    optional: {{ .optional }}
{{- end }}
{{- end -}}

{{/*
Environment entries mapping ONE key of a Secret the chart does not create onto
the variable the binary reads — for a Secret whose key is not the variable name
(loopruntime-db-credentials stores the URL as `uri`; the binary reads DATABASE_URL).
Same typing and the same reason rule as additionalSources; the entry mirrors
the Kubernetes shape (name, secretKeyRef name/key):

  secrets:
    keyRefs:
      - name: DATABASE_URL
        secretName: loopruntime-db-credentials
        key: uri
        optional: true
        reason: "..."

An explicit container variable outranks every envFrom source, so a keyRef
decides its variable regardless of what the ConfigMap or an envFrom Secret
carries.

Usage in a container:
  env:
    {{- include "loopruntime-common.secretKeyRefs" . | nindent 12 }}
*/}}
{{- define "loopruntime-common.secretKeyRefs" -}}
{{- range ((.Values.secrets | default dict).keyRefs | default list) }}
- name: {{ .name | quote }}
  valueFrom:
    secretKeyRef:
      name: {{ .secretName | quote }}
      key: {{ .key | quote }}
      optional: {{ .optional }}
{{- end }}
{{- end -}}

{{/*
----------------------------------------------------------------------
INGRESS ROUTE (Traefik)
----------------------------------------------------------------------
*/}}

{{/*
A Traefik IngressRoute to the chart's Service, off unless the install enables
it. A published chart does not assume an ingress exists in the target cluster;
the Helmfile bootstrap does run Traefik, and turns this on.

  ingressRoute:
    enabled: false
    host: ""                 # required when enabled
    entryPoints: [websecure]
    middlewares: []          # [{name, namespace}]
    streamMiddlewares: []    # optional; see below
    certIssuer: CFManaged    # a ClusterIssuer name, or CFManaged

`streamMiddlewares`, when set, adds a route for the host's GET `.../stream`
endpoints (Server-Sent Events) with those middlewares instead. Leave out any
buffering middleware (body-limit): Traefik's buffering holds the whole response
until it ends, so an open stream sends nothing, and Traefik v3.0 cannot buffer
only the request.

`CFManaged` means TLS terminates at the Cloudflare edge: the route carries an
empty tls block and no cert-manager Certificate is emitted. Any other value is
the ClusterIssuer of a Certificate named <fullname>-tls for the host.

Usage (templates/ingressroute.yaml):
  {{ include "loopruntime-common.ingressRoute" . }}
*/}}
{{- define "loopruntime-common.ingressRoute" -}}
{{- $ir := .Values.ingressRoute | default dict -}}
{{- if $ir.enabled }}
{{- if not $ir.host }}
{{- fail (printf "%s: ingressRoute.host is required when ingressRoute.enabled is true" .Chart.Name) }}
{{- end }}
{{- $name := include "loopruntime-common.fullname" . }}
{{- $cfManaged := eq ($ir.certIssuer | default "CFManaged") "CFManaged" }}
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: {{ $name }}
  labels:
    {{- include "loopruntime-common.labels" . | nindent 4 }}
spec:
  entryPoints:
    {{- toYaml ($ir.entryPoints | default (list "websecure")) | nindent 4 }}
  routes:
    {{- if hasKey $ir "streamMiddlewares" }}
    - match: Host(`{{ $ir.host }}`) && PathRegexp(`/(stream|timeline)$`)
      kind: Rule
      {{- with $ir.streamMiddlewares }}
      middlewares:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      services:
        - name: {{ $name }}
          port: {{ .Values.service.port }}
    {{- end }}
    - match: Host(`{{ $ir.host }}`)
      kind: Rule
      {{- with $ir.middlewares }}
      middlewares:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      services:
        - name: {{ $name }}
          port: {{ .Values.service.port }}
  tls:
    {{- if $cfManaged }}
    {}
    {{- else }}
    secretName: {{ $name }}-tls
    {{- end }}
{{- if not $cfManaged }}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: {{ $name }}-tls
  labels:
    {{- include "loopruntime-common.labels" . | nindent 4 }}
spec:
  secretName: {{ $name }}-tls
  duration: 2160h     # 90d
  renewBefore: 360h   # 15d
  commonName: {{ $ir.host }}
  dnsNames:
    - {{ $ir.host }}
  issuerRef:
    name: {{ $ir.certIssuer | quote }}
    kind: ClusterIssuer
{{- end }}
{{- end }}
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
    {{- include "loopruntime-common.tmpVolumeMount" . | nindent 12 }}
And in deployment.spec.template.spec.volumes:
  {{- include "loopruntime-common.tmpVolume" . | nindent 8 }}
*/}}
{{- define "loopruntime-common.tmpVolumeMount" -}}
- name: tmp
  mountPath: /tmp
{{- end -}}

{{- define "loopruntime-common.tmpVolume" -}}
- name: tmp
  emptyDir:
    sizeLimit: {{ (.Values.tmpVolume | default dict).sizeLimit | default "64Mi" }}
{{- end -}}

{{/*
----------------------------------------------------------------------
NetworkPolicy base — provides common egress rules (DNS, K8s API, PostgreSQL).
Each chart adds custom ingress and extra egress via values.

Usage in chart template:
  {{ include "loopruntime-common.networkpolicy" (dict "root" . "ingress" $ingress "extraEgress" $extraEgress) }}

Or simpler: use .Values.networkPolicy.extraEgress in the chart template.
*/}}
{{- define "loopruntime-common.networkpolicy" -}}
{{- if and .Values.networkPolicy .Values.networkPolicy.enabled }}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "loopruntime-common.fullname" . }}
  labels:
    {{- include "loopruntime-common.labels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      {{- include "loopruntime-common.selectorLabels" . | nindent 6 }}
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
    {{- /* The namespace the platform's Gitea runs in. core.helmfile installs Gitea
           and tells each consumer where (networkPolicy.giteaNamespace), so the
           egress rule follows the install instead of every environment
           restating the whole extraEgress list to change one namespace. */}}
    {{- with .Values.networkPolicy.giteaNamespace }}
    - ports:
        - protocol: TCP
          port: 3000
      to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: {{ . }}
    {{- end }}
    {{- /* T2.2 of plan dashboard-badge-data-plane-poller (Red Team #128).
           When the consuming chart declares dataPlanePoller.enabled=true,
           implicitly add the Prometheus egress rule the poller needs so
           no values file has to duplicate it. loopruntime-api is the one chart
           that declares it (the poller moved there from loopruntime-app-operator).
           `and` short-circuits on charts that do not declare
           .Values.dataPlanePoller at all — no leakage. */}}
    {{- if and .Values.dataPlanePoller .Values.dataPlanePoller.enabled }}
    - ports:
        - protocol: TCP
          port: 9090
      to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: monitoring
          podSelector:
            matchLabels:
              app.kubernetes.io/name: prometheus
    {{- end }}
  {{- if .Values.networkPolicy.ingress }}
  ingress:
    {{- toYaml .Values.networkPolicy.ingress | nindent 4 }}
  {{- end }}
{{- end }}
{{- end }}

{{/*
----------------------------------------------------------------------
OPERATOR DEPLOYMENT (template method — one skeleton, four operators)
----------------------------------------------------------------------
*/}}

{{/*
The Deployment every loopruntime-*-operator chart renders. The four charts used to carry
near-identical copies of this file that differed in three places only; those
three places are the hooks:

  checksum      the name of the checksum-annotations helper to include
                ("loopruntime-common.checksumAnnotations" when the chart ships a Secret
                template, "loopruntime-common.checksumAnnotationsConfigOnly" when not)
  envFrom       extra envFrom entries after the chart ConfigMap (YAML list, may be "")
  env           env entries (YAML list, may be "")
  volumeMounts  extra volumeMounts after /tmp (YAML list, may be "")
  volumes       extra volumes after tmp (YAML list, may be "")

Usage (templates/deployment.yaml of an operator chart):
  {{- include "loopruntime-common.operatorDeployment" (dict
        "root" .
        "checksum" "loopruntime-common.checksumAnnotations"
        "envFrom" (include "loopruntime-tenant-operator.envFrom" .)
        "env" (include "loopruntime-tenant-operator.env" .)
        "volumeMounts" (include "loopruntime-tenant-operator.volumeMounts" .)
        "volumes" (include "loopruntime-tenant-operator.volumes" .)) }}
*/}}
{{- define "loopruntime-common.operatorDeployment" -}}
{{- $hooks := . -}}
{{- with .root -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "loopruntime-common.fullname" . }}
  labels:
    {{- include "loopruntime-common.operatorLabels" . | nindent 4 }}
  {{- if .Values.reloader.enabled }}
  annotations:
    reloader.stakater.com/auto: "true"
  {{- end }}
spec:
  replicas: {{ .Values.replicaCount }}
  revisionHistoryLimit: {{ .Values.revisionHistoryLimit | default 3 }}
  selector:
    matchLabels:
      {{- include "loopruntime-common.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        {{- include $hooks.checksum . | nindent 8 }}
        {{- with .Values.podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      labels:
        {{- include "loopruntime-common.selectorLabels" . | nindent 8 }}
        runtime.getloopscale.com/component: control-plane
        app.kubernetes.io/component: loopruntime-operator
        {{- with .Values.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      {{- with (include "loopruntime-common.imagePullSecrets" .) }}
      {{- . | nindent 6 }}
      {{- end }}
      serviceAccountName: {{ include "loopruntime-common.serviceAccountName" . }}
      automountServiceAccountToken: {{ include "loopruntime-common.automountServiceAccountToken" . }}
      securityContext:
        {{- include "loopruntime-common.podSecurityContext" . | nindent 8 }}
      {{- include "loopruntime-common.priorityClassName" . | nindent 6 }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- include "loopruntime-common.affinity" . | nindent 6 }}
      {{- include "loopruntime-common.topologySpreadConstraints" . | nindent 6 }}
      containers:
        - name: controller
          image: {{ include "loopruntime-common.image" . }}
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          args:
            - "--leader-elect={{ .Values.args.leaderElect }}"
            - "--health-probe-bind-address={{ .Values.args.healthProbeBindAddress }}"
            - "--metrics-bind-address={{ .Values.args.metricsBindAddress }}"
          securityContext:
            {{- include "loopruntime-common.containerSecurityContext" . | nindent 12 }}
          ports:
            - name: health
              containerPort: 8081
              protocol: TCP
            - name: metrics
              containerPort: 8443
              protocol: TCP
          envFrom:
            - configMapRef:
                name: {{ include "loopruntime-common.fullname" . }}
            {{- with trim $hooks.envFrom }}
            {{- . | nindent 12 }}
            {{- end }}
          {{- with trim $hooks.env }}
          env:
            {{- . | nindent 12 }}
          {{- end }}
          {{- include "loopruntime-common.startupProbe" . | nindent 10 }}
          {{- with .Values.livenessProbe }}
          livenessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .Values.readinessProbe }}
          readinessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          volumeMounts:
            {{- include "loopruntime-common.tmpVolumeMount" . | nindent 12 }}
            {{- with trim $hooks.volumeMounts }}
            {{- . | nindent 12 }}
            {{- end }}
      volumes:
        {{- include "loopruntime-common.tmpVolume" . | nindent 8 }}
        {{- with trim $hooks.volumes }}
        {{- . | nindent 8 }}
        {{- end }}
{{- end }}
{{- end }}
