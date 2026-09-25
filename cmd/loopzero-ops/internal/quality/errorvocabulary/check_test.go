// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package errorvocabulary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

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
	writeFile(t, root, "api/internal/routes/x/x.go", `package x

type e struct{}

func (e) Wrap(string) e { return e{} }

var httpio struct {
	WriteLoopZeroError func(any, e)
}

func h() {
	httpio.WriteLoopZeroError(nil, e{}.Wrap("ArgoCD sync failed for tenant_id tn-1; check Gitea and MinIO"))
}
`)
	writeFile(t, root, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root})
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
	writeFile(t, root, "api/internal/routes/x/x.go", `package x

import "log/slog"

func h() {
	slog.Warn("ArgoCD sync failed for tenant_id tn-1")
}
`)
	writeFile(t, root, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root})
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
	// Floor MEASURED 2026-08-19: 168 literals inside error writers. The first
	// version of this test demanded 200, a number I took from B-081's "277
	// messages" — and 277 counts CALL SITES of `WriteLoopZeroError`, not literals.
	// Two different metrics; repeating one in place of the other is how a number
	// gets invented. A value below 168 means the scan shrank (writer renamed, root
	// moved) and the green started to be worth less without anything warning.
	if out.Messages < 168 {
		t.Errorf("only %d message(s) reached; the real tree had 168 on 2026-08-19", out.Messages)
	}
	if !out.Pass {
		t.Errorf("the real tree carries a message with a term banned by Rule 3:\n%+v", out.Violations)
	}
}
