// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routeauthcoverage

import (
	"testing"
)

// routerWithImport builds a syntactically valid router.go whose auth group is
// gated by <qualifier>.Auth, with the middleware package imported under that
// same local name.
func routerWithImport(importSpec, qualifier, inside string) string {
	return `package server

import (
` + importSpec + `
)

func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(` + qualifier + `.Auth)
` + inside + `
	})
	return r
}
`
}

// TestAuthGroupIsFoundThroughAnImportAlias — B-059.
//
// The gate used to recognise the auth group by the letters `middleware.Auth`.
// That made it depend on a package's local name rather than on the package: the
// day `internal/middleware` split into concern packages, `router.go` imported
// the auth one as `mwauth` (because `internal/routes/auth` already owns the
// plain name) and the gate stopped finding the group it exists to police —
// reporting "could not locate the auth group" instead of the violations it was
// asked about. A gate that goes blind on a rename fails open on the next one.
//
// The group is now identified by the import path, so any alias works.
func TestAuthGroupIsFoundThroughAnImportAlias(t *testing.T) {
	for _, tc := range []struct {
		name, importSpec, qualifier string
	}{
		{
			name:       "concern package under an alias",
			importSpec: "\tmwauth \"github.com/getloopscale/loopzero/api/internal/middleware/auth\"",
			qualifier:  "mwauth",
		},
		{
			name:       "concern package under its own name",
			importSpec: "\t\"github.com/getloopscale/loopzero/api/internal/middleware/auth\"",
			qualifier:  "auth",
		},
		{
			name:       "the flat package this split replaced",
			importSpec: "\t\"github.com/getloopscale/loopzero/api/internal/middleware\"",
			qualifier:  "middleware",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := tree(t, map[string]string{
				routerPath: routerWithImport(
					tc.importSpec, tc.qualifier,
					"\t\tr.With(rateLimit...).Post(\"/api/v1/builds\", h)\n",
				),
			})

			out, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1}))

			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if out.PassCount != 1 {
				t.Fatalf("route inside the auth group was not counted as protected; PassCount=%d", out.PassCount)
			}
		})
	}
}

// TestAuthGroupIsNotFoundThroughAnUnrelatedPackage — the mutant for the test
// above. Widening "any identifier whose selector is Auth" would make the gate
// bless a group gated by somebody else's Auth symbol; the import path is what
// makes the recognition specific.
func TestAuthGroupIsNotFoundThroughAnUnrelatedPackage(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWithImport(
			"\tauth \"github.com/getloopscale/loopzero/api/internal/routes/auth\"", "auth",
			"\t\tr.With(rateLimit...).Post(\"/api/v1/builds\", h)\n",
		),
	})

	if _, err := violationOf(Check(Config{RepoRoot: root, MinRoutes: 1})); err == nil {
		t.Fatal("a group gated by an unrelated package's Auth must not be accepted as THE auth group")
	}
}
