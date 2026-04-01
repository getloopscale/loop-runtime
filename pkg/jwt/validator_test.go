// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package jwt

import "testing"

func TestIsJWTToken(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ1c2VyMSJ9.signature", true},
		{"ghp_abc123def456", false},
		{"gho_abc123def456", false},
		{"github_pat_abc", false},
		{"short.jwt", false},
		{"", false},
		{"plaintext-token-no-dots", false},
	}

	for _, tt := range tests {
		t.Run(tt.token[:min(len(tt.token), 20)], func(t *testing.T) {
			if got := IsJWTToken(tt.token); got != tt.want {
				t.Errorf("IsJWTToken(%q) = %v, want %v", tt.token[:min(len(tt.token), 20)], got, tt.want)
			}
		})
	}
}

func TestIsGitHubPAT(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"ghp_abc123def456ghi789", true},
		{"gho_abc123", true},
		{"github_pat_abc123", true},
		{"eyJhbGciOiJSUzI1NiJ9.payload.sig", false},
		{"random-token", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.token[:min(len(tt.token), 15)], func(t *testing.T) {
			if got := IsGitHubPAT(tt.token); got != tt.want {
				t.Errorf("IsGitHubPAT(%q) = %v, want %v", tt.token[:min(len(tt.token), 15)], got, tt.want)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
