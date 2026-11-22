{{- /* SPDX-License-Identifier: Apache-2.0 */ -}}

{{- define "public-routes.cfManaged" -}}
{{- if eq (.Values.certIssuer | default "CFManaged") "CFManaged" }}true{{ end -}}
{{- end -}}

{{- /* The tls block of a route for host; with a ClusterIssuer, the Secret its Certificate writes. */ -}}
{{- define "public-routes.tls" -}}
{{- if include "public-routes.cfManaged" .root }}
tls: {}
{{- else }}
tls:
  secretName: {{ .name }}-tls
{{- end }}
{{- end -}}

{{- define "public-routes.certificate" -}}
{{- if not (include "public-routes.cfManaged" .root) }}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: {{ .name }}-tls
  namespace: {{ .namespace }}
spec:
  secretName: {{ .name }}-tls
  duration: 2160h     # 90d
  renewBefore: 360h   # 15d
  commonName: {{ .host }}
  dnsNames:
    - {{ .host }}
  issuerRef:
    name: {{ .root.Values.certIssuer | quote }}
    kind: ClusterIssuer
{{- end }}
{{- end -}}
