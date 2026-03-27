# Sprint 7 — WORKLOAD SECURITY HARDENING

**Objetivo:** Eliminar todas as brechas de segurança identificadas no cluster workload.
**Confiança:** ALTA (7.1-7.4), MÉDIA (7.5-7.7)
**Esforço estimado:** ~6-8h incluindo testes e validação
**Rollback:** Cada task tem rollback individual. Nenhuma é destrutiva.

---

## Contexto

Auditoria de segurança do cluster workload (2026-03-26) identificou 8 problemas:

| # | Severidade | Problema |
|---|-----------|----------|
| 7.1 | CRÍTICO | Pods de workload sem SecurityContext (rodam como root) |
| 7.2 | CRÍTICO | Namespaces sem PodSecurity Admission labels |
| 7.3 | ALTO | NetworkPolicy é condicional (pode ser desligada) |
| 7.4 | ALTO | Pods usam ServiceAccount `default` sem restrição |
| 7.5 | MÉDIO | PostgreSQL com egress `- {}` (tudo aberto) |
| 7.6 | MÉDIO | MinIO ingress sem source selector (porta aberta) |
| 7.7 | BAIXO | Image refs podem usar tags em vez de digest |

---

## Pré-requisito: GATE-6 (Sistema operacional saudável)

Antes de iniciar:

- [ ] Sprint 6 concluído (3/4 ou 4/4 tasks)
- [ ] 3 deploys estáveis pós-Sprint 6
- [ ] `kubectl --context <runtime> get ns` — namespaces de tenant existem
- [ ] `kubectl --context <runtime> get networkpolicy -A` — policies existentes funcionam
- [ ] Nenhum pod em CrashLoopBackOff nos namespaces de tenant
- [ ] Testes passando: `cd operators && go test ./...`

**Se qualquer item falhar → PARAR. Corrigir primeiro.**

---

## Tasks (serializadas — uma por vez)

### Task 7.1 — SecurityContext nos pods de workload (CRÍTICO)

**Problema:** `renderer.go:buildDeployment()` gera Deployments sem SecurityContext. Containers rodam como root.

**Arquivo:** `api/internal/manifests/renderer.go`

**Passo 1 — Adicionar structs de SecurityContext:**

Após a struct `k8sProbe` (linha 173), adicionar:

```go
type k8sPodSecurityContext struct {
	RunAsNonRoot *bool  `json:"runAsNonRoot,omitempty"`
	RunAsUser    *int64 `json:"runAsUser,omitempty"`
	RunAsGroup   *int64 `json:"runAsGroup,omitempty"`
	FSGroup      *int64 `json:"fsGroup,omitempty"`
	SeccompProfile *k8sSeccompProfile `json:"seccompProfile,omitempty"`
}

type k8sSeccompProfile struct {
	Type string `json:"type"`
}

type k8sContainerSecurityContext struct {
	AllowPrivilegeEscalation *bool `json:"allowPrivilegeEscalation,omitempty"`
	ReadOnlyRootFilesystem   *bool `json:"readOnlyRootFilesystem,omitempty"`
	RunAsNonRoot             *bool `json:"runAsNonRoot,omitempty"`
}

type k8sVolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

type k8sVolume struct {
	Name     string          `json:"name"`
	EmptyDir *k8sEmptyDir    `json:"emptyDir,omitempty"`
}

type k8sEmptyDir struct{}
```

**Passo 2 — Atualizar `k8sPodSpec` e `k8sContainer`:**

```go
type k8sPodSpec struct {
	SecurityContext    *k8sPodSecurityContext `json:"securityContext,omitempty"`
	ServiceAccountName string                `json:"serviceAccountName,omitempty"`
	Containers         []k8sContainer        `json:"containers"`
	Volumes            []k8sVolume           `json:"volumes,omitempty"`
}

type k8sContainer struct {
	Name            string                       `json:"name"`
	Image           string                       `json:"image"`
	Ports           []k8sContainerPort           `json:"ports,omitempty"`
	Resources       k8sResources                 `json:"resources"`
	Env             []k8sEnvVar                  `json:"env,omitempty"`
	EnvFrom         []k8sEnvFromSource           `json:"envFrom,omitempty"`
	ReadinessProbe  *k8sProbe                    `json:"readinessProbe,omitempty"`
	LivenessProbe   *k8sProbe                    `json:"livenessProbe,omitempty"`
	SecurityContext *k8sContainerSecurityContext  `json:"securityContext,omitempty"`
	VolumeMounts    []k8sVolumeMount             `json:"volumeMounts,omitempty"`
}
```

**Passo 3 — Atualizar `buildDeployment()`:**

No return do `k8sDeployment`, a seção `Spec.Template.Spec` deve ficar:

```go
Spec: k8sPodSpec{
	SecurityContext: &k8sPodSecurityContext{
		RunAsNonRoot: boolPtr(true),
		RunAsUser:    int64Ptr(1000),
		RunAsGroup:   int64Ptr(1000),
		FSGroup:      int64Ptr(1000),
		SeccompProfile: &k8sSeccompProfile{
			Type: "RuntimeDefault",
		},
	},
	ServiceAccountName: "loopzero-workload",
	Containers: []k8sContainer{container},
	Volumes: []k8sVolume{
		{Name: "tmp", EmptyDir: &k8sEmptyDir{}},
	},
},
```

E no container, antes de retornar:

```go
container.SecurityContext = &k8sContainerSecurityContext{
	AllowPrivilegeEscalation: boolPtr(false),
	ReadOnlyRootFilesystem:   boolPtr(true),
	RunAsNonRoot:             boolPtr(true),
}
container.VolumeMounts = []k8sVolumeMount{
	{Name: "tmp", MountPath: "/tmp"},
}
```

**Passo 4 — Adicionar helpers:**

```go
func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }
```

**Validação:**

```bash
cd api && go test ./internal/manifests/...
# Verificar YAML gerado contém securityContext
go test -run TestRender -v 2>&1 | grep -A5 securityContext
```

**Rollback:** Reverter as mudanças em `renderer.go`. Deployments existentes não são afetados (ArgoCD só aplica no próximo sync).

---

### Task 7.2 — PodSecurity Admission labels nos namespaces (CRÍTICO)

**Problema:** Namespaces de tenant não têm labels PSA. Pods privilegiados podem ser submetidos.

**Arquivo:** `operators/internal/controller/environment_controller.go`

**Mudança:** Na função `reconcileNamespace()` (linha 202-210), adicionar labels PSA:

```go
result, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, ns, func() error {
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels["loopzero.io/tenant"] = tenantName
	ns.Labels["loopzero.io/environment"] = env.Spec.Environment
	ns.Labels["loopzero.io/managed-by"] = "environment-controller"

	// PodSecurity Admission — enforce restricted profile
	// https://kubernetes.io/docs/concepts/security/pod-security-standards/
	ns.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
	ns.Labels["pod-security.kubernetes.io/enforce-version"] = "latest"
	ns.Labels["pod-security.kubernetes.io/audit"] = "restricted"
	ns.Labels["pod-security.kubernetes.io/warn"] = "restricted"
	return nil
})
```

**Por que `restricted`:**
- `restricted` é o nível mais restritivo do PSA
- Exige: runAsNonRoot, drop ALL capabilities, seccompProfile, readOnlyRootFilesystem
- Combina com o SecurityContext da Task 7.1
- Se a Task 7.1 não for feita antes, pods existentes vão ser rejeitados

**Ordem obrigatória:** Task 7.1 ANTES de Task 7.2.

**Validação:**

```bash
cd operators && go test ./internal/controller/... -run TestEnvironment
# No cluster:
kubectl --context <runtime> get ns <tenant-ns> -o jsonpath='{.metadata.labels}' | jq .
# Deve conter: "pod-security.kubernetes.io/enforce": "restricted"
```

**Rollback:** Remover as 4 linhas de labels PSA. O namespace continua funcionando sem enforce.

---

### Task 7.3 — NetworkPolicy obrigatória (ALTO)

**Problema:** `reconcileNetworkPolicies()` só executa se `env.Spec.NetworkPolicy.Isolate == true`. Sem essa flag, o namespace fica aberto.

**Arquivo:** `operators/internal/controller/environment_controller.go`

**Mudança:** Remover a condicional na linha 103:

**Antes:**
```go
// Step 3: Ensure NetworkPolicy (CreateOrUpdate)
if env.Spec.NetworkPolicy.Isolate {
	if err := r.reconcileNetworkPolicies(ctx, nsName); err != nil {
```

**Depois:**
```go
// Step 3: Ensure NetworkPolicy (CreateOrUpdate) — always enforced, not optional.
// Security invariant: every tenant namespace MUST have deny-all + selective allow.
{
	if err := r.reconcileNetworkPolicies(ctx, nsName); err != nil {
```

Ou seja, remover `if env.Spec.NetworkPolicy.Isolate` e sempre executar. Manter as chaves `{}` para não alterar o escopo da variável `err`.

**Validação:**

```bash
cd operators && go test ./internal/controller/... -run TestEnvironment
# No cluster — namespace sem flag Isolate agora deve ter policies:
kubectl --context <runtime> get networkpolicy -n <tenant-ns>
# Deve listar: deny-all, allow-ingress-traefik, allow-egress
```

**Rollback:** Restaurar o `if`. Policies existentes não são removidas.

---

### Task 7.4 — ServiceAccount dedicado para workloads (ALTO)

**Problema:** Pods usam SA `default` que pode ter token auto-montado e permissões herdadas.

**Arquivo 1:** `operators/internal/controller/environment_controller.go`

**Mudança:** Na função `reconcileRBAC()`, após criar o SA `loopzero-deployer` (linha 221-231), adicionar criação do SA de workload:

```go
// ServiceAccount for workload pods (minimal, no auto-mounted token)
workloadSA := &corev1.ServiceAccount{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "loopzero-workload",
		Namespace: nsName,
	},
}
if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, workloadSA, func() error {
	f := false
	workloadSA.AutomountServiceAccountToken = &f
	return nil
}); err != nil {
	return fmt.Errorf("serviceaccount %s/%s: %w", nsName, "loopzero-workload", err)
}
```

**Arquivo 2:** `api/internal/manifests/renderer.go`

Já coberto na Task 7.1 (`ServiceAccountName: "loopzero-workload"` no PodSpec).

**Validação:**

```bash
cd operators && go test ./internal/controller/... -run TestEnvironment
# No cluster:
kubectl --context <runtime> get sa -n <tenant-ns>
# Deve listar: default, loopzero-deployer, loopzero-workload
kubectl --context <runtime> get sa loopzero-workload -n <tenant-ns> -o yaml | grep automount
# Deve ser: automountServiceAccountToken: false
```

**Rollback:** Remover bloco de criação do SA. Pods continuam rodando com SA `default`.

---

### Task 7.5 — Restringir egress do PostgreSQL (MÉDIO)

**Problema:** PostgreSQL tem `egress: - {}` que permite tráfego para qualquer destino.

**Arquivo:** `infra/helm/network-policies/loopzero-system.yaml`

**Mudança:** Substituir o bloco de egress do PostgreSQL (linhas 175-183):

**Antes:**
```yaml
  egress:
    # DNS
    - ports:
        - port: 53
          protocol: UDP
        - port: 53
          protocol: TCP
    # Allow all egress for metrics and replication
    - {}
```

**Depois:**
```yaml
  egress:
    # DNS
    - ports:
        - port: 53
          protocol: UDP
        - port: 53
          protocol: TCP
    # Metrics scraping (Prometheus)
    - ports:
        - port: 9187
          protocol: TCP
    # PostgreSQL replication (HA peer)
    - to:
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: postgresql
      ports:
        - port: 5432
          protocol: TCP
```

**Validação:**

```bash
kubectl apply -f infra/helm/network-policies/loopzero-system.yaml
# Testar conectividade:
kubectl -n loopzero-system exec -it deploy/loopzero-api -- curl -sf localhost:3001/api/v1/health
# Se API responde, PostgreSQL continua acessível
```

**Rollback:** Restaurar `- {}` no egress. `kubectl apply -f` novamente.

---

### Task 7.6 — Restringir ingress do MinIO (MÉDIO)

**Problema:** Segunda regra de ingress do MinIO permite porta 9000 de qualquer origem.

**Arquivo:** `infra/helm/network-policies/loopzero-system.yaml`

**Mudança:** Adicionar comentário explicando o trade-off e, se em produção multi-cluster, manter como está. Se em single-cluster ou quando migrar para produção, adicionar CIDR do build cluster:

**Depois:**
```yaml
    # Cross-cluster: build cluster workflow pods access MinIO for cache.
    # In production, restrict to build cluster CIDR.
    # TODO(security): replace with ipBlock when build cluster IP range is known.
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: argo
      ports:
        - port: 9000
          protocol: TCP
    # Fallback for cross-cluster (Docker networking, not k8s service mesh).
    # Remove this rule when ipBlock is configured above.
    - ports:
        - port: 9000
          protocol: TCP
```

**Nota:** Em setup k3d local, cross-cluster vai por Docker networking e não tem namespace selector. Esta task é para documentar o risco e preparar a correção para produção. A regra aberta deve ser removida quando o `ipBlock` do build cluster for conhecido.

**Validação:**

```bash
kubectl apply -f infra/helm/network-policies/loopzero-system.yaml
# Disparar um build e verificar que cache restore/save ainda funciona
```

**Rollback:** Restaurar regra original.

---

### Task 7.7 — Enforce image digest no renderer (BAIXO)

**Problema:** `ImageRef` aceita tags (`:v1`, `:latest`). Tags são mutáveis — um atacante com acesso ao registry pode substituir a imagem.

**Arquivo:** `api/internal/manifests/renderer.go`

**Mudança:** Adicionar validação na função `Render()` ou em `buildDeployment()`:

```go
// buildDeployment — no início da função, após port default
if app.ImageRef != "" && !strings.Contains(app.ImageRef, "@sha256:") {
	// Log warning but don't block — digest enforcement is best-effort
	// until the build pipeline always emits digests.
	// TODO(security): make this a hard error when BuildKit digest parsing is stable.
}
```

**A correção real** está no pipeline de build: o `workflow_builder.go` já parseia o digest do metadata do BuildKit. Verificar que o digest é propagado até o `AppManifestInput.ImageRef`.

**Arquivo de verificação:** `api/internal/argo/workflow_builder.go` — procurar por `containerimage.digest`.

**Validação:**

```bash
# Verificar que builds recentes usam digest:
kubectl --context <build> get workflow -n build-<tenant> -o yaml | grep -A2 "image:"
# Deve mostrar: registry/tenant/project/app@sha256:abc123...
```

**Rollback:** N/A — é validação, não muda comportamento.

---

## Ordem de Execução

```
Task 7.1 (SecurityContext)        ← PRIMEIRO: base para PSA funcionar
    ↓
Task 7.4 (ServiceAccount)        ← Cria o SA que 7.1 referencia
    ↓
Task 7.2 (PSA labels)            ← Agora seguro: pods já compliance com restricted
    ↓
Task 7.3 (NetworkPolicy always)  ← Independente, mas valida após namespace existir
    ↓
Task 7.5 (PostgreSQL egress)     ← Infra, pode ser feito em paralelo com 7.3
    ↓
Task 7.6 (MinIO ingress)         ← Infra, documentação + prep para prod
    ↓
Task 7.7 (Image digest)          ← Baixa prioridade, pode ser última
```

**GATE entre 7.4 e 7.2:** Fazer deploy de um app de teste e verificar que o pod sobe com SecurityContext correto ANTES de ativar PSA enforce. Se PSA for ativado antes, pods existentes que não têm o SecurityContext serão rejeitados em qualquer reschedule.

---

## Critério de Conclusão (GATE-7)

- [ ] Todos os pods de workload rodam como UID 1000 (não root)
- [ ] `allowPrivilegeEscalation: false` em todos os containers de tenant
- [ ] `seccompProfile: RuntimeDefault` em todos os pods de tenant
- [ ] Namespaces de tenant têm `pod-security.kubernetes.io/enforce: restricted`
- [ ] NetworkPolicy existe em TODOS os namespaces de tenant (sem exceção)
- [ ] SA `loopzero-workload` existe com `automountServiceAccountToken: false`
- [ ] PostgreSQL sem egress `- {}`
- [ ] 3 deploys estáveis pós-hardening
- [ ] Testes passando: `cd api && go test ./...` e `cd operators && go test ./...`

---

## Referências

- [Kubernetes Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)
- [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/)
- [NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- Auditoria de segurança do cluster workload — 2026-03-26
