#!/usr/bin/env bash
# gen-state-machine-dots.sh — Regenerate DOT files for every state machine.
#
# Usage:
#   gen-state-machine-dots.sh --check   # exit 1 if any DOT is out-of-date
#   gen-state-machine-dots.sh --write   # regenerate all DOTs
#
# Per pkg-sota-faang-grade-plan v1.0 T2.7 + gate PKG-G18.
#
# DOT output is deterministic (Visualize sorts states + transitions),
# so any drift between code and committed DOT signals an unintended
# transition change in PR review. CI calls --check; humans run --write
# locally before commit.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="$ROOT/docs/state-machines"

mode="${1:-}"
case "$mode" in
  --check|--write) ;;
  *) echo "usage: $0 --check|--write" >&2; exit 2 ;;
esac

mkdir -p "$OUT_DIR"

# Build a tiny Go program that imports pkg/statemachine and writes
# DOT files for each known machine.
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

cat > "$tmpdir/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"

	sm "github.com/loopzero/loopzero/pkg/statemachine"
)

func main() {
	machines := map[string]*sm.Machine{
		"build":              sm.NewBuildMachine(),
		"release":            sm.NewReleaseMachine(),
		"tenant":             sm.NewTenantMachine(),
		"domain":             sm.NewDomainMachine(),
		"environment":        sm.NewEnvironmentMachine(),
		"platform-bootstrap": sm.NewPlatformBootstrapMachine(),
		"promotion":          sm.NewPromotionMachine(),
		"rollback":           sm.NewRollbackMachine(),
		"application":        sm.NewApplicationMachine(),
		"exposed-route":      sm.NewExposedRouteMachine(),
		"preview":            sm.NewPreviewMachine(),
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gen <out_dir>")
		os.Exit(2)
	}
	outDir := os.Args[1]
	for name, m := range machines {
		path := outDir + "/" + name + ".dot"
		if err := os.WriteFile(path, []byte(m.Visualize()), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
			os.Exit(3)
		}
	}
}
EOF

cd "$ROOT/pkg"
case "$mode" in
  --write)
    go run "$tmpdir/main.go" "$OUT_DIR"
    echo "Wrote DOT files to $OUT_DIR"
    ;;
  --check)
    snapshot="$(mktemp -d)"
    go run "$tmpdir/main.go" "$snapshot"
    if ! diff -rq "$OUT_DIR" "$snapshot" >/dev/null 2>&1; then
      echo "ERROR: state machine DOT files out-of-date — run \`task pkg:state-machine-dots:write\` and commit." >&2
      diff -ruN "$OUT_DIR" "$snapshot" >&2 || true
      rm -rf "$snapshot"
      exit 1
    fi
    rm -rf "$snapshot"
    echo "OK: state machine DOT files are up-to-date."
    ;;
esac
