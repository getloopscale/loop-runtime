// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package architecture

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportGraph_HTTPPackagesAreLeaves is the architectural guard that began as
// T1.3 of deep-review-remediation-plan (find_arch_001 +
// find_dep_routes_type_hub).
//
// Before T1.3, services/, db/, k8s/, loki/ and github/ imported the routes/
// package upward to consume types like CustomDomain, CreatePreviewRequest,
// AttestationStore, LogEntry, EnvironmentEntry and TenantCRSpec — a DIP
// inversion: lower-layer packages depended on the HTTP handler package. The fix
// moved those types to the ports package.
//
// Since api/internal was organized by feature (docs/decisions.md, 2026-09-25)
// the handlers are the `<feature>/<name>http` packages, and the invariant is the
// same one said for all of them: production code does not import an HTTP
// package, except
//   - the HTTP package's own subtree (e.g. a test-helper package nested in it),
//   - the feature's adapter package (`<feature>/<feature>adapter`), which
//     satisfies the handler's ports,
//   - the composition roots, server and backgroundloops.
//
// If you want to add such an import, the type you need belongs in the feature
// root or in ports, not in the handler package. feature-fence enforces the same
// rule at the quality-gate level; this test keeps it runnable with `go test`.
func TestImportGraph_HTTPPackagesAreLeaves(t *testing.T) {
	const internal = "github.com/getloopscale/loop-runtime/api/internal/"
	out, err := exec.Command("go", "list", "-f",
		"{{.ImportPath}} {{join .Imports \" \"}}", "github.com/getloopscale/loop-runtime/api/internal/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}

	httpPackages := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		importer := strings.TrimPrefix(fields[0], internal)
		if isHTTPPackage(importer) {
			httpPackages++
		}
		for _, imp := range fields[1:] {
			target, ok := strings.CutPrefix(imp, internal)
			if !ok || !isHTTPPackage(target) {
				continue
			}
			if mayImportHTTP(importer, target) {
				continue
			}
			t.Errorf("%s imports the HTTP package %s — handlers are leaves; move the "+
				"type it needs to the feature root or to ports", importer, target)
		}
	}
	if httpPackages == 0 {
		t.Fatal("no HTTP package found under api/internal — the walk is broken, not the graph clean")
	}
}

// isHTTPPackage reports whether rel (relative to api/internal) is inside a
// `<feature>/<name>http` package or its subtree.
func isHTTPPackage(rel string) bool {
	return httpUnit(rel) != ""
}

// httpUnit returns the `<feature>/<name>http` directory rel lies in, or "".
func httpUnit(rel string) string {
	segs := strings.Split(rel, "/")
	if len(segs) >= 2 && strings.HasSuffix(segs[1], "http") {
		return segs[0] + "/" + segs[1]
	}
	return ""
}

func mayImportHTTP(importer, target string) bool {
	if unit := httpUnit(target); unit != "" && httpUnit(importer) == unit {
		return true // the HTTP package's own subtree
	}
	segs := strings.Split(importer, "/")
	switch segs[0] {
	case "server", "backgroundloops":
		return true
	}
	feature := strings.Split(target, "/")[0]
	return len(segs) >= 2 && segs[0] == feature && segs[1] == feature+"adapter"
}
