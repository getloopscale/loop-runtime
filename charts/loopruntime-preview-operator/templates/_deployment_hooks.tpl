{{/*
Hooks for loopruntime-common.operatorDeployment — the only parts of this operator's
Deployment that differ from the other three operators. The skeleton itself lives
in charts/loopruntime-common/templates/_helpers.tpl.
*/}}

{{- define "loopruntime-preview-operator.envFrom" -}}{{- end }}

{{- define "loopruntime-preview-operator.env" -}}
{{- if .Values.kubeconfig.cell0.existingConfigMap }}
- name: CELL_0_KUBECONFIG
  value: /etc/loopruntime/cell-0-kubeconfig
{{- end }}
{{- end }}

{{- define "loopruntime-preview-operator.volumeMounts" -}}
{{- if .Values.kubeconfig.cell0.existingConfigMap }}
- name: cell-0-kubeconfig
  mountPath: /etc/loopruntime/cell-0-kubeconfig
  subPath: config
  readOnly: true
{{- end }}
{{- end }}

{{- define "loopruntime-preview-operator.volumes" -}}
{{- if .Values.kubeconfig.cell0.existingConfigMap }}
- name: cell-0-kubeconfig
  configMap:
    name: {{ .Values.kubeconfig.cell0.existingConfigMap }}
    items:
      - key: config
        path: config
{{- end }}
{{- end }}
