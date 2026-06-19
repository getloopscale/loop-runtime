#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2025-2026 The LoopZero Authors
"""dev-credentials.json fallback resolver for patch-kratos-dev-public.sh.

De-costumed from an inline python3 heredoc per plan home-unhomed-logic-
suspect-keeps (un-homed-logic gate: KEEP-* scripts may NOT embed Python
heredocs because that hides reimplementable logic from review). Same
pattern as patch_kratos_dev_public.py (sibling transform helper).

Resolves Finding #3 of issue #91 (Cleanroom mandate canonical chain run
2026-06-18). The canonical post-Cleanroom seed file is
~/.loopzero/dev-credentials.json (per memories
feedback_post_cleanroom_seed_github_app_canonical +
reference_admin_cli_hydra_client_canonical). Operators who only
populated that file (and not env vars or .env) hit opaque exit-1
failures on first Cleanroom because patch-kratos-dev-public.sh did
NOT consult the canonical source.

Args (positional):
  argv[1] = required env var name (e.g. LOOPZERO_GITHUB_CLIENT_ID)
  argv[2] = path to dev-credentials.json

Behavior:
  - Maps LOOPZERO_* env-var name -> JSON path inside dev-credentials.json.
  - Writes resolved value to STDOUT (no trailing newline).
  - Exit 0 + empty stdout when the key is not in the mapping, the file
    is malformed, or the JSON path is missing — same fall-through
    semantics as the bash caller expects (empty value triggers the
    "ERROR: $key not set" branch downstream).
"""
import json
import sys

# Authoritative mapping: every LOOPZERO_* env var consulted by patch-kratos-
# dev-public.sh load_env MUST have an entry here. Adding a new key
# without updating this mapping AND the hermetic regression lock will
# fail CI.
MAPPING = {
    "LOOPZERO_GITHUB_CLIENT_ID":     ["github", "client_id"],
    "LOOPZERO_GITHUB_CLIENT_SECRET": ["github", "client_secret"],
    "LOOPZERO_GOOGLE_CLIENT_ID":     ["google", "client_id"],
    "LOOPZERO_GOOGLE_CLIENT_SECRET": ["google", "client_secret"],
    "LOOPZERO_RESEND_API_KEY":       ["resend", "api_key"],
}


def main() -> int:
    if len(sys.argv) != 3:
        return 0  # silent fall-through; bash caller treats empty as not-found
    key, path = sys.argv[1], sys.argv[2]

    selector = MAPPING.get(key)
    if not selector:
        return 0

    try:
        with open(path) as f:
            doc = json.load(f)
    except (OSError, json.JSONDecodeError):
        return 0

    cursor = doc
    for seg in selector:
        if not isinstance(cursor, dict) or seg not in cursor:
            return 0
        cursor = cursor[seg]

    if not isinstance(cursor, (str, int, float)):
        return 0

    sys.stdout.write(str(cursor))
    return 0


if __name__ == "__main__":
    sys.exit(main())
