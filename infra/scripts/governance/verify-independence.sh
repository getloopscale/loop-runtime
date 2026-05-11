#!/usr/bin/env bash
# verify-independence.sh — Validates persona independence config before T3.1 runs.
#
# Per T3.0 of status-validation-and-doc-consolidation-plan (EC-1 MUST FIX).
#
# Checks that the configured 3 personas have ≥2 of 3 dimensions varied:
#   - model (across all 3 personas)
#   - temperature (across all 3 personas)
#   - seed (across all 3 personas)
#
# Exit codes:
#   0 — independence verified
#   1 — independence violated (≤1 dimension varied)
#   2 — config file not found or unparseable

set -euo pipefail

CONFIG_FILE="${CONFIG_FILE:-docs/governance/personas/independence-config.yaml}"
USE_FALLBACK="${USE_FALLBACK:-false}"

usage() {
    cat <<EOF
Usage: $(basename "$0") [--fallback]

Verifies persona independence per ADR D1 of status-validation plan.

Options:
  --fallback  Validate the fallback config (used when codex unavailable)

Exit codes:
  0 = independence verified
  1 = independence violated
  2 = config error
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --fallback) USE_FALLBACK=true; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; exit 2 ;;
    esac
done

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "Config not found: $CONFIG_FILE" >&2
    exit 2
fi

export CONFIG_FILE
export USE_FALLBACK

python3 - <<'PYEOF'
import os
import sys
import yaml

config_file = os.environ['CONFIG_FILE']
use_fallback = os.environ.get('USE_FALLBACK', 'false') == 'true'

try:
    with open(config_file, 'r') as f:
        cfg = yaml.safe_load(f)
except Exception as e:
    print(f"Error parsing {config_file}: {e}", file=sys.stderr)
    sys.exit(2)

personas = cfg.get('personas', {})
if len(personas) != 3:
    print(f"Expected 3 personas, found {len(personas)}", file=sys.stderr)
    sys.exit(1)

# Collect dimensions
models = []
temps = []
seeds = []

for name, p in personas.items():
    if use_fallback and 'fallback' in p:
        eff = {**p, **p['fallback']}
    else:
        eff = p
    models.append(eff.get('model'))
    temps.append(eff.get('temperature'))
    seeds.append(eff.get('seed'))

dim_models = len(set(models))
dim_temps = len(set(temps))
dim_seeds = len(set(seeds))

print(f"Configured personas: {list(personas.keys())}")
print(f"Models varied: {dim_models}/3 ({set(models)})")
print(f"Temperatures varied: {dim_temps}/3 ({set(temps)})")
print(f"Seeds varied: {dim_seeds}/3 ({set(seeds)})")

# Count "varied" dimensions (need ≥2 distinct values per dimension)
varied_count = 0
varied_names = []
if dim_models >= 2:
    varied_count += 1
    varied_names.append('model')
if dim_temps >= 2:
    varied_count += 1
    varied_names.append('temperature')
if dim_seeds >= 2:
    varied_count += 1
    varied_names.append('seed')

print(f"\nDimensions with ≥2 distinct values: {varied_count}/3 ({varied_names})")

min_required = cfg.get('validation', {}).get('min_dimensions_varied', 2)
if use_fallback:
    min_required = cfg.get('validation_fallback', {}).get('min_dimensions_varied', 2)

if varied_count < min_required:
    print(f"\nFAIL — only {varied_count} dimension(s) varied; need ≥{min_required}", file=sys.stderr)
    print(f"\nRemediation: edit {config_file} to make ≥{min_required} of (model, temperature, seed) distinct across all 3 personas.", file=sys.stderr)
    sys.exit(1)

# Check cross-vendor for max confidence cap
has_cross_vendor = any(
    p.get('subagent_type', '').startswith('codex:')
    for p in personas.values()
)
print(f"\nCross-vendor (codex) configured: {has_cross_vendor}")
if not has_cross_vendor:
    print("Note: Using Claude-only family. Confidence cap should be 90-95% (not 95-97%) per validation_fallback.warning.")

print("\nPASS — independence verified.")
sys.exit(0)
PYEOF
