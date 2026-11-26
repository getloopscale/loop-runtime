// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package id

import (
	"regexp"
	"testing"
)

var buildIDRegexp = regexp.MustCompile(`^build-[0-9a-f]{16}$`)
var releaseIDRegexp = regexp.MustCompile(`^rel-[0-9a-f]{16}$`)

func TestGenerateBuildID(t *testing.T) {
	t.Run("returns build-{16hex} format", func(t *testing.T) {
		id := GenerateBuildID()
		if !buildIDRegexp.MatchString(id) {
			t.Errorf("expected build-{16hex}, got %q", id)
		}
	})

	t.Run("generates unique IDs across 1000 calls", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			id := GenerateBuildID()
			if seen[id] {
				t.Fatalf("duplicate ID found: %s", id)
			}
			seen[id] = true
		}
	})
}

func TestGenerateReleaseID(t *testing.T) {
	t.Run("returns rel-{16hex} format", func(t *testing.T) {
		id := GenerateReleaseID()
		if !releaseIDRegexp.MatchString(id) {
			t.Errorf("expected rel-{16hex}, got %q", id)
		}
	})

	t.Run("generates unique IDs across 1000 calls", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			id := GenerateReleaseID()
			if seen[id] {
				t.Fatalf("duplicate ID found: %s", id)
			}
			seen[id] = true
		}
	})
}

// IsValid* validators per ADR-0028 supporting facilities and PKG-G06.

func TestIsValidBuildID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"canonical", "build-abcdef0123456789", true},
		{"generated", GenerateBuildID(), true},
		{"wrong prefix", "rel-abcdef0123456789", false},
		{"short hex", "build-abc", false},
		{"long hex", "build-abcdef01234567899", false},
		{"uppercase hex", "build-ABCDEF0123456789", false},
		{"non-hex chars", "build-abcdefghijk12345", false},
		{"empty", "", false},
		{"prefix only", "build-", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsValidBuildID(c.in); got != c.want {
				t.Errorf("IsValidBuildID(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIsValidReleaseID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"canonical", "rel-abcdef0123456789", true},
		{"generated", GenerateReleaseID(), true},
		{"wrong prefix", "build-abcdef0123456789", false},
		{"short", "rel-abc", false},
		{"uppercase", "rel-ABCDEF0123456789", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsValidReleaseID(c.in); got != c.want {
				t.Errorf("IsValidReleaseID(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIsValidPreviewID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"canonical", "prev-abcdef0123456789", true},
		{"generated", GeneratePreviewID(), true},
		{"wrong prefix", "rel-abcdef0123456789", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsValidPreviewID(c.in); got != c.want {
				t.Errorf("IsValidPreviewID(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestIsValidAppAttestationID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"canonical", "raa-abcdef0123456789", true},
		{"generated", GenerateAppAttestationID(), true},
		{"wrong prefix", "build-abcdef0123456789", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsValidAppAttestationID(c.in); got != c.want {
				t.Errorf("IsValidAppAttestationID(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
