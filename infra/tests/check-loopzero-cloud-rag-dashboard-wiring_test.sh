#!/usr/bin/env bash
# T1+T2 hermetic gate — loopzero-cloud dev-public MUST wire RAG dashboard durably.
#
# Root cause (delegated brief 2026-07-15 DELEGATION-M7-rag-ui.md):
#   `RAG_BASE_URL` was empty on loopzero-cloud pod in dev-public → broker nil →
#   mountRagDashboard (cmd/main.go:998) skipped → `/v1/dashboard/rag/*` 404.
#
# Contract asserted by this test (helmfile-templated dev-public Deployment):
#   - env RAG_BASE_URL is set to a non-empty http(s):// URL
#   - env RAG_PLATFORM_ADMIN_KEY is sourced via valueFrom.secretKeyRef
#     (name=loopzero-rag-admin-key, optional=true)
#   - ExternalSecret template file exists in the chart
#   - Rendered ExternalSecret targets Secret loopzero-rag-admin-key, remoteRef
#     key=loopzero/cloud property=rag_platform_admin_key
#
# Runs against `helmfile template` output — hermetic, no live cluster required.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "=== Rendering helmfile dev-public loopzero-cloud... ==="
LOOPZERO_HELMFILE_ENV=dev-public helmfile -e dev-public \
  -f infra/helmfile/mgmt.helmfile.yaml.gotmpl \
  --selector name=loopzero-cloud \
  template > "$TMP/rendered.yaml" 2> "$TMP/err.log" || {
    echo "FAIL — helmfile template errored"; cat "$TMP/err.log"; exit 1
  }

fail=0

# Multi-line asserts via python3 YAML parsing — robust vs regex fragility.
python3 - "$TMP/rendered.yaml" <<'PY'
import sys, yaml, pathlib
p = pathlib.Path(sys.argv[1])
docs = [d for d in yaml.safe_load_all(p.read_text()) if d]

# Assertions
fails = []

def find_kind(kind, name=None):
    for d in docs:
        if d.get("kind") == kind and (name is None or d.get("metadata", {}).get("name") == name):
            return d
    return None

def envs_of_deployment():
    dep = find_kind("Deployment", "loopzero-cloud")
    if not dep:
        fails.append("FAIL — Deployment/loopzero-cloud not rendered")
        return {}
    containers = dep["spec"]["template"]["spec"]["containers"]
    ct = next((c for c in containers if c["name"] == "loopzero-cloud"), None)
    if not ct:
        fails.append("FAIL — container loopzero-cloud not in Deployment spec")
        return {}
    return {e["name"]: e for e in ct.get("env", [])}

envs = envs_of_deployment()

# A1: RAG_BASE_URL literal env, non-empty http(s):// URL
if "RAG_BASE_URL" not in envs:
    fails.append("FAIL — RAG_BASE_URL env absent from Deployment")
else:
    val = envs["RAG_BASE_URL"].get("value", "")
    if not val.startswith(("http://", "https://")):
        fails.append(f"FAIL — RAG_BASE_URL value not http(s):// URL (got '{val}')")
    else:
        print(f"PASS — RAG_BASE_URL={val}")

# A2: RAG_PLATFORM_ADMIN_KEY via valueFrom.secretKeyRef
if "RAG_PLATFORM_ADMIN_KEY" not in envs:
    fails.append("FAIL — RAG_PLATFORM_ADMIN_KEY env absent from Deployment")
else:
    ent = envs["RAG_PLATFORM_ADMIN_KEY"]
    vf = ent.get("valueFrom", {}).get("secretKeyRef")
    if not vf:
        fails.append("FAIL — RAG_PLATFORM_ADMIN_KEY not sourced via valueFrom.secretKeyRef")
    else:
        if vf.get("name") != "loopzero-rag-admin-key":
            fails.append(f"FAIL — secretKeyRef.name is '{vf.get('name')}', expected loopzero-rag-admin-key")
        if vf.get("key") != "RAG_PLATFORM_ADMIN_KEY":
            fails.append(f"FAIL — secretKeyRef.key is '{vf.get('key')}', expected RAG_PLATFORM_ADMIN_KEY")
        if not vf.get("optional", False):
            fails.append("FAIL — secretKeyRef.optional MUST be true (preserves boot on clusters without ESO)")
        else:
            print(f"PASS — RAG_PLATFORM_ADMIN_KEY sourced via secretKeyRef name={vf['name']} key={vf['key']} optional=true")

# A3: ExternalSecret template file present in chart
tpl = pathlib.Path("infra/helm/loopzero-cloud/templates/externalsecret-rag-admin-key.yaml")
if not tpl.is_file():
    fails.append(f"FAIL — ExternalSecret template missing at {tpl}")
else:
    print(f"PASS — ExternalSecret template present at {tpl}")

# A4: Rendered ExternalSecret targets Secret loopzero-rag-admin-key
es = find_kind("ExternalSecret", "loopzero-rag-admin-key")
if not es:
    fails.append("FAIL — ExternalSecret/loopzero-rag-admin-key not rendered (rag.externalSecretEnabled?)")
else:
    tgt = es["spec"].get("target", {}).get("name")
    if tgt != "loopzero-rag-admin-key":
        fails.append(f"FAIL — ExternalSecret target.name is '{tgt}', expected loopzero-rag-admin-key")
    data = es["spec"].get("data", [])
    if not any(d.get("secretKey") == "RAG_PLATFORM_ADMIN_KEY"
               and d.get("remoteRef", {}).get("key") == "loopzero/cloud"
               and d.get("remoteRef", {}).get("property") == "rag_platform_admin_key"
               for d in data):
        fails.append("FAIL — ExternalSecret data entry mismatch (expected secretKey=RAG_PLATFORM_ADMIN_KEY, remoteRef.key=loopzero/cloud, property=rag_platform_admin_key)")
    else:
        print("PASS — ExternalSecret targets Secret loopzero-rag-admin-key from OpenBao loopzero/cloud.rag_platform_admin_key")

# A5: ExternalSecret creationPolicy Owner (ESO takes ownership; delete ExternalSecret → deletes Secret; anti-orphan)
if es and es["spec"].get("target", {}).get("creationPolicy") != "Owner":
    fails.append("FAIL — ExternalSecret target.creationPolicy MUST be Owner (prevents orphan Secret on manifest deletion)")
elif es:
    print("PASS — ExternalSecret target.creationPolicy=Owner")

if fails:
    print()
    for f in fails: print(f)
    print(f"\n=== RED: {len(fails)} assertion(s) failed ===")
    sys.exit(1)
print("\n=== GREEN: loopzero-cloud dev-public wires RAG dashboard durably ===")
PY
