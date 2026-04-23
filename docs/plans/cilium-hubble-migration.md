# Plan: Cilium + Hubble Network Observability Migration

> **Status:** DEFERRED — post-launch  
> **Tier:** T3 (CNI migration across 3 clusters — requires meeting + ADR)  
> **Created:** 2026-04-22

## Context

The LoopZero PaaS currently uses default Kubernetes CNI with traditional NetworkPolicy. For a multi-cluster/multi-cell architecture, flow-level network visibility is needed to diagnose retries, timeouts, policy drops, egress failures, and cross-cell asymmetries.

Cilium as CNI + Hubble provides L3/L4/L7 network observability transparently, without application instrumentation.

## Why Deferred

- CNI migration is high-risk infrastructure (potential downtime during migration)
- Requires re-provisioning all 3 DOKS clusters or in-place CNI swap
- Pre-release with few tenants doesn't justify the risk
- Current NetworkPolicy provides adequate isolation

## Migration Phases (when ready)

### Phase A: Validation
- Deploy Cilium on a new test DOKS cluster
- Validate NetworkPolicy compatibility (Cilium implements K8s NetworkPolicy natively)
- Benchmark performance vs default CNI

### Phase B: Workload Cluster
- Lowest risk — only runs tenant applications
- Drain → swap CNI → re-deploy → validate
- Enable Hubble for service map and flow visibility

### Phase C: Build Cluster
- Swap CNI on build cluster
- Validate Argo Workflows pod networking
- Enable egress flow monitoring

### Phase D: Management Cluster
- Highest risk — runs all control plane services
- Swap CNI on mgmt cluster
- Enable Hubble UI for cross-cluster flow visualization

## Hubble Dashboards (planned)

1. **Service Map** — real-time service dependency graph
2. **Flow Visibility** — L3/L4 flow table with drop/forward/policy verdicts
3. **DNS Observability** — DNS query/response visibility
4. **HTTP L7** — request/response metadata for instrumented protocols
5. **Policy Audit** — NetworkPolicy enforcement visibility (allow/deny)

## Prerequisites

- DigitalOcean support for custom CNI on DOKS (verify current support)
- Cilium 1.16+ for Kubernetes 1.32 compatibility
- Hubble relay + UI deployment
- Grafana integration via Hubble Prometheus metrics exporter

## Risks

- In-place CNI swap may cause brief connectivity loss
- Cilium kernel requirements (eBPF) — verify DOKS kernel version
- Increased resource consumption vs basic CNI
- Operational complexity (Cilium operator, Hubble relay)
