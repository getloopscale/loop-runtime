package id

import (
	"regexp"
	"testing"
)

var buildIDRegexp = regexp.MustCompile(`^build-[0-9a-f]{8}$`)
var releaseIDRegexp = regexp.MustCompile(`^rel-[0-9a-f]{8}$`)

func TestGenerateBuildID(t *testing.T) {
	t.Run("returns build-{8hex} format", func(t *testing.T) {
		id := GenerateBuildID()
		if !buildIDRegexp.MatchString(id) {
			t.Errorf("expected build-{8hex}, got %q", id)
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
	t.Run("returns rel-{8hex} format", func(t *testing.T) {
		id := GenerateReleaseID()
		if !releaseIDRegexp.MatchString(id) {
			t.Errorf("expected rel-{8hex}, got %q", id)
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
