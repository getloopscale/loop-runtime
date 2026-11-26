// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainsinkpurity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func sink(t *testing.T, files map[string]string) Config {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, sinkDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Config{
		RepoRoot: root,
		Frozen:   []string{"db.Build", "db.DBTX"},
	}
}

const twoFrozen = `package deploycontracts

import "github.com/getloopscale/loop-runtime/api/internal/db"

type Port interface {
	WithLock(fn func(tx db.DBTX) error) error
	Activate(b *db.Build) error
}
`

func TestCheck_FrozenSetPasses(t *testing.T) {
	if _, err := violationOf(Check(sink(t, map[string]string{"a.go": twoFrozen}))); err != nil {
		t.Errorf("the measured set is the passing case; got %v", err)
	}
}

// The whole point: the FIFTH reference. A leak that is allowed to grow reaches
// every package that depends on the sink — measured at 35.
func TestCheck_ANewSymbolFails(t *testing.T) {
	extra := twoFrozen + "\ntype More struct{ R *db.Release }\n"
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": extra})))
	if !errors.Is(err, ErrSinkLeakGrew) {
		t.Fatalf("a symbol beyond the frozen set must FAIL; got %v", err)
	}
	if len(out.New) != 1 || out.New[0] != "db.Release" {
		t.Errorf("the failure must NAME the symbol that appeared; got %+v", out.New)
	}
}

// R3: freezing a COUNT would let one symbol be swapped for another and stay
// green. The gate freezes the SET.
func TestCheck_SwappingOneSymbolForAnotherFails(t *testing.T) {
	swapped := `package deploycontracts

import "github.com/getloopscale/loop-runtime/api/internal/db"

type Port interface {
	WithLock(fn func(tx db.DBTX) error) error
	Activate(b *db.Release) error
}
`
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": swapped})))
	if !errors.Is(err, ErrSinkLeakGrew) {
		t.Fatalf("same count, different set, must FAIL — a ratchet on a number is not a ratchet; got %v", err)
	}
	if len(out.New) != 1 || out.New[0] != "db.Release" {
		t.Errorf("got %+v", out.New)
	}
}

// A symbol that DISAPPEARS is progress, not a failure — but it must be reported,
// so the frozen set can be tightened rather than quietly drifting from reality.
func TestCheck_ARemovedSymbolIsReportedNotFailed(t *testing.T) {
	fewer := `package deploycontracts

import "github.com/getloopscale/loop-runtime/api/internal/db"

type Port interface {
	WithLock(fn func(tx db.DBTX) error) error
}
`
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": fewer})))
	if err != nil {
		t.Fatalf("removing a leak must never fail the gate; got %v", err)
	}
	if len(out.Gone) != 1 || out.Gone[0] != "db.Build" {
		t.Errorf("a removal must be REPORTED so the frozen set can be tightened; got %+v", out.Gone)
	}
}

// The floor. A gate whose subject is absent has verified nothing, and reporting
// PASS there is the failure mode B-040 measured.
func TestCheck_AbsentPackageIsAnError(t *testing.T) {
	cfg := Config{RepoRoot: t.TempDir(), Frozen: []string{"db.Build"}}
	if _, err := violationOf(Check(cfg)); !errors.Is(err, ErrSinkNotFound) {
		t.Errorf("an absent sink must FAIL, never pass vacuously; got %v", err)
	}
}

// A package present but holding no readable Go file is the same condition.
func TestCheck_NoGoFilesIsAnError(t *testing.T) {
	cfg := sink(t, map[string]string{"README.md": "nada"})
	if _, err := violationOf(Check(cfg)); !errors.Is(err, ErrSinkNotFound) {
		t.Errorf("zero Go files parsed means nothing was examined; got %v", err)
	}
}

// Test files are excluded: a fixture naming a db type is not the package's
// public surface, and counting it would make the ratchet fire on test edits.
func TestCheck_TestFilesAreNotTheSurface(t *testing.T) {
	files := map[string]string{
		"a.go": twoFrozen,
		"a_test.go": `package deploycontracts

import "github.com/getloopscale/loop-runtime/api/internal/db"

var _ = db.Release{}
`,
	}
	if _, err := violationOf(Check(sink(t, files))); err != nil {
		t.Errorf("a test fixture is not the package surface; got %v", err)
	}
}

// Against the real tree. The frozen set is the one measured in the slice-4 plan;
// a failure here is a real growth of the leak, not a fixture artefact.
func TestCheck_RealTree(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: qtestutil.Root(t)}))
	if err != nil {
		t.Fatalf("the sink's db surface grew beyond what slice-4 measured: new=%v gone=%v", out.New, out.Gone)
	}
	if len(out.Found) == 0 {
		t.Fatal("the real sink references db types; finding none means the extractor is broken, not that the leak is closed")
	}
}

// MEASURED bypass, found by probing rather than reading: with the local name
// hardcoded to "db", an aliased import made the extractor find NOTHING and the
// gate report PASS over a leak it could not see.
//
//	import persistence "…/api/internal/db"
//	type Sneaky struct{ R *persistence.Release }   → found=[] new=[] err=nil
//
// A ratchet keyed on a name the caller chooses is a ratchet the caller unlocks.
func TestCheck_AliasedImportIsStillCaught(t *testing.T) {
	body := `package deploycontracts

import persistence "github.com/getloopscale/loop-runtime/api/internal/db"

type Sneaky struct{ R *persistence.Release }
`
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": body})))
	if !errors.Is(err, ErrSinkLeakGrew) {
		t.Fatalf("an aliased import must not hide a leak; got %v found=%v", err, out.Found)
	}
	// Keyed under the canonical prefix, not the alias — otherwise renaming an
	// import would silently empty the frozen set on the next tightening.
	if len(out.New) != 1 || out.New[0] != "db.Release" {
		t.Errorf("the finding must be keyed canonically, never by the alias; got %+v", out.New)
	}
}

// A file that does not import persistence at all contributes nothing — and must
// not contribute a false hit through some other package that happens to be
// called db.
func TestCheck_UnrelatedPackageNamedDbIsNotCounted(t *testing.T) {
	body := `package deploycontracts

import "example.com/somethingelse/db"

type Fine struct{ X db.Release }
`
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": body, "b.go": twoFrozen})))
	if err != nil {
		t.Fatalf("a different package that happens to be named db is not OUR persistence package; got %v found=%v", err, out.Found)
	}
}

// MEASURED bypass: a dot-import makes every reference unqualified, and the
// extractor found nothing. The doc-comment CLAIMED the caller reported this as a
// hard finding; nothing did, and two new public leaks crossed with exit 0.
//
// A comment asserting a guard that does not exist is worse than no guard — the
// next reader believes it and stops looking. It is now implemented.
func TestCheck_DotImportIsAHardFinding(t *testing.T) {
	body := `package deploycontracts

import . "github.com/getloopscale/loop-runtime/api/internal/db"

type Sneaky struct{ R *Release }
`
	if _, err := violationOf(Check(sink(t, map[string]string{"a.go": body}))); !errors.Is(err, ErrUnqualifiedImport) {
		t.Fatalf("a dot-import puts the boundary beyond static reach and must FAIL, never pass silently; got %v", err)
	}
}

// MEASURED bypass: Go permits importing one path twice under different names,
// and resolving only the FIRST let `dbx.AppSecret` cross with the gate green.
func TestCheck_DuplicateImportUnderTwoNamesIsCaught(t *testing.T) {
	body := `package deploycontracts

import (
	"github.com/getloopscale/loop-runtime/api/internal/db"
	dbx "github.com/getloopscale/loop-runtime/api/internal/db"
)

type Port interface {
	WithLock(fn func(tx db.DBTX) error) error
	Secret() *dbx.AppSecret
}
`
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": body})))
	if !errors.Is(err, ErrSinkLeakGrew) {
		t.Fatalf("a second alias for the same path must not hide a leak; got %v found=%v", err, out.Found)
	}
	if len(out.New) != 1 || out.New[0] != "db.AppSecret" {
		t.Errorf("got %+v", out.New)
	}
}

// MEASURED false positive: a local variable named `db` — ordinary Go — was
// reported as "NOVO VAZAMENTO db.Close", a finding about something that crosses
// no boundary. A ratchet that fires on non-leaks is one somebody switches off.
func TestCheck_LocalVariableShadowingTheImportIsNotALeak(t *testing.T) {
	body := `package deploycontracts

import "github.com/getloopscale/loop-runtime/api/internal/db"

type logger struct{}

func (logger) Close() error { return nil }

func helper() error {
	db := logger{}
	return db.Close()
}

type Port interface {
	WithLock(fn func(tx db.DBTX) error) error
}
`
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": body})))
	if err != nil {
		t.Fatalf("a shadowing local is not the package; got %v new=%v", err, out.New)
	}
	for _, f := range out.Found {
		if f == "db.Close" {
			t.Errorf("db.Close is a method on a local value, not a persistence type crossing the boundary; found=%v", out.Found)
		}
	}
}

// db/store is persistence too, and was a blind spot of BOTH gates:
// dbgeneratedboundary fences only db/generated (and db inside `server`).
func TestCheck_DbStoreIsWatchedToo(t *testing.T) {
	body := `package deploycontracts

import "github.com/getloopscale/loop-runtime/api/internal/db/store"

type Port interface{ Repo() *store.ReleaseStore }
`
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": body})))
	if !errors.Is(err, ErrSinkLeakGrew) {
		t.Fatalf("db/store crossing the sink must FAIL; got %v found=%v", err, out.Found)
	}
	if len(out.New) != 1 || out.New[0] != "store.ReleaseStore" {
		t.Errorf("got %+v", out.New)
	}
}

// A blank import brings no symbol into scope, so it is not a boundary crossing.
func TestCheck_BlankImportIsNotACrossing(t *testing.T) {
	body := `package deploycontracts

import (
	_ "github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/db"
)

type Port interface {
	WithLock(fn func(tx db.DBTX) error) error
}
`
	if _, err := violationOf(Check(sink(t, map[string]string{"a.go": body}))); err != nil {
		t.Errorf("a blank import exposes no symbol; got %v", err)
	}
}

// MEASURED hole, opened by slice-6 and caught by its review: the SQL access port
// moved to `api/internal/db/dbtx` and `db` re-exports it by TYPE ALIAS, so
// `dbtx.DBTX` IS `db.DBTX`. With findings keyed by the import's base name,
// changing one import line in the sink produced:
//
//	Found=[] New=[] Gone=[db.DBTX] err=<nil>
//
// The frozen symbol reported as GONE — which this gate's own Outcome documents
// as "progress that should be locked in" — while the boundary stayed crossed by
// an identical type. A ratchet that reads a rename as progress goes green
// precisely when someone routes around it.
func TestCheck_AliasPathIsKeyedUnderTheCanonicalPrefix(t *testing.T) {
	body := `package deploycontracts

import "github.com/getloopscale/loop-runtime/api/internal/db/dbtx"

type Port interface{ WithLock(fn func(tx dbtx.DBTX) error) error }
`
	out, err := violationOf(Check(sink(t, map[string]string{"a.go": body})))
	if err != nil {
		t.Fatalf("the frozen set carries db.DBTX and this IS db.DBTX; got %v new=%v", err, out.New)
	}
	for _, g := range out.Gone {
		if g == "db.DBTX" {
			// db.Build is legitimately absent from this fixture; db.DBTX is not.
			t.Errorf("importing the alias path must NOT read as removal — the type never left; got gone=%+v found=%+v", out.Gone, out.Found)
		}
	}
	if len(out.Found) != 1 || out.Found[0] != "db.DBTX" {
		t.Errorf("the finding must be keyed under the canonical prefix regardless of which path names it; got %+v", out.Found)
	}
}
