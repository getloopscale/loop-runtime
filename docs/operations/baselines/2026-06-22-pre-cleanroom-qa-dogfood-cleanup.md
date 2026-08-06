---
type: baseline
title: "Pre-Cleanroom Baseline — 2026-06-22T20:35:17+00:00"
---

# Pre-Cleanroom Baseline — 2026-06-22T20:35:17+00:00

## Layer 1 — Code state
```
925d0a70ba6628b65ad42c567b716d7112008d1b
develop
?? docs/operations/baselines/2026-06-22-pre-cleanroom-qa-dogfood-cleanup.md
```
## Layer 2 — Runtime state
```
NAMESPACE          NAME                                                             READY   STATUS      RESTARTS   AGE
argocd             argocd-application-controller-0                                  1/1     Running     0          119s
argocd             argocd-application-controller-1                                  1/1     Running     0          2m13s
argocd             argocd-application-controller-2                                  1/1     Running     0          2m27s
argocd             argocd-applicationset-controller-648c57694f-5f8rx                1/1     Running     0          36h
argocd             argocd-dex-server-65b668974b-67zn5                               1/1     Running     0          36h
argocd             argocd-notifications-controller-8c67dfdd9-wg6vh                  1/1     Running     0          36h
argocd             argocd-redis-5996845779-bjjhz                                    1/1     Running     0          36h
argocd             argocd-redis-secret-init-48mgd                                   0/1     Completed   0          36h
argocd             argocd-repo-server-6556785576-6bjrc                              1/1     Running     0          36h
argocd             argocd-repo-server-6556785576-6frsl                              1/1     Running     0          36h
argocd             argocd-repo-server-6556785576-npm5v                              1/1     Running     0          36h
argocd             argocd-server-cfc88cf45-g4j5s                                    1/1     Running     0          36h
cert-manager       cert-manager-68476677c7-jmh4g                                    1/1     Running     0          36h
cert-manager       cert-manager-cainjector-7746778dc-s57kn                          1/1     Running     0          36h
cert-manager       cert-manager-webhook-56cf5d6d77-rfmfw                            1/1     Running     0          36h
external-secrets   external-secrets-cert-controller-6688cd4869-jq7lt                1/1     Running     0          36h
external-secrets   external-secrets-d9c56ffc5-smlss                                 1/1     Running     0          36h
external-secrets   external-secrets-webhook-77b55766d5-g4mq2                        1/1     Running     0          36h
kube-system        coredns-9fc4f469d-66q6s                                          1/1     Running     0          36h
```
## Layer 4 — LIVE functional probes (pre-Cleanroom)
```
  https://app-dev.loopzero.example/ → 200
  https://app-dev.loopzero.example/projects → 200
  https://app-dev.loopzero.example/audit → 200
  https://app-dev.loopzero.example/scale → 200
  https://app-dev.loopzero.example/domains → 200
  https://app-dev.loopzero.example/environments → 200
  https://app-dev.loopzero.example/secrets → 200
```
