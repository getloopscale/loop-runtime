# ADR: securityContext exception for nfs-server-provisioner

## Status

Accepted — 2026-05-03

## Context

`nfs-server-provisioner` runs an NFS daemon inside a pod that exports a Kubernetes-managed filesystem to clients (RWX PVCs). The NFS kernel module + filesystem operations require:

- **Root privileges** for `mount` syscall and `nfsd` kernel module.
- **Writable root filesystem** because the NFS server writes export tables (`/etc/exports`) and runtime state.
- **`SYS_ADMIN` and `SETPCAP` capabilities** for namespace operations.
- **`hostNetwork: true`** in some configurations to advertise the NFS service.

These requirements are inherent to the NFS protocol implementation and cannot be satisfied while enforcing `runAsNonRoot: true` + `readOnlyRootFilesystem: true` + `drop: [ALL]`. Kubernetes upstream documentation and the nfs-server-provisioner chart maintainers confirm this trade-off.

## Decision

We DO NOT enforce the standard hardening profile (`runAsNonRoot: true`, `readOnlyRootFilesystem: true`, `drop: [ALL]`, `allowPrivilegeEscalation: false`) for `nfs-server-provisioner`.

Specifically:

- `runAsNonRoot: false` (must run as root for `nfsd`)
- `readOnlyRootFilesystem: false` (writes `/etc/exports`, runtime state)
- `capabilities` not dropped — `SYS_ADMIN` retained
- `allowPrivilegeEscalation: true` allowed

## Consequences

### Runtime risk

A compromise of the NFS provisioner pod gives an attacker root inside the pod's namespace, with kernel-level capabilities (`SYS_ADMIN`). Since we use it for shared-volume RWX provisioning in the runtime cluster (not management or build), the blast radius is limited to:

- The runtime cluster's data plane (tenant workloads).
- Files exported via NFS (which already include user-app data).

### Mitigations

1. **Namespace isolation.** `nfs-server-provisioner` runs in dedicated namespace `nfs-system` (or equivalent), separated from tenant namespaces by NetworkPolicy.
2. **PSA `privileged` only on this namespace.** Other namespaces remain `restricted`/`baseline`.
3. **Host-level controls.** Runtime cluster nodes are managed by DigitalOcean (DOKS); SSH access locked to platform team; node-level audit logs forwarded to Loki.
4. **NFS export restriction.** Provisioner only exports to in-cluster IPs (no external access). NetworkPolicy restricts ingress to NFS ports (2049/tcp, 111/tcp) from tenant-namespace CIDR only.
5. **Resource limits.** CPU/memory limits prevent resource exhaustion attacks via NFS.
6. **Image source.** The upstream chart's default image, pulled from its vendor registry. This item used to say "pulled from Harbor, signed by cosign"; neither held — no values override pointed the image at Harbor, and Harbor was retired on 2026-09-25 (docs/decisions.md, "Retire Harbor").

### Review trigger

Revisit this exception when ANY of:

- The NFS provisioner is replaced (e.g., migration to Longhorn, OpenEBS, or ROOK-Ceph for RWX).
- A vulnerability in the NFS protocol or `nfs-ganesha`/`nfsd` (the typical implementations) is announced.
- Tenant workloads no longer require RWX volumes (then the provisioner can be removed entirely).
- Sigstore/cosign keyless flow is fully cut over (INF-G16) — re-evaluate if image attestation strengthens the trust model enough to relax some hardening.

## References

- INF-G08: Secure Defaults gate (this exception is documented per gate's "documented and explicit" criterion).
- ADR D7 of the infra SOTA hardening plan: audit-first / fix-or-document approach. (The path `infra-sota-hardening-plan` cited here until 2026-09-11 exists in no commit.)
- Kubernetes NFS provisioner upstream: https://github.com/kubernetes-sigs/nfs-ganesha-server-and-external-provisioner
