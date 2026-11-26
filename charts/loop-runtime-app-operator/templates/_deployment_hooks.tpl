{{/*
Hooks for loop-runtime-common.operatorDeployment — the only parts of this operator's
Deployment that differ from the other three operators. The skeleton itself lives
in charts/loop-runtime-common/templates/_helpers.tpl.
*/}}

{{- define "loop-runtime-app-operator.envFrom" -}}{{- end }}

{{/*
T1.4a of deep-review-remediation-plan (closes find_arch_002 / find_arch_007 partial):
mount the cell-aware Secret + set CELL_0_KUBECONFIG env so the operator binary can
resolve per-cell runtime clients via CellAwareRuntimeClientFactory.

When .Values.cell0KubeconfigMount.requireRunning is true (set post-rollout), the
operator main flips LOOP_RUNTIME_REQUIRE_CELL_0_KUBECONFIG which fail-fasts on
missing/broken kubeconfig (per ADR D3).
*/}}
{{- define "loop-runtime-app-operator.env" -}}
{{- if .Values.cell0KubeconfigMount.enabled }}
- name: CELL_0_KUBECONFIG
  value: {{ .Values.cell0KubeconfigMount.path | default "/etc/loop-runtime/runtime/kubeconfig" }}
- name: CELL_0_KUBE_CONTEXT
  value: {{ .Values.cell0KubeconfigMount.context | default "in-cluster" }}
{{- if .Values.cell0KubeconfigMount.requireRunning }}
- name: LOOP_RUNTIME_REQUIRE_CELL_0_KUBECONFIG
  value: "true"
{{- end }}
{{- end }}
{{- end }}

{{- define "loop-runtime-app-operator.volumeMounts" -}}
{{- if .Values.cell0KubeconfigMount.enabled }}
- name: cell-0-kubeconfig
  mountPath: {{ .Values.cell0KubeconfigMount.dir | default "/etc/loop-runtime/runtime" }}
  readOnly: true
{{- end }}
{{- end }}

{{- define "loop-runtime-app-operator.volumes" -}}
{{- if .Values.cell0KubeconfigMount.enabled }}
- name: cell-0-kubeconfig
  secret:
    secretName: {{ .Values.cell0KubeconfigMount.secretName | default "loop-runtime-cluster-kubeconfigs" }}
    items:
      - key: {{ .Values.cell0KubeconfigMount.secretKey | default "cell-0.kubeconfig" }}
        path: kubeconfig
{{- end }}
{{- end }}
