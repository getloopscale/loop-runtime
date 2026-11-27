{{- define "dps.labels" -}}
app.kubernetes.io/name: {{ .Release.Name }}
app.kubernetes.io/part-of: loop-runtime-data-plane
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/* The PostgreSQL the service connects to: the one this chart runs, or postgres.external. */}}
{{- define "dps.postgresHost" -}}{{ .Values.postgres.external.host | default (printf "%s-postgres" .Release.Name) }}{{- end }}
{{- define "dps.postgresPort" -}}{{ ternary .Values.postgres.external.port 5432 (ne .Values.postgres.external.host "") }}{{- end }}
{{/* What follows the database name in a connection URI. With postgres.external.caSecret the
     driver verifies the server's certificate and host name against the mounted CA. */}}
{{- define "dps.postgresParams" -}}{{ if .Values.postgres.external.caSecret }}?sslmode=verify-full&sslrootcert=/etc/postgres-ca/ca.crt{{ end }}{{- end }}
{{/* The CA mount of a container that connects to an external PostgreSQL over TLS. */}}
{{- define "dps.postgresCAMount" -}}
{{- if .Values.postgres.external.caSecret }}
- { name: postgres-ca, mountPath: /etc/postgres-ca, readOnly: true }
{{- end }}
{{- end }}

{{/* The env var holding a postgres.roles entry's password: themory_app -> THEMORY_APP_PASSWORD. */}}
{{- define "dps.rolePasswordEnv" -}}{{ upper .name }}_PASSWORD{{- end }}

{{/* One process's environment: the app ("app") or a worker (its name). Called with
     (dict "root" $ "process" <name>). POSTGRES_PASSWORD comes first: $(VAR) expansion only
     resolves a variable defined earlier in the list. A postgres.roles entry that names
     processes serves only those; one that names none serves them all. A worker's own env
     map is merged over the shared one, so a name appears once and the worker's value wins. */}}
{{- define "dps.env" -}}
{{- $root := .root }}
{{- $process := .process }}
- name: POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef: { name: {{ $root.Values.existingSecret }}, key: postgres-password }
- name: {{ $root.Values.postgres.uriEnv }}
  value: "postgres://{{ $root.Values.postgres.user }}:$(POSTGRES_PASSWORD)@{{ include "dps.postgresHost" $root }}:{{ include "dps.postgresPort" $root }}/{{ $root.Values.postgres.database }}{{ include "dps.postgresParams" $root }}"
{{- range $root.Values.postgres.roles }}
{{- if or (not .processes) (has $process .processes) }}
- name: {{ include "dps.rolePasswordEnv" . }}
  valueFrom:
    secretKeyRef: { name: {{ $root.Values.existingSecret }}, key: {{ .passwordKey }} }
- name: {{ .uriEnv }}
  value: "postgres://{{ .name }}:$({{ include "dps.rolePasswordEnv" . }})@{{ include "dps.postgresHost" $root }}:{{ include "dps.postgresPort" $root }}/{{ $root.Values.postgres.database }}{{ include "dps.postgresParams" $root }}"
{{- end }}
{{- end }}
{{- range $root.Values.secretEnv }}
- name: {{ .name }}
  valueFrom:
    secretKeyRef: { name: {{ $root.Values.existingSecret }}, key: {{ .key }} }
{{- end }}
{{- range $k, $v := merge (dict) (.workerEnv | default dict) (get $root.Values "env") }}
- name: {{ $k }}
  value: {{ $v | quote }}
{{- end }}
{{- end }}
