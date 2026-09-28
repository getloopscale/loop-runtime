#!/usr/bin/env bash
# check-workflow-hygiene.sh — two properties of this repo's workflows.
#
# This line said "checked on every PR". workflow-hygiene.yml has never had a
# pull_request trigger: it runs on PUSH to develop/main, post-merge. Corrected 2026-09-06 (B-192). A
# sentence promising a moment of verification that does not exist is the defect family that item measured.
#
# WHY IT EXISTS. The two defects below were already fixed BY HAND once (#233 and #250) and nothing
# prevented their reintroduction. This repo hosts `build-publish.yml`, the reusable workflow called by
# FIVE consumer repos — a `uses: foo@v4` or a `${{ }}` inside `run:` here does not stay here: it
# propagates to all of them on the next build. Fixing without guarding trades a defect for an interval
# until it comes back.
#
# G1 — every third-party action `uses:` is pinned to a 40-character SHA.
#      A tag is a MOVABLE pointer: whoever controls the action's repo can repoint `v4` to another
#      commit after review, and the next build runs code nobody read. `tj-actions/
#      changed-files` (March 2025) was exactly that, across ~23 thousand repos. A SHA is immutable.
#      `./.github/...` (local reusable) is excluded: there is no third party, and the content comes
#      from the same commit.
#
# G2 — no `${{ ... }}` inside a `run:` block.
#      `${{ }}` is substituted INTO THE TEXT of the script before the shell exists. A value with quotes,
#      `;` or `$(...)` stops being an argument and becomes a command, with access to the job's secrets.
#      The safe form is to pass it through the step's `env:` and use `"${VAR}"` — then the value arrives
#      as an environment variable and none of its content can become syntax.
#
#      The most dangerous case here is `workflow_call`: an input is, by definition, controlled by the
#      CALLER, and the set of callers changes without this file changing.
#
# HONEST LIMIT: G2 delimits the `run:` block by INDENTATION, not with a YAML parser. A `run:` with
# exotic indentation would escape. It is a cheap textual approximation that covers the shape this
# repo uses; the faithful alternative would require a dependency just for this.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

fail=0
note() { fail=1; printf '  %s\n' "$*" >&2; }

# ---- G1: third-party actions pinned by SHA -------------------------------------------------------
while IFS= read -r hit; do
  ref="${hit#*uses:}"; ref="${ref%%#*}"; ref="$(printf '%s' "${ref}" | tr -d ' \t')"
  case "${ref}" in
    ./*) continue ;;                                    # local reusable — same commit, no third party
  esac
  sha="${ref##*@}"
  if ! printf '%s' "${sha}" | grep -qE '^[0-9a-f]{40}$'; then
    [ "${fail}" -eq 0 ] && echo "workflow-hygiene: third-party action NOT pinned by SHA (G1, #233):" >&2
    note "${hit}"
  fi
done < <(grep -rnE '^\s*-?\s*uses:' .github/workflows/ 2>/dev/null)

# ---- G2: no interpolation inside `run:` ---------------------------------------------------------
g2=$(python3 - <<'PY'
import glob, re
hits = []
for path in sorted(glob.glob('.github/workflows/*.yml') + glob.glob('.github/workflows/*.yaml')):
    in_run, run_indent = False, 0
    for lineno, line in enumerate(open(path), 1):
        line = line.rstrip('\n')
        stripped = line.strip()
        if re.match(r'^-?\s*run:', stripped):
            in_run, run_indent = True, len(line) - len(line.lstrip())
            if '${{' in line:
                hits.append(f'{path}:{lineno}: {stripped[:110]}')
            continue
        if in_run:
            if stripped and (len(line) - len(line.lstrip())) <= run_indent:
                in_run = False
                continue
            if '${{' in line:
                hits.append(f'{path}:{lineno}: {stripped[:110]}')
print('\n'.join(hits))
PY
)
if [ -n "${g2}" ]; then
  [ "${fail}" -eq 0 ] && echo >&2
  echo "workflow-hygiene: \${{ }} interpolated inside \`run:\` (G2, #250):" >&2
  printf '%s\n' "${g2}" | sed 's/^/  /' >&2
  fail=1
fi

if [ "${fail}" -ne 0 ]; then
  cat >&2 <<'MSG'

How to fix:
  G1  replace the tag with the 40-character SHA of the reviewed commit, with the version in a comment:
        uses: actions/checkout@9c091bb...  # v7.0.0
  G2  move the value to the step's `env:` and use the variable in quotes:
        env: { IMAGE_REF: "${{ inputs.registry }}/${{ inputs.image }}" }
        run: cosign sign --yes "${IMAGE_REF}"
MSG
  exit 1
fi
echo "workflow-hygiene: actions pinned by SHA and no \${{ }} inside run: — ok."
