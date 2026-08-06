---
type: baseline
title: T6.2 Pre-Rebuild Baseline
description: "Captured: 2026-06-11T16:29:11Z"
---

# T6.2 Pre-Rebuild Baseline
Captured: 2026-06-11T16:29:11Z

## Layer 1 — Code state
```
HEAD:    540bc70f879f0357fcd5bb118139a5067fb8b4ca
Branch:  worktree-glowing-growing-babbage
Dirty:   1 files
Ahead:   2410 commits ahead of main
```

## Layer 2 — Runtime state (clusters)
```
NAME             SERVERS   AGENTS   LOADBALANCER
loopzero-build       1/1       1/1      true
loopzero-mgmt        1/1       2/2      true
loopzero-runtime-2   1/1       1/1      true
loopzero-workload    1/1       1/1      true
---docker loopzero containers---
k3d-loopzero-runtime-2-serverlb Up 25 hours
k3d-loopzero-runtime-2-agent-0 Up 25 hours
k3d-loopzero-runtime-2-server-0 Up 25 hours
k3d-loopzero-workload-serverlb Up 25 hours
k3d-loopzero-workload-agent-0 Up 25 hours
k3d-loopzero-workload-server-0 Up 25 hours
k3d-loopzero-build-serverlb Up 25 hours
k3d-loopzero-build-agent-0 Up 25 hours
k3d-loopzero-build-server-0 Up 25 hours
k3d-loopzero-mgmt-serverlb Up 25 hours
k3d-loopzero-mgmt-agent-1 Up 25 hours
k3d-loopzero-mgmt-agent-0 Up 25 hours
k3d-loopzero-mgmt-server-0 Up 25 hours
k3d-loopzero-registry Up 25 hours
```

## Layer 2b — Helm releases (mgmt cluster)
```
NAME                  	NAMESPACE       	REVISION	UPDATED                                	STATUS  	CHART                             	APP VERSION                 
argocd                	argocd          	1       	2026-06-10 15:55:09.725090511 +0000 UTC	deployed	argo-cd-7.3.11                    	v2.11.7                     
blackbox-exporter     	monitoring      	1       	2026-06-10 15:52:46.736409727 +0000 UTC	deployed	prometheus-blackbox-exporter-9.2.0	v0.25.0                     
cert-manager          	cert-manager    	2       	2026-06-10 16:24:14.519161416 +0000 UTC	deployed	cert-manager-v1.15.3              	v1.15.3                     
cluster-issuer        	cert-manager    	1       	2026-06-10 16:24:35.453291561 +0000 UTC	deployed	cluster-issuer-0.1.0              	1.0.0                       
external-secrets      	external-secrets	1       	2026-06-10 15:52:44.565786822 +0000 UTC	deployed	external-secrets-0.14.3           	v0.14.3                     
gitea                 	loopzero-system     	1       	2026-06-10 15:55:09.129204923 +0000 UTC	deployed	gitea-10.6.0                      	1.22.3                      
gitea-redis           	loopzero-system     	1       	2026-06-10 15:52:52.545298933 +0000 UTC	deployed	redis-19.6.4                      	7.2.5                       
grafana               	monitoring      	1       	2026-06-10 15:54:21.886183451 +0000 UTC	deployed	grafana-8.5.2                     	11.2.1                      
harbor                	loopzero-system     	1       	2026-06-10 15:55:09.112285052 +0000 UTC	deployed	harbor-1.16.2                     	2.12.2                      
harbor-redis          	loopzero-system     	1       	2026-06-10 15:52:52.517816905 +0000 UTC	deployed	redis-19.6.4                      	7.2.5                       
minio                 	loopzero-system     	1       	2026-06-10 15:52:44.382298975 +0000 UTC	deployed	minio-5.4.0                       	RELEASE.2024-12-18T13-15-44Z
nfs-server-provisioner	nfs-system      	1       	2026-06-10 15:52:44.614199148 +0000 UTC	deployed	nfs-server-provisioner-1.8.0      	4.0.8                       
openbao               	vault-system    	1       	2026-06-10 16:25:02.761979969 +0000 UTC	deployed	openbao-0.28.3                    	v2.5.4                      
openbao-cert          	vault-system    	1       	2026-06-10 16:24:37.183203896 +0000 UTC	deployed	openbao-cert-0.1.0                	1.0.0                       
openbao-init          	vault-system    	1       	2026-06-10 16:27:14.18199492 +0000 UTC 	deployed	openbao-init-0.1.0                	2.5.4                       
opencost              	loopzero-system     	1       	2026-06-10 15:54:20.175534104 +0000 UTC	deployed	opencost-1.0.0                    	1.120.2                     
ory-hydra             	loopzero-auth       	1       	2026-06-10 15:55:11.324227992 +0000 UTC	deployed	hydra-0.61.0                      	v26.2.0                     
ory-kratos            	loopzero-auth       	1       	2026-06-10 15:54:20.697916399 +0000 UTC	deployed	kratos-0.61.0                     	v26.2.0                     
otel-collector        	monitoring      	1       	2026-06-10 15:54:20.819661686 +0000 UTC	deployed	opentelemetry-collector-0.96.0    	0.103.1                     
postgresql            	loopzero-system     	1       	2026-06-10 15:52:53.300905435 +0000 UTC	deployed	postgresql-18.6.2                 	18.3.0                      
prometheus            	monitoring      	1       	2026-06-10 15:52:46.831401821 +0000 UTC	deployed	prometheus-25.27.0                	v2.54.1                     
tempo                 	monitoring      	1       	2026-06-10 15:52:46.152206571 +0000 UTC	deployed	tempo-1.14.0                      	2.6.1                       
loopzero-crds             	loopzero-system     	1       	2026-06-10 15:52:44.244833388 +0000 UTC	deployed	loopzero-crds-0.2.0                   	dev                         
traefik               	traefik         	1       	2026-06-10 15:54:21.966011806 +0000 UTC	deployed	traefik-28.3.0                    	v3.0.2                      
```

## Layer 2c — Pods inventory (loopzero-system + argocd)
```
app-operator-648dd55774-mtkph Running
gitea-6fc9696d4c-rdcw2 Running
gitea-redis-master-0 Running
harbor-core-5c868d9ddb-wl7xh Running
harbor-database-0 Running
harbor-jobservice-6bcbf54984-vfqfx Running
harbor-portal-7b49df4d49-6v82p Running
harbor-redis-0 Running
harbor-redis-master-0 Running
harbor-registry-d664674c6-sntn4 Running
harbor-trivy-0 Running
minio-5759b486cc-wfbl8 Running
opencost-577ddc7764-gncb9 Running
platform-operator-987f4c698-8d8wj Running
postgresql-0 Running
preview-operator-55bcb7b6d-rg4v8 Running
tenant-operator-66c5cc6976-4m7kw Running
loopzero-api-6f5d56b775-rb9pq Running
loopzero-api-6f5d56b775-t4wwg Running
loopzero-cloud-c9ccbb65d-9jl8c Running
---argocd---
argocd-application-controller-0 Running
argocd-application-controller-1 Running
argocd-application-controller-2 Running
argocd-applicationset-controller-648c57694f-ncwgg Running
argocd-dex-server-65b668974b-b6j7v Running
argocd-notifications-controller-8c67dfdd9-hn6wt Running
argocd-redis-5996845779-z5fb2 Running
argocd-redis-secret-init-rw9nb Completed
argocd-repo-server-6bcb9b779c-85mpg Running
argocd-repo-server-6bcb9b779c-97ffz Running
```

## Layer 2d — Tenant + Environment CRs
```
loopzero-system   tenant.loopzero.io/tn-0ca75f9f32a0193d   Active   starter   21h
loopzero-system   tenant.loopzero.io/tn-e2etest            Active   starter   24h
loopzero-system   environment.loopzero.io/tn-0ca75f9f32a0193d-production   Active   loopzero-tn-0ca75f9f32a0193d-teste-production   21h
---ClusterCredentials---
loopzero-system   cell-1         cell-1         true   24h
loopzero-system   cell-2         cell-2         true   24h
loopzero-system   cell-default   cell-default   true   24h
---ArgoCD apps---
argocd   m2-test-same-name-diff-proj-staging   Unknown   Healthy
argocd   test-add-env-idem-staging             Unknown   Healthy
argocd   test-rm-env-production                Unknown   Healthy
argocd   tn-0ca75f9f32a0193d-production        Unknown   Healthy
```

## Layer 3 — Config state (schema_migrations)
```
Defaulted container "postgresql" out of: postgresql, metrics
Password for user loopzero: 
psql: error: connection to server on socket "/tmp/.s.PGSQL.5432" failed: fe_sendauth: no password supplied
command terminated with exit code 2
---kratos config resourceVersion---
Error from server (NotFound): configmaps "ory-kratos-config" not found

---hydra config resourceVersion---
Error from server (NotFound): configmaps "ory-hydra-config" not found

---loopzero-service-auth secret resourceVersion---
614
```
