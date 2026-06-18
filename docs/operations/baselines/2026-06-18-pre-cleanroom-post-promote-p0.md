# Steady-State Baseline — pre-Cleanroom post-promote-P0-fix

**Captured**: 2026-06-18T21:36:32Z
**Trigger**: pre-Cleanroom TOTAL (per cleanroom-up-live-test-mandate.md)
**Context**: 4 PRs merged today in engine repo (#85/#86/#87/#88); 1 PR open in loopzero-cloud (#1 Promote canonical). Validating end-to-end via canonical chain.

## Layer 1 — Code state

engine:
 HEAD=92cc162c26cc87124d9e58000eef6e3cb802065e
 branch=develop
 ahead=0
 working_tree_clean=no

loopzero-cloud:
 HEAD=54f6ca1a340fc815ad8fd197532eee1084723194
 branch=feat/promote-to-production-contextual
 working_tree_clean=no

Last 5 commits engine:
54f6ca1 fix(dashboard): HMR-safe mutation-in-flight store (singleton on globalThis)
177af34 feat(dashboard): in-flight transitional badge on deploy detail header (Phase 1.2)
ef1a115 feat(dashboard): promote transitional badge in deployments grid (Phase 1.1 follow-up)
73dc1f2 feat(dashboard): contextual Promote to Production + generalized mutation-in-flight
6471f38 fix(dashboard): rollback dialog closes immediately, toast carries result

Last 5 commits loopzero-cloud:
54f6ca1 fix(dashboard): HMR-safe mutation-in-flight store (singleton on globalThis)
177af34 feat(dashboard): in-flight transitional badge on deploy detail header (Phase 1.2)
ef1a115 feat(dashboard): promote transitional badge in deployments grid (Phase 1.1 follow-up)
73dc1f2 feat(dashboard): contextual Promote to Production + generalized mutation-in-flight
6471f38 fix(dashboard): rollback dialog closes immediately, toast carries result

## Layer 2 — Runtime state

### Clusters
NAME             SERVERS   AGENTS   LOADBALANCER
loopzero-build       1/1       1/1      true
loopzero-mgmt        1/1       2/2      true
loopzero-runtime-2   1/1       1/1      true
loopzero-workload    1/1       1/1      true

### Helm releases (mgmt)
NAME                  	NAMESPACE       	REVISION	UPDATED                                	STATUS  	CHART                             	APP VERSION                 
argocd                	argocd          	1       	2026-06-17 20:41:30.785512417 +0000 UTC	deployed	argo-cd-7.3.11                    	v2.11.7                     
blackbox-exporter     	monitoring      	1       	2026-06-17 20:39:32.312516501 +0000 UTC	deployed	prometheus-blackbox-exporter-9.2.0	v0.25.0                     
cert-manager          	cert-manager    	2       	2026-06-17 21:24:51.099666544 +0000 UTC	deployed	cert-manager-v1.15.3              	v1.15.3                     
cluster-issuer        	cert-manager    	1       	2026-06-17 21:25:06.924823256 +0000 UTC	deployed	cluster-issuer-0.1.0              	1.0.0                       
external-secrets      	external-secrets	1       	2026-06-17 20:39:29.707495453 +0000 UTC	deployed	external-secrets-0.14.3           	v0.14.3                     
gitea                 	loopzero-system     	1       	2026-06-17 20:41:30.308363217 +0000 UTC	deployed	gitea-10.6.0                      	1.22.3                      
gitea-redis           	loopzero-system     	1       	2026-06-17 20:39:39.841852139 +0000 UTC	deployed	redis-19.6.4                      	7.2.5                       
grafana               	monitoring      	1       	2026-06-17 20:40:38.961668571 +0000 UTC	deployed	grafana-8.5.2                     	11.2.1                      
harbor                	loopzero-system     	1       	2026-06-17 20:41:30.263347737 +0000 UTC	deployed	harbor-1.16.2                     	2.12.2                      
harbor-redis          	loopzero-system     	1       	2026-06-17 20:39:40.122885131 +0000 UTC	deployed	redis-19.6.4                      	7.2.5                       
minio                 	loopzero-system     	1       	2026-06-17 20:39:29.585148056 +0000 UTC	deployed	minio-5.4.0                       	RELEASE.2024-12-18T13-15-44Z
nfs-server-provisioner	nfs-system      	1       	2026-06-17 20:39:29.526102824 +0000 UTC	deployed	nfs-server-provisioner-1.8.0      	4.0.8                       
openbao               	vault-system    	1       	2026-06-17 21:25:10.72227994 +0000 UTC 	deployed	openbao-0.28.3                    	v2.5.4                      
openbao-cert          	vault-system    	1       	2026-06-17 21:25:08.775543902 +0000 UTC	deployed	openbao-cert-0.1.0                	1.0.0                       
openbao-init          	vault-system    	1       	2026-06-17 21:26:04.774869673 +0000 UTC	deployed	openbao-init-0.1.0                	2.5.4                       
opencost              	loopzero-system     	1       	2026-06-17 20:40:37.211550532 +0000 UTC	deployed	opencost-1.0.0                    	1.120.2                     
ory-hydra             	loopzero-auth       	1       	2026-06-17 20:41:32.866514353 +0000 UTC	deployed	hydra-0.61.0                      	v26.2.0                     
ory-kratos            	loopzero-auth       	1       	2026-06-17 20:40:37.704084429 +0000 UTC	deployed	kratos-0.61.0                     	v26.2.0                     
otel-collector        	monitoring      	1       	2026-06-17 20:40:37.947348589 +0000 UTC	deployed	opentelemetry-collector-0.96.0    	0.103.1                     
postgresql            	loopzero-system     	1       	2026-06-17 20:39:39.817110817 +0000 UTC	deployed	postgresql-18.6.2                 	18.3.0                      
prometheus            	monitoring      	1       	2026-06-17 20:39:32.364668271 +0000 UTC	deployed	prometheus-25.27.0                	v2.54.1                     
tempo                 	monitoring      	1       	2026-06-17 20:39:32.056091816 +0000 UTC	deployed	tempo-1.14.0                      	2.6.1                       
loopzero-api              	loopzero-system     	1       	2026-06-17 20:42:58.766707174 +0000 UTC	deployed	loopzero-api-0.1.0                    	1.0.0                       
loopzero-app-operator     	loopzero-system     	1       	2026-06-17 20:43:00.664540521 +0000 UTC	deployed	loopzero-app-operator-0.1.0           	dev                         

### loopzero-api image (post-PR#88 deploy)
k3d-loopzero-registry:5000/loopzero-api:promote-namespace-canonical-1781817498

### Tenant + Application CRs
NAMESPACE     NAME                  PHASE    PLAN      AGE
loopzero-system   tn-e9bb8ad57e7f3c21   Active   starter   21h

NAMESPACE   NAME                               SYNC STATUS   HEALTH STATUS
argocd      tn-e9bb8ad57e7f3c21-preview-pr-1   OutOfSync     Missing

## Layer 3 — Config state
schema_migrations:
33|f
10|f

DB state — projects + active env_targets:
     project     | environment  |   kind    | active 
-----------------+--------------+-----------+--------
 teste           | preview-pr-1 | preview   | set
 loopzerokit-example | production   | permanent | set
(2 rows)


## Layer 4 — LIVE functional probes (pre-destruction)
- GET /api/v1/projects/teste/builds via cloud BFF:
 status=401
- GET /api/v1/projects/loopzerokit-example/builds via cloud BFF:
 status=401
- Dashboard /projects:
 status=200

