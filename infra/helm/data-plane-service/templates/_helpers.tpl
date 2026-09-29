{{- define "dps.labels" -}}
app.kubernetes.io/name: {{ .Release.Name }}
app.kubernetes.io/part-of: loopzero-data-plane
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "dps.postgresHost" -}}{{ .Release.Name }}-postgres{{- end }}

{{/* The environment shared by the app and its workers. POSTGRES_PASSWORD comes first:
     $(VAR) expansion only resolves a variable defined earlier in the list. */}}
{{- define "dps.env" -}}
- name: POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef: { name: {{ .Values.existingSecret }}, key: postgres-password }
- name: {{ .Values.postgres.uriEnv }}
  value: "postgres://{{ .Values.postgres.user }}:$(POSTGRES_PASSWORD)@{{ include "dps.postgresHost" . }}:5432/{{ .Values.postgres.database }}"
{{- range .Values.secretEnv }}
- name: {{ .name }}
  valueFrom:
    secretKeyRef: { name: {{ $.Values.existingSecret }}, key: {{ .key }} }
{{- end }}
{{- range $k, $v := .Values.env }}
- name: {{ $k }}
  value: {{ $v | quote }}
{{- end }}
{{- end }}
