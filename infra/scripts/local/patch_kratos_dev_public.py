#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
"""Kratos ConfigMap transform for patch-kratos-dev-public.sh (de-costumed from
the inline python3 heredoc per plan home-unhomed-logic-suspect-keeps T2.2).

Per ADR D2 the kubectl get/apply I/O stays in the bash orchestrator; this
module is a pure transform: it reads the live `kratos.yaml` from STDIN, applies
the 7 dev-public patches, and writes to STDOUT either:

  - DRY_RUN=1 → a unified diff (live → desired), or "no-op" when already in state
  - DRY_RUN=0 → the full patched ConfigMap manifest YAML (for `kubectl apply -f -`)

Env: GH_ID/GH_SEC/GG_ID/GG_SEC/RESEND/KRATOS_PUBLIC_URL/DASHBOARD_URL/CLOUD_URL/
     COOKIE_DOMAIN/KRATOS_NAMESPACE/DRY_RUN
"""
import difflib
import os
import sys

import yaml


def main() -> int:
    ns = os.environ["KRATOS_NAMESPACE"]
    dry = os.environ.get("DRY_RUN", "0") == "1"

    raw = sys.stdin.read()
    cfg = yaml.safe_load(raw)

    # Patch 1 — OIDC providers
    keys = {
        "github": (os.environ["GH_ID"], os.environ["GH_SEC"]),
        "google": (os.environ["GG_ID"], os.environ["GG_SEC"]),
    }
    for p in cfg["selfservice"]["methods"]["oidc"]["config"].get("providers", []):
        if p["id"] in keys:
            cid, sec = keys[p["id"]]
            p["client_id"] = cid
            p["client_secret"] = sec

    # Patch 2 — OIDC base_redirect_uri
    cfg["selfservice"]["methods"]["oidc"]["config"]["base_redirect_uri"] = (
        os.environ["KRATOS_PUBLIC_URL"].rstrip("/") + "/"
    )

    # Patch 3 — Resend Authorization
    cfg["courier"]["http"]["request_config"]["headers"]["Authorization"] = (
        "Bearer " + os.environ["RESEND"]
    )

    # Patch 4 — serve.public
    cfg.setdefault("serve", {}).setdefault("public", {})
    cfg["serve"]["public"]["base_url"] = os.environ["KRATOS_PUBLIC_URL"].rstrip("/") + "/"
    cfg["serve"]["public"].setdefault("port", 4433)
    cfg["serve"]["public"]["cors"] = {
        "enabled": True,
        "allow_credentials": True,
        "allowed_origins": [os.environ["DASHBOARD_URL"], os.environ["CLOUD_URL"]],
        "allowed_methods": ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"],
        "allowed_headers": ["Authorization", "Content-Type", "Cookie", "X-Session-Token"],
        "exposed_headers": ["Content-Type", "Set-Cookie"],
    }

    # Patch 5 — cookies + session.cookie
    domain = os.environ["COOKIE_DOMAIN"]
    cfg["cookies"] = {"domain": domain, "same_site": "Lax"}
    cfg.setdefault("session", {}).setdefault("cookie", {})
    cfg["session"]["cookie"]["domain"] = domain
    cfg["session"].setdefault("lifespan", "24h")

    # Patch 6 — selfservice flows
    ss = cfg["selfservice"]
    app = os.environ["DASHBOARD_URL"].rstrip("/")
    ss["default_browser_return_url"] = app + "/"
    ss["allowed_return_urls"] = [app, app + "/", os.environ["CLOUD_URL"], os.environ["CLOUD_URL"] + "/"]
    flows = ss["flows"]
    flows.setdefault("login", {})["ui_url"] = app + "/login"
    flows.setdefault("registration", {})["ui_url"] = app + "/register"
    flows.setdefault("verification", {})["ui_url"] = app + "/verification"
    flows.setdefault("recovery", {})["ui_url"] = app + "/recovery"
    flows.setdefault("settings", {})["ui_url"] = app + "/settings"
    flows.setdefault("error", {})["ui_url"] = app + "/login"
    flows.setdefault("logout", {}).setdefault("after", {})["default_browser_return_url"] = app + "/login"

    new_yaml = yaml.safe_dump(cfg, default_flow_style=False, sort_keys=False)

    if dry:
        if raw == new_yaml:
            print("no-op: ConfigMap already in desired state")
        else:
            diff = difflib.unified_diff(
                raw.splitlines(keepends=True),
                new_yaml.splitlines(keepends=True),
                fromfile="ConfigMap (live)",
                tofile="ConfigMap (desired)",
                n=2,
            )
            sys.stdout.writelines(diff)
        return 0

    patched = {
        "apiVersion": "v1", "kind": "ConfigMap",
        "metadata": {"name": "ory-kratos-config", "namespace": ns},
        "data": {"kratos.yaml": new_yaml},
    }
    yaml.safe_dump(patched, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
