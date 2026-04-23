# pprof Profiling Runbook

## Overview

The LoopZero API and Operator expose [pprof](https://pkg.go.dev/net/http/pprof) endpoints on dedicated internal ports for on-demand profiling.

| Component | Port | Default |
|-----------|------|---------|
| API | `LOOPZERO_PPROF_ADDR` | `:6060` |
| Operator | `LOOPZERO_PPROF_ADDR` | `:6061` |

## Access via kubectl

```bash
# API profiling
kubectl port-forward -n platform-system deploy/loopzero-api 6060:6060

# Operator profiling
kubectl port-forward -n platform-system deploy/loopzero-controller 6061:6061
```

## Common profiles

### CPU profile (30 seconds)
```bash
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

### Heap / memory
```bash
go tool pprof http://localhost:6060/debug/pprof/heap
```

### Goroutines
```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

### Block profile (contention)
```bash
go tool pprof http://localhost:6060/debug/pprof/block
```

### Interactive web UI
```bash
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap
```

## Configuration

| Env var | Description | Default |
|---------|-------------|---------|
| `LOOPZERO_PPROF_ENABLED` | Enable/disable pprof server | `false` |
| `LOOPZERO_PPROF_ADDR` | Listen address | `:6060` (API), `:6061` (Operator) |

To enable for debugging:
```yaml
env:
  - name: LOOPZERO_PPROF_ENABLED
    value: "true"
```

## Security

- pprof is **disabled by default** — requires explicit `LOOPZERO_PPROF_ENABLED=true`
- Uses a **dedicated ServeMux** (not DefaultServeMux) to prevent handler pollution
- Runs on a SEPARATE port from the API and metrics ports
- NOT exposed via Traefik/Ingress — only accessible via `kubectl port-forward`
- Contains sensitive runtime information (heap, goroutines) — restrict access to operators
