# Dogfood 2026-04-29 Repro Suite

Scripts e testes que reproduzem os 13 achados do dogfood
[`docs/plans/dogfood-2026-04-29-remediation-plan.md`](../../../docs/plans/dogfood-2026-04-29-remediation-plan.md)
contra k3d standalone-multi.

## Uso

```bash
task dogfood:repro                                  # Run completo + summary
bash infra/tests/livetest/dogfood/repro-findings.sh --json   # Apenas JSON (CI)
```

## Output Schema

```json
{
  "findings": [
    {
      "id": "F-01",
      "severity": "S0",
      "status": "REPRODUCED" | "FIXED" | "INDETERMINATE",
      "evidence": "..."
    }
  ],
  "summary": {
    "total": 13,
    "reproduced": 10,
    "fixed": 0,
    "indeterminate": 3
  }
}
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Todos os achados ainda REPRODUZEM (matriz consistente, nenhum fix mergeado) |
| 1 | Pelo menos 1 achado FIXED — atualize a regression matrix e o CHANGELOG |
| 2 | Erro de pre-condição (k3d ausente, kubectl falha) |

## Status `INDETERMINATE`

Alguns checks dependem de outros (cascata). Por exemplo, F-07 (`/api/v1/version` retorna 404)
não pode ser validado se F-01 (API inalcançável) ainda reproduz, porque a connect-refused
mascara a 404. Quando F-01 for fixed, F-07 fica determinístico.

## Mapeamento Achado → Check Function

| Finding | Check | Severity | Categoria |
|---------|-------|----------|-----------|
| F-01 | `check_F01` | S0 | Infra (NodePort/IngressRoute) |
| F-02 | `check_F02` | S1 | CLI (exit code drift) |
| F-03 | `check_F03` | S2 | CLI (Commander parse) |
| F-04 | `check_F04` | S1 | Operator (Tenant ns) |
| F-05 | `check_F05` | S1 | CLI (init silent-success) |
| F-06 | `check_F06` | S3 | CLI (logout msg) |
| F-07 | `check_F07` | S2 | API (`/version`) |
| F-08 | `check_F08` | S3 | Observability (`/metrics`) |
| F-09 | `check_F09` | S2 | Operator (pull secret loop) |
| F-10 | `check_F10` | S2 | Operator (OTel exporter) |
| F-11 | `check_F11` | S2 | ArgoCD (sharding/Lua) |
| F-12 | `check_F12` | S2 | API (ArgoCD auth) |
| F-13 | `check_F13` | S3 | Argo Workflows (podGC) |
