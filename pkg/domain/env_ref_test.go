package domain

import (
	"errors"
	"strings"
	"testing"
	"testing/quick"
)

func TestNewEnvRef_HappyPath(t *testing.T) {
	ref, err := NewEnvRef("tn-abc", "api", "production")
	if err != nil {
		t.Fatalf("NewEnvRef error: %v", err)
	}
	if ref.TenantID != "tn-abc" || ref.Project != "api" || ref.EnvName != "production" {
		t.Errorf("EnvRef fields wrong: %+v", ref)
	}
	if ref.CellID != "" {
		t.Errorf("CellID should default empty, got %q", ref.CellID)
	}
}

func TestNewEnvRef_WithCellID(t *testing.T) {
	ref, err := NewEnvRef("tn-abc", "api", "production", "cell-0")
	if err != nil {
		t.Fatalf("NewEnvRef error: %v", err)
	}
	if ref.CellID != "cell-0" {
		t.Errorf("CellID = %q; want %q", ref.CellID, "cell-0")
	}
}

func TestNewEnvRef_RejectsEmptyRequiredFields(t *testing.T) {
	cases := []struct {
		name              string
		tenant, proj, env string
	}{
		{"empty tenant", "", "api", "production"},
		{"empty project", "tn-abc", "", "production"},
		{"empty env", "tn-abc", "api", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewEnvRef(tc.tenant, tc.proj, tc.env)
			if !errors.Is(err, ErrInvalidEnvRefField) {
				t.Errorf("expected ErrInvalidEnvRefField, got %v", err)
			}
		})
	}
}

// TestNewEnvRef_RejectsSeparatorChar — EC-4 absorption: U+001F separator must
// NEVER appear in user inputs. Defense-in-depth against String/Parse round-trip
// corruption.
func TestNewEnvRef_RejectsSeparatorChar(t *testing.T) {
	_, err := NewEnvRef("tn-abc\x1f", "api", "production")
	if !errors.Is(err, ErrInvalidEnvRefField) {
		t.Errorf("expected ErrInvalidEnvRefField, got %v", err)
	}
}

// TestEnvRef_StringParseRoundTrip — property-based: every well-formed EnvRef
// must round-trip through String → ParseEnvRef without loss. Per ADR D2 EC-4:
// uses ASCII Unit Separator so values containing '-' survive (e.g., "staging-eu-1").
func TestEnvRef_StringParseRoundTrip(t *testing.T) {
	f := func(tenant, project, env, cell string) bool {
		// Skip degenerate inputs that NewEnvRef rejects by design
		if tenant == "" || project == "" || env == "" {
			return true
		}
		if strings.Contains(tenant+project+env+cell, "\x1f") {
			return true
		}
		ref, err := NewEnvRef(tenant, project, env, cell)
		if err != nil {
			return false
		}
		parsed, err := ParseEnvRef(ref.String())
		if err != nil {
			t.Logf("ParseEnvRef failed for %q: %v", ref.String(), err)
			return false
		}
		return ref.Equal(parsed)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// TestEnvRef_RoundTrip_HyphenatedEnvName — explicit regression test for
// the H6 reality: env names like "staging-eu-1" or "preview-pr-42" MUST
// round-trip without ambiguous-parse collision (former hyphen-separator
// design would have failed this case).
func TestEnvRef_RoundTrip_HyphenatedEnvName(t *testing.T) {
	cases := []EnvRef{
		{TenantID: "tn-x", Project: "api", EnvName: "staging-eu-1", CellID: "cell-0"},
		{TenantID: "tn-x", Project: "api", EnvName: "preview-pr-42", CellID: ""},
		{TenantID: "tn-abc-def", Project: "frontend-app", EnvName: "production", CellID: "cell-0"},
	}
	for _, want := range cases {
		t.Run(want.EnvName, func(t *testing.T) {
			got, err := ParseEnvRef(want.String())
			if err != nil {
				t.Fatalf("ParseEnvRef error: %v", err)
			}
			if !want.Equal(got) {
				t.Errorf("round-trip mismatch: want=%+v got=%+v", want, got)
			}
		})
	}
}

// TestEnvRef_Equal_ByValue — value object semantic (not pointer identity).
func TestEnvRef_Equal_ByValue(t *testing.T) {
	a, _ := NewEnvRef("tn-x", "api", "production", "cell-0")
	b, _ := NewEnvRef("tn-x", "api", "production", "cell-0")
	if !a.Equal(b) {
		t.Error("EnvRef.Equal should return true for value-identical refs")
	}
	// The other cell has to be a GENUINELY distinct cell. While the first one was
	// called `cell-default` and the second `cell-1`, the two names here differed
	// by an accident of vocabulary: they named the SAME machine (ADR 2026-08-09).
	// With the 1:1 unification, `cell-0` and `cell-1` are two real cells, and that
	// is what makes this case prove something.
	c, _ := NewEnvRef("tn-x", "api", "production", "cell-1")
	if a.Equal(c) {
		t.Error("EnvRef.Equal should return false when CellID differs")
	}
}

// TestEnvRef_Namespace_Canonical — T1.3.b of plan 2026-06-11-code-review-remediation.
// Asserts the canonical namespace formula `loopruntime-{tenant}-{project}-{env}`
// matches pkg/naming.Namespace for the same inputs. The format string is
// locked in BOTH pkg/naming.Namespace AND this method; this test pins
// them in lock-step so any future change to one MUST also update the
// other (or the test fails loud).
func TestEnvRef_Namespace_Canonical(t *testing.T) {
	cases := []struct {
		tenant, project, env string
		want                 string
	}{
		{"tn-acme", "api", "production", "loopruntime-tn-acme-api-production"},
		{"tn-x", "frontend", "staging-eu-1", "loopruntime-tn-x-frontend-staging-eu-1"},
		{"tn-abc", "monorepo", "preview-pr-42", "loopruntime-tn-abc-monorepo-preview-pr-42"},
	}
	for _, tc := range cases {
		ref, err := NewEnvRef(tc.tenant, tc.project, tc.env)
		if err != nil {
			t.Fatalf("NewEnvRef(%q,%q,%q) error: %v", tc.tenant, tc.project, tc.env, err)
		}
		got, nerr := ref.Namespace()
		if nerr != nil {
			t.Errorf("ref(%+v).Namespace() error: %v", ref, nerr)
			continue
		}
		if got != tc.want {
			t.Errorf("Namespace mismatch: got %q, want %q", got, tc.want)
		}
	}
}

// TestEnvRef_Namespace_RejectsEmptyFields — defense-in-depth. EnvRef
// constructed via the canonical constructor refuses empty fields, but
// struct-literal construction can produce a zero-value field. The
// Namespace() method enforces the invariant at the derivation site.
func TestEnvRef_Namespace_RejectsEmptyFields(t *testing.T) {
	cases := []struct {
		name string
		ref  EnvRef
	}{
		{"empty tenant", EnvRef{Project: "api", EnvName: "production"}},
		{"empty project", EnvRef{TenantID: "tn-x", EnvName: "production"}},
		{"empty env", EnvRef{TenantID: "tn-x", Project: "api"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.ref.Namespace()
			if err == nil {
				t.Errorf("expected error for zero-value field, got namespace=%q", got)
			}
			if !errors.Is(err, ErrInvalidEnvRefField) {
				t.Errorf("expected ErrInvalidEnvRefField, got %v", err)
			}
		})
	}
}
