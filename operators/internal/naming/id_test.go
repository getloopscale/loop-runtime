package naming

import (
	"regexp"
	"testing"
)

var buildIDRegexp = regexp.MustCompile(`^build-[0-9a-f]{8}$`)
var releaseIDRegexp = regexp.MustCompile(`^rel-[0-9a-f]{8}$`)

func TestGenerateBuildID_Format(t *testing.T) {
	id := GenerateBuildID()
	if !buildIDRegexp.MatchString(id) {
		t.Errorf("build ID %q does not match expected format build-{8 hex chars}", id)
	}
}

func TestGenerateReleaseID_Format(t *testing.T) {
	id := GenerateReleaseID()
	if !releaseIDRegexp.MatchString(id) {
		t.Errorf("release ID %q does not match expected format rel-{8 hex chars}", id)
	}
}

func TestGenerateBuildID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateBuildID()
		if seen[id] {
			t.Fatalf("duplicate build ID generated: %q (after %d iterations)", id, i)
		}
		seen[id] = true
	}
}

func TestGenerateReleaseID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateReleaseID()
		if seen[id] {
			t.Fatalf("duplicate release ID generated: %q (after %d iterations)", id, i)
		}
		seen[id] = true
	}
}
