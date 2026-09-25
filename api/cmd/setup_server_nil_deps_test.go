package main

import (
	"testing"
)

// buildServerConfig is called from main.go with values that main.go itself treats
// as nilable: `dbDeps` comes from setupDatabase and is guarded by `if dbDeps != nil`
// (main.go:127), and `stores` is declared as a nil `*StoreDeps` and only assigned
// inside a conditional (main.go:146-168).
//
// buildServerConfig guards them too — but AFTER it has already read fields off
// them. `if dbDeps != nil` sits at line 159 and `if stores != nil` at line 214,
// while `stores.Locker` and `dbDeps.ProjectStore` are read at 104 and 111.
//
// So the two nil checks describe a state the function cannot survive reaching.
// That state is not hypothetical: it is exactly the no-database boot that
// setup_database.go documents as deliberate "graceful degradation" (B-116). The
// degradation does not degrade — it panics before the server is ever built.
//
// These tests assert the contract main.go already assumes: passing nil for a
// dependency main.go can pass nil for must not panic.
func TestBuildServerConfig_SurvivesNilDatabaseDeps(t *testing.T) {
	assertNoPanic(t, "dbDeps", func() {
		buildServerConfigWithNils(t, nil, &StoreDeps{})
	})
}

func TestBuildServerConfig_SurvivesNilStoreDeps(t *testing.T) {
	assertNoPanic(t, "stores", func() {
		buildServerConfigWithNils(t, &DatabaseDeps{}, nil)
	})
}

// Both nil at once is the real no-database boot: setupStores is only called when
// the database came up, so a nil dbDeps implies a nil stores.
func TestBuildServerConfig_SurvivesTheNoDatabaseBoot(t *testing.T) {
	assertNoPanic(t, "dbDeps and stores", func() {
		buildServerConfigWithNils(t, nil, nil)
	})
}

func assertNoPanic(t *testing.T, what string, call func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("buildServerConfig panicked with a nil %s: %v\n\n"+
				"main.go passes nil here on the no-database boot path, and "+
				"buildServerConfig's own `if %s != nil` guard says it expects to "+
				"receive one — but it reads fields off it first.", what, r, what)
		}
	}()
	call()
}

// buildServerConfigWithNils calls the real constructor with the nil-ability of each
// parameter taken from what `main.go` actually does, not from what would be
// convenient to assert:
//
//	nilable  dbAdapter (main.go:126) · orchDeps + ghDeps (152-153) ·
//	         cellRouter + cellStore (227-228) · dbDeps (121+127) · stores (146)
//	never    k8sDeps — setupK8s failing makes main os.Exit(1) at main.go:118, so
//	         the call site cannot reach buildServerConfig with a nil one
//
// Passing nil for k8sDeps would assert a contract nobody has, and a test that
// invents a contract fails for a reason unrelated to the defect.
func buildServerConfigWithNils(t *testing.T, dbDeps *DatabaseDeps, stores *StoreDeps) {
	t.Helper()
	w := quietWiring(t)
	w.dbDeps = dbDeps
	w.stores = stores
	buildServerConfig(w)
}
