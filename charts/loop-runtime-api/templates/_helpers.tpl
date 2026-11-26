{{/*
Explicit container variables of the API, in the order they take effect. An
explicit variable outranks every envFrom source, so each one here decides its
value regardless of the ConfigMap or an envFrom Secret.

  1. Kubeconfig paths. Either one Secret holding all three clusters
     (kubeconfig.clusterSecret, mounted at /etc/loop-runtime/kubeconfigs) or per-cluster
     ConfigMaps (kubeconfig.build/cell0.existingConfigMap) — never both: the two
     would each claim BUILD_KUBECONFIG, and which path wins would be an accident
     of the list order.
  2. secrets.keyRefs — single keys of Secrets the chart does not create.
*/}}
{{- define "loop-runtime-api.env" -}}
{{- $kc := .Values.kubeconfig -}}
{{- if and $kc.clusterSecret.name (or $kc.build.existingConfigMap $kc.cell0.existingConfigMap) }}
{{- fail "loop-runtime-api: kubeconfig.clusterSecret and kubeconfig.build/cell0.existingConfigMap are exclusive — set one source for the cluster kubeconfigs" }}
{{- end }}
{{- if $kc.clusterSecret.name }}
- name: CORE_KUBECONFIG
  value: /etc/loop-runtime/kubeconfigs/core.kubeconfig
- name: BUILD_KUBECONFIG
  value: /etc/loop-runtime/kubeconfigs/build.kubeconfig
- name: CELL_0_KUBECONFIG
  value: /etc/loop-runtime/kubeconfigs/cell-0.kubeconfig
{{- end }}
{{- if $kc.build.existingConfigMap }}
- name: BUILD_KUBECONFIG
  value: /etc/loop-runtime/build-kubeconfig
{{- end }}
{{- if $kc.cell0.existingConfigMap }}
- name: CELL_0_KUBECONFIG
  value: /etc/loop-runtime/cell-0-kubeconfig
{{- end }}
{{- include "loop-runtime-common.secretKeyRefs" . }}
{{- end -}}
