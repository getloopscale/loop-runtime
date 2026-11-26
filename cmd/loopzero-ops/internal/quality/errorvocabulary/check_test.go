// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package errorvocabulary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// fixtureRoots are the two feature directories the fixtures write: a handler
// package and a service. The default roots come from the real registry, which a
// fixture tree does not reproduce.
var fixtureRoots = []string{"api/internal/x", "api/internal/y"}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The mutant: a message that names the internal tool. Without this proof the gate
// could be green by seeing nothing, which is indistinguishable from there being no
// defect.
func TestFailsMessageWithBannedTerm(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/x/xhttp/x.go", `package x

type e struct{}

func (e) Wrap(string) e { return e{} }

var httpio struct {
	WriteLoopZeroError func(any, e)
}

func h() {
	httpio.WriteLoopZeroError(nil, e{}.Wrap("ArgoCD sync failed for tenant_id tn-1; check Gitea and MinIO"))
}
`)
	writeFile(t, root, "api/internal/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root, Roots: fixtureRoots})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a message with ArgoCD/tenant_id/Gitea/MinIO passed — the gate does not see the shape it exists to watch")
	}
	if len(out.Violations) != 1 {
		t.Fatalf("wanted 1 violation (one message), got %d: %+v", len(out.Violations), out.Violations)
	}
	if !strings.Contains(out.Violations[0].Message, "ArgoCD") {
		t.Errorf("the violation does not carry the offending message: %+v", out.Violations[0])
	}
}

// The counterpart: a literal with the same term OUTSIDE an error writer is not
// user-facing output — it is a log or an internal field name. Without this assertion
// the gate would become a general ban on the word in code, would fail everywhere and
// would be switched off on the first Friday.
func TestDoesNotFailTermOutsideErrorWriter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/x/xhttp/x.go", `package x

import "log/slog"

func h() {
	slog.Warn("ArgoCD sync failed for tenant_id tn-1")
}
`)
	writeFile(t, root, "api/internal/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root, Roots: fixtureRoots})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("a log with an internal term was failed; Rule 3 protects USER-FACING OUTPUT, not the vocabulary of the code: %+v", out.Violations)
	}
}

// The real population. A mutant proves the detector fires; only this says what it
// finds in what exists. The item recorded that no message carries a banned term
// today — the gate is born green, and this test is what backs that sentence.
func TestRealTreeIsClean(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Files == 0 || out.Messages == 0 {
		t.Fatalf("empty scan (%d files, %d messages): green about nothing is not green", out.Files, out.Messages)
	}
	// Floor MEASURED 2026-09-25: 143 authored messages. It read 168 from
	// 2026-08-19 until the scan was corrected: it counted EVERY string literal
	// inside a writer call, error codes included (NewLoopZeroError's first argument,
	// a LoopZeroError literal's Code), and it did not see a message authored in a
	// constructor outside the writer. When the upload, build and team handlers
	// moved to build-then-write, 37 messages left the scan and the gate stayed
	// green. Counted the corrected way (messages only, wherever authored) the
	// same base tree holds 146; the 3 fewer are the role refusals now answered by
	// one RequireRole. A value below 143 means the scan shrank again.
	if out.Messages < 143 {
		t.Errorf("only %d message(s) reached; the real tree had 143 on 2026-09-25", out.Messages)
	}
	if !out.Pass {
		t.Errorf("the real tree carries a message with a term banned by Rule 3:\n%+v", out.Violations)
	}
}
