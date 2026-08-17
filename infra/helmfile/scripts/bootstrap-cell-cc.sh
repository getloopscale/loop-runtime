#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
#
# Canonical bootstrap-cell-cc — consolidates 6 prior scripts into 1
# parameterized by --cell <id> --env <name>.
#
# Plan v1.2 T4.2 / D3 (scripts-param-by-env): consolidates os scripts por-cell que
# existiam antes (`bootstrap-cell-{default,1,2}-cc.sh` e as variantes `-drill`), hoje
# todos deletados.
#
# ## Cell ↔ cluster: 1:1, e cada cluster se chama pela cell que hospeda
#
#   | Cell ID  | k3d local           | drill DOKS                   |
#   |----------|---------------------|------------------------------|
#   | cell-0   | k3d-loopzero-cell-0     | runtimeCluster (pulumi key)  |
#   | cell-1   | k3d-loopzero-cell-1     | runtimeCluster2 (pulumi key) |
#
# ESTE CABEÇALHO ESTAVA MENTINDO, e o modo como mentia é o registro mais claro do risco
# que o `B-001` nomeia (*"substituição global cega destrói o repositório"*). Uma
# substituição de `cell-default` → `cell-0` sobre o texto anterior produziu:
#
#   - duas linhas da tabela com o MESMO id (`cell-0` e `cell-0`), porque `cell-default`
#     e `cell-0` eram linhas distintas que colapsaram numa só;
#   - dois nomes de arquivo repetidos na lista de substituídos, pela mesma razão;
#   - a frase *"cell-0 reuses cell-0's cluster"*, que não afirma nada;
#   - `Cell-2` e `runtime-2` citados na prosa — nenhum dos dois existe;
#   - e o pior: `--cell <default|1|2>` no Usage, mandando o operador passar valores que
#     o código **rejeita** (`case "$CELL"` aceita `0` e `1`, e mais nada).
#
# O texto acima agora descreve o que o código faz. A relação 2:1 (`cell-default` + `cell-1`
# no mesmo cluster) foi eliminada pelo `B-005`, e `DefaultCellID` vale `cell-0`
# (`api/internal/placement/cells.go:30`).
#
# Usage:
#   bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell <0|1> --env <dev|drill|prod|standalone|standalone-multi> [--dry-run]
#
# Examples:
#   # Local k3d, cell-0 → cluster loopzero-cell-0:
#   bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell 0 --env standalone-multi
#
#   # Drill DOKS, cell-1 → runtimeCluster2:
#   # `mgmt.yaml` aqui NÃO é resíduo do rename: é o nome do arquivo que o drill escreve
#   # hoje (`drill-helmfile-bootstrap.sh:143`, `evidence-capture.sh:221`). A metade drill
#   # não foi renomeada, e trocar o nome AQUI sem trocar lá quebraria o exemplo.
#   DRILL_KUBECONFIG=/tmp/drill-kc/mgmt.yaml \
#     bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell 1 --env drill
#
# Idempotent. Shellcheck clean.
set -euo pipefail

usage() {
  cat <<USAGE
Usage: $0 --cell <default|1|2> --env <dev|drill|prod|standalone|standalone-multi> [--dry-run]

Bootstraps a ClusterCredential CR + bearer-token Secret for the given
(cell, env) pair. See plan v1.2 T4.2 + EC-4 mapping table.
USAGE
  exit 1
}

CELL=""
ENV=""
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cell)
      CELL="$2"
      shift 2
      ;;
    --env)
      ENV="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      echo "[bootstrap-cell-cc] unknown arg: $1" >&2
      usage
      ;;
  esac
done

# --- Arg validation ---
if [[ -z "$CELL" ]] || [[ -z "$ENV" ]]; then
  echo "[bootstrap-cell-cc] --cell and --env are required" >&2
  usage
fi

# Uma cell por cluster. O conjunto era `default, 1, 2` — tres nomes para duas maquinas,
# porque `default` e `1` mapeavam ambos para o mesmo cluster. A unificacao 1:1 de
# 2026-08-09 (ADR docs/decisions/2026-08-09-cell-cluster-unification-adr.md) eliminou a
# duplicata; `default` nao existe mais como cell.
case "$CELL" in
  0|1) ;;
  *)
    echo "[bootstrap-cell-cc] --cell must be one of: 0, 1 (got: $CELL)" >&2
    exit 1
    ;;
esac

case "$ENV" in
  dev|drill|prod|standalone|standalone-multi) ;;
  *)
    echo "[bootstrap-cell-cc] --env must be one of: dev, drill, prod, standalone, standalone-multi (got: $ENV)" >&2
    exit 1
    ;;
esac

CELL_ID="cell-${CELL}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INFRA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# --- Env-specific resolution ---
case "$ENV" in
  drill|prod)
    # Cloud DOKS env — use Pulumi-backed lib
    LIB="$INFRA_ROOT/scripts/lib/bootstrap-cell-cc-drill-common.sh"
    if [[ ! -f "$LIB" ]]; then
      echo "[bootstrap-cell-cc] library not found: $LIB" >&2
      exit 1
    fi
    # shellcheck source=../../scripts/lib/bootstrap-cell-cc-drill-common.sh disable=SC1091
    source "$LIB"

    # Resolve Pulumi output key per EC-4 mapping
    # As chaves de output do Pulumi NAO mudam: sao contrato externo de codigo legado,
    # vivo ate o corte AWS. So o lado da cell foi renumerado.
    case "$CELL" in
      0) PULUMI_KEY="runtimeCluster" ;;
      1) PULUMI_KEY="runtimeCluster2" ;;
    esac

    echo "[bootstrap-cell-cc] env=$ENV cell=$CELL_ID → Pulumi output key=$PULUMI_KEY"
    bootstrap_cell_cc_drill "$CELL_ID" "$PULUMI_KEY" "$DRY_RUN"
    ;;

  dev|standalone|standalone-multi)
    # Local k3d env — use docker-inspect-backed lib
    LIB="$INFRA_ROOT/scripts/lib/bootstrap-cell-cc-common.sh"
    if [[ ! -f "$LIB" ]]; then
      echo "[bootstrap-cell-cc] library not found: $LIB" >&2
      exit 1
    fi
    # shellcheck source=../../scripts/lib/bootstrap-cell-cc-common.sh disable=SC1091
    source "$LIB"

    # Resolve k3d cluster name per EC-4 mapping (verified against B82)
    # O nome do cluster E o da cell — era aqui que a relacao 2:1 vivia em shell.
    case "$CELL" in
      0) K3D_CLUSTER="cell-0" ;;
      1) K3D_CLUSTER="cell-1" ;;
    esac

    echo "[bootstrap-cell-cc] env=$ENV cell=$CELL_ID → k3d cluster=$K3D_CLUSTER"
    bootstrap_cell_cc "$CELL_ID" "$K3D_CLUSTER" "$DRY_RUN"
    ;;
esac
