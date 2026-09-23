{{/*
Hooks for loopzero-common.operatorDeployment — the only parts of this operator's
Deployment that differ from the other three operators. The skeleton itself lives
in charts/loopzero-common/templates/_helpers.tpl.
*/}}

{{- define "loopzero-tenant-operator.envFrom" -}}
- secretRef:
    name: {{ include "loopzero-common.secrets.name" . }}
{{- end }}

{{- define "loopzero-tenant-operator.env" -}}
{{- if .Values.kubeconfig.build.existingConfigMap }}
- name: BUILD_KUBECONFIG
  value: /etc/loopzero/build-kubeconfig
{{- end }}
{{- if .Values.kubeconfig.cell0.existingConfigMap }}
- name: CELL_0_KUBECONFIG
  value: /etc/loopzero/cell-0-kubeconfig
{{- end }}
{{- end }}

{{- define "loopzero-tenant-operator.volumeMounts" -}}
{{- if .Values.kubeconfig.build.existingConfigMap }}
- name: build-kubeconfig
  mountPath: /etc/loopzero/build-kubeconfig
  subPath: config
  readOnly: true
{{- end }}
{{- if .Values.kubeconfig.cell0.existingConfigMap }}
- name: cell-0-kubeconfig
  mountPath: /etc/loopzero/cell-0-kubeconfig
  subPath: config
  readOnly: true
{{- end }}
{{- end }}

{{- define "loopzero-tenant-operator.volumes" -}}
{{- if .Values.kubeconfig.build.existingConfigMap }}
- name: build-kubeconfig
  configMap:
    name: {{ .Values.kubeconfig.build.existingConfigMap }}
    items:
      - key: config
        path: config
{{- end }}
{{- if .Values.kubeconfig.cell0.existingConfigMap }}
- name: cell-0-kubeconfig
  configMap:
    name: {{ .Values.kubeconfig.cell0.existingConfigMap }}
    items:
      - key: config
        path: config
{{- end }}
{{- end }}
