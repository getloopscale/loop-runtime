{{/*
SPDX-License-Identifier: Apache-2.0
F1.4 — Common Helm template helpers for loopzero-openbao chart.
*/}}

{{/* Chart name + version for labels */}}
{{- define "openbao.name" -}}
{{- "openbao" -}}
{{- end -}}

{{- define "openbao.fullname" -}}
{{- .Release.Name | default "openbao" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "openbao.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Common labels (per LoopZero convention) */}}
{{- define "openbao.labels" -}}
helm.sh/chart: {{ include "openbao.chart" . }}
app.kubernetes.io/name: {{ include "openbao.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: secret-store
loopzero.io/env: {{ .Values.env | quote }}
plan.loopzero.io/ref: "F1.4 of 2026-06-02-openbao-adoption v1.2"
{{- end -}}

{{/* Selector labels (subset for Service+PDB+NetworkPolicy match) */}}
{{- define "openbao.selectorLabels" -}}
app.kubernetes.io/name: {{ include "openbao.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Namespace name (per Q2-F1 = vault-system) */}}
{{- define "openbao.namespace" -}}
{{- .Release.Namespace | default "vault-system" -}}
{{- end -}}

{{/* DNS names for cert-manager Certificate SANs */}}
{{- define "openbao.dnsSans" -}}
- {{ include "openbao.fullname" . }}
- {{ include "openbao.fullname" . }}.{{ include "openbao.namespace" . }}
- {{ include "openbao.fullname" . }}.{{ include "openbao.namespace" . }}.svc
- {{ include "openbao.fullname" . }}.{{ include "openbao.namespace" . }}.svc.cluster.local
{{- range $i, $_ := until (int .Values.size) }}
- {{ include "openbao.fullname" $ }}-{{ $i }}.{{ include "openbao.fullname" $ }}.{{ include "openbao.namespace" $ }}.svc.cluster.local
{{- end }}
{{- end -}}
