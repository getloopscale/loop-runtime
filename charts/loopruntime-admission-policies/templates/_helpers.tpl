{{/*
loopruntime-admission.systemActorExemptions — the ONE matchCondition every policy that
matches UPDATE or DELETE uses to stay out of the platform's own way (B33 / B-095).

It renders a `matchConditions` list item. The policy runs only when the request
comes from NONE of:
  - a kube-system service account — the namespace controller, the garbage
    collector and the other in-tree controllers that delete and de-finalize
    objects on the cluster's behalf;
  - system:kube-controller-manager;
  - a member of system:masters (break-glass admin);
  - a platform service account in loopruntime-system, listed in
    `.Values.systemActorExemptions.platformServiceAccounts`.

Why one helper: the exemption used to be hand-typed in four templates, in two
different forms, and the copy nobody made (the published vap-tenant-namespace)
is exactly the policy that could deadlock a Tenant's deletion — removing a
finalizer is an UPDATE. A floor that lives in one place cannot be forgotten in
one file.

EC-2: an empty list would render `!(username in [])`, which is always true, so
the policy would run for every identity — the operators included. Rendering
fails instead.
*/}}
{{- define "loopruntime-admission.systemActorExemptions" -}}
{{- $accounts := .Values.systemActorExemptions.platformServiceAccounts | default list -}}
{{- if lt (len $accounts) 2 -}}
{{- fail "systemActorExemptions.platformServiceAccounts must name at least loopruntime-api and loopruntime-tenant-operator (EC-2: an empty exemption list makes the policy deny the platform's own controllers)" -}}
{{- end -}}
- name: exclude-system-actors
  expression: >-
    !(request.userInfo.username.startsWith('system:serviceaccount:kube-system:'))
    && request.userInfo.username != 'system:kube-controller-manager'
    && !(request.userInfo.username in [
{{- range $i, $sa := $accounts }}
      'system:serviceaccount:loopruntime-system:{{ $sa }}'{{ if lt (add1 $i) (len $accounts) }},{{ end }}
{{- end }}
    ])
    && !('system:masters' in request.userInfo.groups)
{{- end -}}
