#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
"""Data-processing helper for seed-admin-loopzero-dev.sh (de-costumed from the
inline python3 heredoc per plan home-unhomed-logic-suspect-keeps T2.1).

The bash orchestrator owns all curl/psql/kubectl I/O; this module only
transforms JSON. Subcommand (argv[1]):

  build-identity — read the current Kratos identity JSON (env CURRENT_IDENTITY_PATH)
                   and emit the PUT body that sets a fresh cleartext password
                   (env SEED_PASSWORD; Kratos hashes on save). Stdout = new JSON.
"""
import json
import os
import sys


def build_identity() -> None:
    with open(os.environ["CURRENT_IDENTITY_PATH"]) as f:
        cur = json.load(f)
    out = {
        "schema_id": cur.get("schema_id", "default"),
        "traits": cur.get("traits", {}),
        "verifiable_addresses": cur.get("verifiable_addresses", []),
        "recovery_addresses": cur.get("recovery_addresses", []),
        "metadata_public": cur.get("metadata_public"),
        "metadata_admin": cur.get("metadata_admin"),
        "state": cur.get("state", "active"),
        # PUT-with-credentials writes cleartext password; Kratos hashes on save.
        # Preserves the OIDC credential implicitly because PUT semantics on
        # /admin/identities/{id} replace only the supplied fields' explicit
        # contents — credentials NOT listed are retained on the server side.
        "credentials": {
            "password": {
                "config": {"password": os.environ["SEED_PASSWORD"]}
            }
        },
    }
    print(json.dumps(out))


MODES = {"build-identity": build_identity}

if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] not in MODES:
        sys.stderr.write(f"usage: {sys.argv[0]} {{{'|'.join(MODES)}}}\n")
        sys.exit(2)
    MODES[sys.argv[1]]()
