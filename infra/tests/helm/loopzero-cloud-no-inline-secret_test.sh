#!/usr/bin/env bash
# Hermetic guard — install-config-contract Slice 2 T3.1.
# Enforces secrets-by-reference: NO secret key carries a real value in any committed values
# file, and no secret-shaped literal is committed. Self-checks that the scanner actually fires
# on a planted inline secret + does NOT false-positive on sentinels/empty. NO kubectl/network.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHART="$ROOT/charts/loopzero-cloud"

python3 - "$CHART" <<'PY'
import re, sys, os, tempfile
chart = sys.argv[1]
KEYS = ['STRIPE_API_KEY','STRIPE_WEBHOOK_SECRET','SESSION_TOKEN_SECRET','LOOPZERO_SERVICE_AUTH_SECRET','THEMORY_API_KEY']
key_re = re.compile(r'(' + '|'.join(KEYS) + r')\s*:\s*(\S.*)$')
secret_literal = re.compile(r'sk_(live|test)_[a-z0-9]|whsec_[a-z0-9]|\b[a-f0-9]{32,}\b', re.I)
SENTINEL = re.compile(r'!!changeme', re.I)

def scan(path):
    """Return list of (lineno, reason) for inline-secret violations."""
    viol = []
    for i, raw in enumerate(open(path), 1):
        line = raw.rstrip('\n')
        if line.lstrip().startswith('#'):
            continue
        m = key_re.search(line)
        if m:
            val = m.group(2).strip().strip('"').strip("'")
            if val and not SENTINEL.search(val):
                viol.append((i, f"secret key {m.group(1)} carries inline value"))
        if secret_literal.search(line) and not SENTINEL.search(line):
            viol.append((i, "secret-shaped literal committed"))
    return viol

fail = 0; pas = 0
def ok(): 
    global pas; pas += 1
def no(msg):
    global fail; fail += 1; print(f"  FAIL: {msg}")

# clean tree
for f in ['values.yaml','values.example.yaml']:
    p = os.path.join(chart, f)
    if not os.path.exists(p): continue
    v = scan(p)
    if v: no(f"inline secret in {f}: {v}")
    else: ok()

# positive self-check: planted secret MUST be flagged
t1 = tempfile.NamedTemporaryFile('w', suffix='.yaml', delete=False)
t1.write("config:\n  foo: bar\nSTRIPE_API_KEY: sk_live_planted123\n"); t1.close()
if scan(t1.name): ok()
else: no("scanner FAILED to flag a planted inline secret (guard is broken)")
os.unlink(t1.name)

# negative self-check: sentinels/empty MUST NOT trip
t2 = tempfile.NamedTemporaryFile('w', suffix='.yaml', delete=False)
t2.write('existingSecret: ""\nimage:\n  tag: "!!changeme-image-tag!!"\n'); t2.close()
if scan(t2.name): no("scanner false-positives on sentinel/empty")
else: ok()
os.unlink(t2.name)

print(f"loopzero-cloud-no-inline-secret: {pas} passed, {fail} failed")
sys.exit(1 if fail else 0)
PY
