# ADR: securityContext exception for promtail

## Status

Accepted — 2026-05-03

## Context

Promtail is a Loki agent that ships container logs from each Kubernetes node to the Loki backend. To collect logs from ALL pods on a node (including ones running as different UIDs), promtail must:

- **Read `/var/log/pods/*/*/*.log`** — owned by various UIDs; effectively requires `hostPath` access and read permissions on container log files.
- **Read `/var/log/containers/*.log`** — symlinks to the above, owned by root in many distros.
- **Read kubelet PIDs** to correlate logs with pod metadata (some configurations).
- **Run as DaemonSet on every node** with hostPath mounts.

The standard hardening profile (`runAsNonRoot: true`, `readOnlyRootFilesystem: true`, `drop: [ALL]`) breaks log collection in several ways:

1. **`runAsNonRoot: true`** — log files are owned by root with mode 0600 in many distros; non-root promtail cannot read them.
2. **`readOnlyRootFilesystem: true`** — promtail writes a positions file (state tracking which line was last shipped) which must persist across pod restarts. While this CAN be redirected to a hostPath volume, in practice the upstream chart writes to a tmp path that requires write access.
3. **`drop: [ALL]`** — `DAC_READ_SEARCH` is needed to bypass DAC permissions on log files owned by other UIDs.

## Decision

We DO NOT enforce the standard hardening profile for promtail.

Specifically:

- `runAsNonRoot: false` (or `runAsUser: 0` to read root-owned log files)
- `readOnlyRootFilesystem: false` (or `true` with explicit `/tmp` emptyDir for positions file)
- `capabilities.add: [DAC_READ_SEARCH]` and `drop: [ALL]` other than that
- `hostPath: /var/log` mounted read-only

## Consequences

### Runtime risk

A compromise of the promtail pod allows an attacker to:

- Read any file under `/var/log` on the node (limited blast radius — logs are typically not secret in themselves).
- Potentially read other logs (kernel, system services) if mount expands.
- Use `DAC_READ_SEARCH` to read other files on the node hostPath if mount is broadened.

### Mitigations

1. **HostPath mount is read-only** — `readOnly: true` on the `/var/log` volumeMount. Promtail cannot write to host filesystem.
2. **DaemonSet runs in `loki-system` namespace** with PSA `privileged` ONLY for that namespace.
3. **NetworkPolicy egress** — promtail only allowed to reach Loki service in the same cluster. Cannot exfiltrate to external endpoints.
4. **Image source** — the upstream chart's default image, pulled from its vendor registry. This item used to say "pulled from Harbor, signed by cosign"; neither held — no values override pointed the image at Harbor, and Harbor was retired on 2026-09-25 (docs/decisions.md, "Retire Harbor").
5. **Resource limits** — CPU/memory limits prevent DoS via hostPath read flooding.
6. **Capability minimization** — only `DAC_READ_SEARCH` added; `SYS_ADMIN` and other dangerous caps NOT granted.

### Review trigger

Revisit this exception when ANY of:

- Loki/Grafana releases an alternative log-collection mechanism that runs as non-root (e.g., a privileged sidecar that reads logs and forwards to non-root promtail via Unix socket).
- Kubernetes 1.34+ introduces a way to read logs without hostPath (e.g., kubelet log-streaming API).
- A vulnerability in promtail itself is announced.
- We migrate to a non-promtail log shipper (Vector, Fluent Bit) that supports rootless operation by default.

## References

- INF-G08: Secure Defaults gate.
- ADR D7 of the infra SOTA hardening plan. (The path `infra-sota-hardening-plan` cited here until 2026-09-11 exists in no commit.)
- Promtail upstream: https://grafana.com/docs/loki/latest/clients/promtail/
- Kubernetes hostPath docs: https://kubernetes.io/docs/concepts/storage/volumes/#hostpath
