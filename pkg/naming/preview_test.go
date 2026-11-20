// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package naming

import (
	"strings"
	"testing"
)

func TestPreviewEnvName(t *testing.T) {
	cases := []struct {
		prNumber int
		want     string
	}{
		{1, "preview-pr-1"},
		{42, "preview-pr-42"},
		{12345, "preview-pr-12345"},
	}
	for _, tc := range cases {
		if got := PreviewEnvName(tc.prNumber); got != tc.want {
			t.Errorf("PreviewEnvName(%d) = %q; want %q", tc.prNumber, got, tc.want)
		}
	}
	// Sanity: the result must match IsPreviewEnv's detection pattern.
	if !IsPreviewEnv(PreviewEnvName(7)) {
		t.Error("PreviewEnvName output must satisfy IsPreviewEnv contract")
	}
}

func TestPreviewSubdomain_WithEntryApp(t *testing.T) {
	got := PreviewSubdomain("tn-acme", "my-api", "web", 42)
	want := "tn-acme-my-api-web-preview-pr-42"
	if got != want {
		t.Errorf("PreviewSubdomain with entryApp: got %q; want %q", got, want)
	}
}

func TestPreviewSubdomain_WithoutEntryApp(t *testing.T) {
	got := PreviewSubdomain("tn-acme", "my-api", "", 42)
	want := "tn-acme-my-api-preview-pr-42"
	if got != want {
		t.Errorf("PreviewSubdomain without entryApp: got %q; want %q", got, want)
	}
}

// TestPreviewSubdomain_TruncatesAtRFC1123Cap — hostname labels are
// capped at 63 chars. T2.5 helper must preserve the constraint that
// the original previewURL inline code enforced.
func TestPreviewSubdomain_TruncatesAtRFC1123Cap(t *testing.T) {
	long := strings.Repeat("a", 60)
	got := PreviewSubdomain(long, "p", "w", 1)
	if len(got) > 63 {
		t.Errorf("subdomain must be ≤ 63 chars; got len=%d (%q)", len(got), got)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("truncation must trim trailing '-' to keep RFC 1123 valid: %q", got)
	}
}

func TestPreviewURL_HappyPath(t *testing.T) {
	got := PreviewURL("tn-acme", "my-api", "web", "preview-dev.getloopscale.app", 42)
	want := "https://tn-acme-my-api-web-preview-pr-42.preview-dev.getloopscale.app"
	if got != want {
		t.Errorf("PreviewURL drift:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestPreviewURL_EmptyDomainReturnsEmpty — callers detect "no preview
// URL configured" without parsing the result. Empty publicDomain is
// the documented signal.
func TestPreviewURL_EmptyDomainReturnsEmpty(t *testing.T) {
	if got := PreviewURL("tn", "p", "w", "", 1); got != "" {
		t.Errorf("expected empty string for empty publicDomain, got %q", got)
	}
}

func TestPreviewURL_WithoutEntryApp(t *testing.T) {
	got := PreviewURL("tn-acme", "my-api", "", "preview-dev.getloopscale.app", 42)
	want := "https://tn-acme-my-api-preview-pr-42.preview-dev.getloopscale.app"
	if got != want {
		t.Errorf("PreviewURL without app: got %q; want %q", got, want)
	}
}

// A label cut at the cap on a hyphen boundary must not end with a hyphen.
func TestPreviewSubdomain_TruncationNeverEndsWithAHyphen(t *testing.T) {
	tenant := strings.Repeat("t", 40)
	project := strings.Repeat("p", 21) // cut lands right after "{tenant}-{project}-"
	got := PreviewSubdomain(tenant, project, "", 1)
	if len(got) > MaxKubernetesNameLength || strings.HasSuffix(got, "-") {
		t.Fatalf("got %q (%d chars)", got, len(got))
	}
	if want := tenant + "-" + project; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
