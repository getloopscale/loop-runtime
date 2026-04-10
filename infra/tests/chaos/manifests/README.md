# LoopZero — Chaos Mesh Manifests

Real fault injection for the LoopZero platform using
[Chaos Mesh](https://chaos-mesh.org/) (CNCF incubating), replacing
the previous `PIt` Ginkgo stubs that were documentation-only.

## Why Chaos Mesh vs LitmusChaos

Both are CNCF projects. Chaos Mesh wins on:

- **Pure Kubernetes CRDs** — every experiment is a YAML file, no
  external operators writing YAML for you. Fits our GitOps model.
- **Mature dashboard** — Grafana-style UI for experiment management,
  lineage, and steady-state visualization.
- **Richer fault types** — `IOChaos` for filesystem latency,
  `TimeChaos` for clock skew, `NetworkChaos` for partitions + latency
  + corruption, `StressChaos` for CPU/memory pressure.
- **Scheduling** — built-in `Schedule` CRD for recurring chaos.

LitmusChaos is workflow-first (Argo Workflow plugin) which is fine
but adds an abstraction layer over the CRDs. We prefer the raw CRD
model.

## Installation

```bash
# Once per cluster — installs Chaos Mesh in the chaos-mesh namespace.
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace=chaos-mesh --create-namespace \
  --version 2.7.0 \
  --set dashboard.create=true \
  --set dashboard.securityMode=true
```

## Running an experiment

```bash
# Apply one experiment
kubectl apply -f pod-kill-controller.yaml

# Watch the experiment status
kubectl describe podchaos pod-kill-controller -n platform-system

# Delete when done
kubectl delete -f pod-kill-controller.yaml
```

## Experiments

Every manifest follows the same structure:

1. **`metadata.annotations.loopzero.io/hypothesis`** — the steady-state
   hypothesis in plain English ("API `/health` returns 200 during
   the experiment").
2. **`spec.duration`** — how long to hold the fault. Short (60-120s)
   so experiments stay runnable in CI.
3. **`spec.selector`** — which pods are targeted.
4. **`loopzero.io/slo-query`** annotation — a PromQL query the test
   runner polls to verify the steady-state holds.

| File | Fault | SLO | Roadmap |
|---|---|---|---|
| `pod-kill-controller.yaml` | Kill loopzero-controller pod | API stays up, builds complete | 02 `TestController_CrashRecovery` |
| `pod-kill-api-leader.yaml` | Kill loopzero-api leader pod | Non-leader takes over < 90s | 01 D/F |
| `pg-network-latency.yaml` | Inject 500ms latency to PG | API timeouts stay < 30s | 04 PG saturation |
| `argocd-crash.yaml` | Kill argocd-server | Other cells unaffected | 08 Cell ArgoCD crash |
| `build-cluster-network-partition.yaml` | Isolate build cluster | Builds queue gracefully | 08 Cell network partition |
| `gitea-outage.yaml` | Drop traffic to Gitea | Builds fail fast with clear error | 08 Gitea unavailable |
| `pg-connection-storm.yaml` | StressChaos on PG pool | Circuit breaker fires | 07 PG circuit breaker |
| `node-unschedulable.yaml` | Cordon build node | Workflows reschedule | 08 Build cluster node failure |

## Steady-state validation

Each experiment is paired with a `steadystate-<name>.yaml` Prometheus
alert that fires IF the SLO is violated during the experiment window.
The `chaos_test.go` Ginkgo specs apply the experiment, wait for its
duration, query the alert state, and fail if the alert ever fired.

```go
// Pseudo-code from chaos_test.go
It("survives controller kill", func() {
  applyChaos("pod-kill-controller.yaml")
  defer deleteChaos("pod-kill-controller.yaml")

  time.Sleep(120 * time.Second)

  // Assert no alert fired during the window
  Expect(checkAlertState("loopzero-api-health")).To(Equal("inactive"))
  Expect(checkAlertState("loopzero-build-success-rate")).To(Equal("inactive"))
})
```

## Safety

- **`duration` is always bounded** (never "forever") so orphaned
  experiments self-expire.
- **`selector.mode: fixed-percent`** caps blast radius to a known
  fraction of pods.
- **NEVER run in production** — these are scoped to the dev/staging
  platforms only. The runner Job validates `cluster.name != "prod"`
  before applying any manifest.

## References

- Chaos Mesh docs: https://chaos-mesh.org/docs/
- CNCF Chaos Engineering principles: https://principlesofchaos.org/
- iskorotkov/chaos-framework (YAML DSL inspiration): https://github.com/iskorotkov/chaos-framework
