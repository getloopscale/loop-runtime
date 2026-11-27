{{- define "loop-edge.labels" -}}
app.kubernetes.io/name: loop-edge
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end }}

{{- define "loop-edge.selectorLabels" -}}
app.kubernetes.io/name: loop-edge
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "loop-edge.image" -}}
{{- $tag := required "image.tag is not set: pin the loop-edge image CI published" .Values.image.tag -}}
{{- if .Values.image.registry -}}
{{- printf "%s/%s:%s" (trimSuffix "/" .Values.image.registry) .Values.image.repository $tag -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end }}

{{/* The ports Caddy listens on inside the pod. Unprivileged, so the container runs without root. */}}
{{- define "loop-edge.httpPort" -}}8080{{- end }}
{{- define "loop-edge.httpsPort" -}}8443{{- end }}
{{- define "loop-edge.askPort" -}}9123{{- end }}
{{- define "loop-edge.healthPort" -}}8081{{- end }}

{{- define "loop-edge.caddyfile" -}}
{{- if not .Values.platformDomains }}
{{- fail "platformDomains is empty: name the platform's domains, which never get a certificate from this fleet" }}
{{- end }}
{{- if not .Values.cells }}
{{- fail "cells is empty: name each cell and the host its load balancer answers for" }}
{{- end }}
{{- $issuer := .Values.acme.issuer }}
{{- if not (has $issuer (list "acme" "internal")) }}
{{- fail (printf "acme.issuer is %q: use acme or internal" $issuer) }}
{{- end }}
{{- if not (has .Values.upstream.hostHeader (list "viewer" "origin")) }}
{{- fail (printf "upstream.hostHeader is %q: use viewer or origin" .Values.upstream.hostHeader) }}
{{- end }}
{
	admin off
	persist_config off
	http_port {{ include "loop-edge.httpPort" . }}
	https_port {{ include "loop-edge.httpsPort" . }}
	log {
		format json
	}
{{- if eq $issuer "acme" }}
	email {{ required "acme.email is not set: the ACME account needs a contact" .Values.acme.email }}
{{- with .Values.acme.directory }}
	acme_ca {{ . }}
{{- end }}
{{- end }}
	# Every pod reads and writes the same certificates, and takes the same lock before it
	# asks for one.
	storage redis {
		host {{ required "storage.host is not set: the fleet shares its certificates through a Redis or Valkey" .Values.storage.host }}
		port {{ .Values.storage.port }}
{{- if .Values.storage.passwordSecret.name }}
		password "{$EDGE_STORAGE_PASSWORD}"
{{- end }}
		key_prefix {{ .Values.storage.keyPrefix }}
{{- if .Values.storage.encryptionKeySecret.name }}
		encryption_key "{$EDGE_STORAGE_ENCRYPTION_KEY}"
{{- end }}
		tls_enabled {{ .Values.storage.tls }}
	}
	# A certificate is obtained at the first request for a host. Caddy asks first: anyone
	# may point a name at this fleet.
	on_demand_tls {
		ask http://127.0.0.1:{{ include "loop-edge.askPort" . }}/ask
	}
}

# Answers whether a host is a tenant's active custom domain: 200 with the cell that serves
# it in X-Loop-Runtime-Cell, or the API's refusal. Caddy asks it before it obtains a
# certificate (?domain=<host>), and the custom domain site asks it for every request.
#
# The address names no host. forward_auth passes the viewer's Host on, and Caddy answers a
# Host no site names with an empty 200, which would let every request through.
http://:{{ include "loop-edge.askPort" . }} {
	bind 127.0.0.1
	@platform expression `{{ include "loop-edge.platformExpression" . }}`
	respond @platform 404
	rewrite * /internal/edge/route?{query}
	reverse_proxy {{ .Values.routeLookup.url }} {
		header_up Authorization "Bearer {env.EDGE_ROUTE_TOKEN}"
	}
}

# What the kubelet probes.
http://:{{ include "loop-edge.healthPort" . }} {
	respond /healthz 200
	respond 404
}

# Tenants' custom domains: every host.
https:// {
	tls {
		on_demand
{{- if eq $issuer "internal" }}
		issuer internal
{{- end }}
	}
	forward_auth 127.0.0.1:{{ include "loop-edge.askPort" . }} {
		uri /ask?domain={host}
		copy_headers X-Loop-Runtime-Cell
	}
	map {http.request.header.X-Loop-Runtime-Cell} {cell_origin} {
{{- range $cell, $origin := .Values.cells }}
		{{ $cell }} {{ $origin }}
{{- end }}
		default cell-without-an-origin.invalid
	}
	reverse_proxy {{ .Values.upstream.address | default "{cell_origin}:443" }} {
{{- if eq .Values.upstream.hostHeader "origin" }}
		header_up Host {cell_origin}
		header_up X-Forwarded-Host {host}
{{- end }}
		header_up -X-Loop-Runtime-Cell
{{- if .Values.upstream.tls.enabled }}
		transport http {
			tls
			tls_server_name {{ .Values.upstream.tls.serverName | default "{cell_origin}" }}
{{- if .Values.upstream.tls.caConfigMap.name }}
			tls_trust_pool file /etc/loop-edge/upstream-ca/{{ required "upstream.tls.caConfigMap.key is not set" .Values.upstream.tls.caConfigMap.key }}
{{- end }}
		}
{{- end }}
	}
}
{{- end }}

{{- define "loop-edge.platformExpression" -}}
{{- $parts := list -}}
{{- range .Values.platformDomains -}}
{{- $parts = append $parts (printf "{query.domain} == %q" .) -}}
{{- $parts = append $parts (printf "{query.domain}.endsWith(%q)" (printf ".%s" .)) -}}
{{- end -}}
{{- join " || " $parts -}}
{{- end }}
