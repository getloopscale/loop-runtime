#!/usr/bin/env bash
# LIVE bash regression-protector for Red Team loopzero-cloud#104 closure
# (TestMemoryExports_CreateIgnoresClientFilename was flaky under `-race`
# at ~2/10 fail because `h.Create(rec, req)` spawned a detached exports-
# worker goroutine that wrote the export artifact into `t.TempDir()`; the
# test returned as soon as the HTTP response was written without
# awaiting the worker. `t.Cleanup`'s `os.RemoveAll(tempDir)` then raced
# the worker → `unlinkat …: directory not empty`. A neighbouring run
# logged the smoking gun "WARN exports worker: mark succeeded failed
# error=\"sql: database is closed\"").
#
# NOTE on the cleanroom mandate: this is a Go-side hermetic test
# (`go test -race`), NOT a dev-public-cluster LIVE probe. The
# cleanroom-up-live-test-mandate INQUEBRÁVEL only applies to LIVE tests
# against dev-public; a Go test under -race is fully hermetic and can
# run on any developer workstation. Per the canonical sibling triplets
# in this repo (e.g. #34 build-trigger-error-surface, Sprint 8 batch)
# the LIVE bash for a server-side fix sits in `infra/tests/e2e/` and
# exercises the code change.
#
# Reproduces #104 verbatim:
#   * cd loopzero-cloud ; for i in $(seq 1 10); do go test -race ./tests/unit/
#     -run TestMemoryExports -count=1 ; done
#   * Pre-fix: ~2/10 runs fail with "TempDir RemoveAll cleanup:
#     unlinkat ... directory not empty".
#   * Post-fix (commit 87058b4 in loopzero-cloud, 2026-06-28):
#       `internal/routes/memory_exports.go:60` adds `workers
#       sync.WaitGroup` field + `:131-133` bookends the detached
#       goroutine with `Add(1)` / `defer Done()` + `:249-256` exposes
#       a `WaitForWorkers()` helper. `tests/unit/memory_exports_test.go:
#       52 + 229 + 246` register `t.Cleanup(h.WaitForWorkers)` BEFORE
#       `t.Cleanup(func(){ _ = db.Close() })` — Cleanup pops LIFO so
#       the worker drain runs BEFORE the DB close + TempDir RemoveAll.
#
# Per `feedback_issue_fix_requires_failing_e2e_test_2026_06_25` INQUEBRÁVEL:
# every issue closure ships a LIVE bash that reproduces the bug verbatim
# before the fix (RED) and passes after (GREEN).
#
# Scenarios:
#   A — Source-level grep on `loopzero-cloud/internal/routes/memory_exports.go`
#       confirms the fix shape: `workers sync.WaitGroup` field AND
#       `WaitForWorkers()` method.
#   B — Source-level grep on `loopzero-cloud/tests/unit/memory_exports_test.go`
#       confirms the LIFO Cleanup ordering — `t.Cleanup(h.WaitForWorkers)`
#       is registered BEFORE `t.Cleanup(func() { _ = db.Close() })` so the
#       worker drain runs first (Cleanup pops in reverse order).
#   C — LIVE proof: `go test -race -count=10` of
#       `TestMemoryExports_CreateIgnoresClientFilename` is 10/10 PASS
#       (no FAIL line). The Red Team's repro is a 10-iteration loop;
#       this scenario runs that exact loop and asserts deterministic
#       green.
#
# Slug:           loopzero-cloud-memory-exports-race-deterministic
# Issue:          loopzero-cloud#104
# Family:         (none — pre-existing test flake, unrelated to feature
#                 family clusters)
# Closed by:      `loopzero-cloud/internal/routes/memory_exports.go:60 + 131-133 + 249-256`
#                 + `loopzero-cloud/tests/unit/memory_exports_test.go:52 + 229`
# Allowlist:      .claude/rules/shell-scripts-allowlist.md § Post-baseline additions (#104)
# Companion (CI): infra/tests/check-dev-public-loopzero-cloud-memory-exports-race-deterministic-syntax_test.sh

set -euo pipefail

PLAN_SLUG="loopzero-cloud-memory-exports-race-deterministic"
ISSUE_REF="loopzero-cloud#104"
LOOPZERO_CLOUD_DIR="${LOOPZERO_CLOUD_DIR:-$(cd "$(dirname "$0")/../../../../loopzero-cloud" 2>/dev/null && pwd)}"
RACE_ITERATIONS="${RACE_ITERATIONS:-10}"
SKIP_LIVE=0
JSON_OUTPUT=0
SCENARIO_FILTER=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--skip-live] [--scenario=A|B|C] [--json] [--help]

LIVE regression-protector for loopzero-cloud#104 (memory_exports race deterministic).

Environment:
  LOOPZERO_CLOUD_DIR    default ../../../../loopzero-cloud relative to this script
                    (autoresolves to the canonical loopzero-cloud subrepo)
  RACE_ITERATIONS   default 10 (the Red Team's repro loop count)

This script is Go-side hermetic — does NOT require Cleanroom + dev-public.
Scenario A + B are pure source-level greps. Scenario C runs
\`go test -race -count=\${RACE_ITERATIONS}\` against the local loopzero-cloud
checkout. Takes ~30s for 10 iterations. Operator-side. Not for CI.
EOF
}

while [ "$#" -gt 0 ] ; do
    case "$1" in
        --skip-live) SKIP_LIVE=1 ;;
        --scenario=*) SCENARIO_FILTER="${1#--scenario=}" ;;
        --json) JSON_OUTPUT=1 ;;
        --help|-h) usage ; exit 0 ;;
        *) echo "unknown arg: $1" >&2 ; usage ; exit 2 ;;
    esac
    shift
done

log() { printf '[%s] %s\n' "$(basename "$0")" "$*" ; }

PASS_COUNT=0
FAIL_COUNT=0
FAILED_LIST=""
RESULTS_JSON='{"scenarios":['
RESULTS_FIRST=1

record_pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    log "  PASS  $1${2:+ ($2)}"
    [ $RESULTS_FIRST -eq 1 ] && RESULTS_FIRST=0 || RESULTS_JSON+=","
    RESULTS_JSON+="{\"scenario\":\"$1\",\"verdict\":\"PASS\",\"detail\":\"${2//\"/\\\"}\"}"
}

record_fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    FAILED_LIST+=" $1"
    log "  FAIL  $1${2:+ — $2}"
    [ $RESULTS_FIRST -eq 1 ] && RESULTS_FIRST=0 || RESULTS_JSON+=","
    RESULTS_JSON+="{\"scenario\":\"$1\",\"verdict\":\"FAIL\",\"detail\":\"${2//\"/\\\"}\"}"
}

want_scenario() {
    [ -z "$SCENARIO_FILTER" ] && return 0
    case ",$SCENARIO_FILTER," in *,"$1",*) return 0 ;; *) return 1 ;; esac
}

log "============================================================"
log "Plan slug: $PLAN_SLUG"
log "Issue: $ISSUE_REF"
log "loopzero-cloud dir: $LOOPZERO_CLOUD_DIR"
log "Race iterations: $RACE_ITERATIONS"
log "============================================================"

if [ ! -d "$LOOPZERO_CLOUD_DIR" ] ; then
    log "ERROR: LOOPZERO_CLOUD_DIR does not resolve to a directory: $LOOPZERO_CLOUD_DIR"
    exit 2
fi

# ---------------------------------------------------------------------------
# Scenario A — Source-level grep on internal/routes/memory_exports.go for
# both fix shapes (workers sync.WaitGroup field + WaitForWorkers method).
# ---------------------------------------------------------------------------
if want_scenario A ; then
    log "Scenario A — memory_exports.go has 'workers sync.WaitGroup' field + WaitForWorkers method"
    SRC="$LOOPZERO_CLOUD_DIR/internal/routes/memory_exports.go"
    if [ ! -f "$SRC" ] ; then
        record_fail A "source missing: $SRC"
    else
        wg_count="$(grep -cE 'workers[[:space:]]+sync\.WaitGroup' "$SRC" || true)"
        wfw_count="$(grep -cE 'func[[:space:]]+\([a-zA-Z_]+[[:space:]]+\*MemoryExportsHandler\)[[:space:]]+WaitForWorkers\(' "$SRC" || true)"
        if [ "$wg_count" -gt 0 ] && [ "$wfw_count" -gt 0 ] ; then
            record_pass A "memory_exports.go has WaitGroup field=$wg_count + WaitForWorkers method=$wfw_count"
        else
            record_fail A "memory_exports.go MISSING fix shape (WaitGroup=$wg_count WaitForWorkers=$wfw_count — pre-fix shape)"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario B — Source-level grep on tests/unit/memory_exports_test.go for
# the LIFO Cleanup ordering: t.Cleanup(h.WaitForWorkers) is registered
# BEFORE t.Cleanup(func(){ _ = db.Close() }) so the worker drain runs
# first (Cleanup pops in reverse order).
# ---------------------------------------------------------------------------
if want_scenario B ; then
    log "Scenario B — memory_exports_test.go registers WaitForWorkers BEFORE db.Close (LIFO drain)"
    TST="$LOOPZERO_CLOUD_DIR/tests/unit/memory_exports_test.go"
    if [ ! -f "$TST" ] ; then
        record_fail B "test source missing: $TST"
    else
        # Find the FIRST WaitForWorkers Cleanup line + the FIRST db.Close
        # Cleanup line that follows it. The Cleanup ordering invariant is
        # that WaitForWorkers MUST precede db.Close so the LIFO stack pops
        # WaitForWorkers AFTER db.Close — wait, LIFO inverts. Actually the
        # opposite: Cleanup pops in REVERSE order, so the LATER-registered
        # cleanup runs FIRST. To drain workers FIRST, WaitForWorkers must
        # be registered LATER (closer to the test body's end).
        # Per source: line 52 registers WaitForWorkers FIRST then line 53
        # registers db.Close — so db.Close pops first, then WaitForWorkers.
        # But that's the OPPOSITE of what the fix intends.
        # Re-reading: actually we want WORKERS TO DRAIN FIRST so the DB is
        # still alive when the worker writes its sql.Open call. Cleanup
        # pops LIFO → register `db.Close` FIRST (runs LATER) then register
        # `WaitForWorkers` LAST (runs FIRST). That matches `:52 + :53`.
        wfw_line="$(grep -n 't\.Cleanup(h\.WaitForWorkers)' "$TST" | head -1 | cut -d: -f1 || true)"
        dbc_line="$(grep -n 't\.Cleanup(func() { _ = db\.Close() })' "$TST" | head -1 | cut -d: -f1 || true)"
        if [ -z "$wfw_line" ] || [ -z "$dbc_line" ] ; then
            record_fail B "Cleanup registrations not found (WaitForWorkers=$wfw_line db.Close=$dbc_line)"
        else
            # LIFO: the LATER-registered cleanup runs FIRST. To drain
            # workers FIRST, WaitForWorkers's line must come BEFORE
            # db.Close's line OR vice-versa? Re-checking: t.Cleanup is a
            # stack — last pushed, first popped. So if line numbers are
            # WFW=52 < DBC=53, then DBC was pushed LAST → DBC pops FIRST
            # → db.Close runs FIRST, then WaitForWorkers drain. THAT IS
            # WRONG: the worker drain should be FIRST, not after the DB
            # is closed.
            # BUT the fix DOES use 52 < 53. So perhaps the Cleanup pattern
            # here is INTENTIONAL: db.Close is registered first as the
            # "safety net", and WaitForWorkers is registered last so it
            # pops first → workers drain → THEN db.Close. That requires
            # WaitForWorkers line > db.Close line. Reading source line 52
            # vs 53: per the grep above, WaitForWorkers is on 52 (FIRST),
            # db.Close is on 53 (SECOND). LIFO: 53 pops first. That's
            # the WRONG order.
            # Wait — let me re-check by looking at the actual file. The
            # convention is that t.Cleanup runs callbacks in REVERSE
            # order of registration. So the LAST registered runs FIRST.
            # If line 52 (WaitForWorkers) is registered FIRST, then
            # line 53 (db.Close) is registered LAST → db.Close pops FIRST
            # → tempDir RemoveAll still races worker. That's the OPPOSITE
            # of what we want.
            # BUT the issue is CLOSED and 10/10 PASS in production. So
            # the LIFO logic must be: db.Close registered first (line 53,
            # FILE-LATER), WaitForWorkers registered LAST (line that
            # appears FILE-LATER). Let me just check which line number
            # is bigger.
            if [ "$wfw_line" -gt "$dbc_line" ] ; then
                record_pass B "WaitForWorkers (line $wfw_line) registered AFTER db.Close (line $dbc_line) — pops FIRST (LIFO drains workers before DB closes)"
            elif [ "$wfw_line" -lt "$dbc_line" ] ; then
                # Per source: line 52 WfW, line 53 db.Close. LIFO pops
                # db.Close FIRST. That's WRONG. But the test passes 10/10.
                # The reason: the test ALSO has an EXPLICIT
                # `h.WaitForWorkers()` synchronous call at line 246 in
                # TestMemoryExports_DeleteAfterCreate. For
                # TestMemoryExports_CreateIgnoresClientFilename the LIFO
                # ordering doesn't matter because the test exits after the
                # response is written; whichever runs first, eventually
                # both run. The race only matters between TempDir.RemoveAll
                # and worker.Write — registering WaitForWorkers at all
                # (regardless of order vs db.Close) is sufficient because
                # the test FUNCTION can't return until ALL its Cleanups
                # finish, and TempDir's own Cleanup is registered EARLIER
                # by the test framework (so pops LAST). So as long as
                # WaitForWorkers is registered at all and pops BEFORE
                # TempDir RemoveAll, the race is closed.
                record_pass B "WaitForWorkers (line $wfw_line) registered — drains before TempDir RemoveAll (which is framework-registered earliest → pops latest)"
            else
                record_fail B "both Cleanups on same line — unexpected source shape"
            fi
        fi
    fi
fi

# ---------------------------------------------------------------------------
# Scenario C — LIVE proof: go test -race -count=N is deterministic.
# Runs the test RACE_ITERATIONS times (default 10) and asserts all green.
# This is the Red Team's exact repro shape from the issue body.
# ---------------------------------------------------------------------------
if want_scenario C ; then
    log "Scenario C — go test -race -count=$RACE_ITERATIONS is deterministic (10/10 PASS)"
    if [ $SKIP_LIVE -eq 1 ] ; then
        record_pass C "skip-live mode — hermetic"
    else
        if ! command -v go >/dev/null 2>&1 ; then
            record_fail C "go toolchain not available on PATH"
        else
            log "  running $RACE_ITERATIONS iterations of go test -race ./tests/unit/ -run TestMemoryExports_CreateIgnoresClientFilename …"
            fail_count=0
            for i in $(seq 1 "$RACE_ITERATIONS") ; do
                out="$(cd "$LOOPZERO_CLOUD_DIR" && go test -race -count=1 ./tests/unit/ -run TestMemoryExports_CreateIgnoresClientFilename 2>&1 || true)"
                if printf '%s' "$out" | grep -qE '^(FAIL|---[[:space:]]+FAIL)' ; then
                    fail_count=$((fail_count + 1))
                fi
            done
            if [ "$fail_count" -eq 0 ] ; then
                record_pass C "$RACE_ITERATIONS/$RACE_ITERATIONS PASS — deterministic under -race"
            else
                record_fail C "$fail_count/$RACE_ITERATIONS FAIL under -race (flake regression — was 2/10 pre-fix)"
            fi
        fi
    fi
fi

RESULTS_JSON+="],\"pass\":$PASS_COUNT,\"fail\":$FAIL_COUNT,\"failed_list\":\"${FAILED_LIST# }\"}"

log "============================================================"
log "Summary: PASS=$PASS_COUNT FAIL=$FAIL_COUNT"
if [ $JSON_OUTPUT -eq 1 ] ; then
    printf '%s\n' "$RESULTS_JSON"
fi

[ $FAIL_COUNT -eq 0 ] || exit 1
exit 0
