// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestParseDockerConfigJSON_EmptyInput(t *testing.T) {
	cfg, err := parseDockerConfigJSON(nil)
	if err != nil {
		t.Fatalf("nil input: unexpected error: %v", err)
	}
	if cfg.Auths == nil {
		t.Error("nil input: Auths must be initialized, not nil")
	}

	cfg, err = parseDockerConfigJSON([]byte{})
	if err != nil {
		t.Fatalf("empty bytes: unexpected error: %v", err)
	}
	if cfg.Auths == nil {
		t.Error("empty bytes: Auths must be initialized, not nil")
	}
}

func TestParseDockerConfigJSON_Valid(t *testing.T) {
	in := []byte(`{"auths":{"registry.example.com":{"username":"u","password":"p","auth":"dXA="}}}`)
	cfg, err := parseDockerConfigJSON(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	entry, ok := cfg.Auths["registry.example.com"]
	if !ok {
		t.Fatal("missing entry registry.example.com")
	}
	if entry.Username != "u" || entry.Password != "p" || entry.Auth != "dXA=" {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

// basicEntry builds an auth entry the way a Docker config.json carries one.
func basicEntry(username, password string) mergedAuthEntry {
	return mergedAuthEntry{
		Username: username,
		Password: password,
		Auth:     base64.StdEncoding.EncodeToString([]byte(username + ":" + password)),
	}
}

func TestMergeAuthsByHost_PreservesExistingHosts(t *testing.T) {
	// Existing: DOCR auth.
	existing := []byte(`{"auths":{"registry.digitalocean.com":{"username":"docr-user","auth":"ZG9jcg=="}}}`)
	// Incoming: another registry's auth.
	incoming := map[string]mergedAuthEntry{
		"mirror.example.com": basicEntry("mirror-user", "mirror-secret"),
	}

	mergedBytes, err := mergeAuthsByHost(existing, incoming)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	merged, err := parseDockerConfigJSON(mergedBytes)
	if err != nil {
		t.Fatalf("re-parse merged: %v", err)
	}

	if _, ok := merged.Auths["registry.digitalocean.com"]; !ok {
		t.Error("DOCR auth was lost during merge — must preserve existing hosts")
	}
	if _, ok := merged.Auths["mirror.example.com"]; !ok {
		t.Error("incoming auth was not added by merge")
	}
}

func TestMergeAuthsByHost_OverridesSameHost(t *testing.T) {
	// Existing: stale entry.
	existing := []byte(`{"auths":{"mirror.example.com":{"username":"old-robot","auth":"b2xk"}}}`)
	// Incoming: refreshed entry (same host).
	incoming := map[string]mergedAuthEntry{
		"mirror.example.com": basicEntry("new-robot", "new-secret"),
	}

	mergedBytes, err := mergeAuthsByHost(existing, incoming)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	merged, _ := parseDockerConfigJSON(mergedBytes)
	got := merged.Auths["mirror.example.com"]
	if got.Username != "new-robot" {
		t.Errorf("override failed: username = %q, want %q", got.Username, "new-robot")
	}
}

func TestMergeAuthsByHost_CorruptExistingDoesNotAbort(t *testing.T) {
	// Corrupt JSON should be tolerated — start from fresh map.
	existing := []byte("not-json{{")
	incoming := map[string]mergedAuthEntry{
		"mirror.example.com": basicEntry("u", "p"),
	}

	mergedBytes, err := mergeAuthsByHost(existing, incoming)
	if err != nil {
		t.Fatalf("merge with corrupt existing must not error: %v", err)
	}
	merged, _ := parseDockerConfigJSON(mergedBytes)
	if _, ok := merged.Auths["mirror.example.com"]; !ok {
		t.Error("incoming auth lost when existing was corrupt")
	}
	if len(merged.Auths) != 1 {
		t.Errorf("expected only the incoming entry, got %d", len(merged.Auths))
	}
}

func TestContainsDOCRHost(t *testing.T) {
	cases := map[string]bool{
		"registry.digitalocean.com":             true,
		"registry.digitalocean.com/v2":          true,
		"registry.digitalocean.com/foo":         true,
		"mirror.example.com":                    false,
		"":                                      false,
		"registry.example.com":                  false,
		"my.registry.digitalocean.com.attacker": false, // not a prefix match — must start with the canonical host
		"registry.digitalocean.com.evil.com":    false, // prefix-spoof: exact domain + attacker suffix must NOT match
		"registry.digitalocean.comx":            false, // prefix-spoof: single char suffix
	}
	for host, want := range cases {
		got := containsDOCRHost(host)
		if got != want {
			t.Errorf("containsDOCRHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestMergeAuthsByHost_TwoWritersConverge(t *testing.T) {
	// Simulate the real reconcile cycle: writer A (DOCR) and writer B (another registry)
	// each add their auth on top of whatever they find. After 2 passes the
	// Secret must contain BOTH auths regardless of who runs first.
	type writer struct {
		host  string
		entry mergedAuthEntry
	}
	docrWriter := writer{
		host:  "registry.digitalocean.com",
		entry: basicEntry("docr-user", "docr-token"),
	}
	mirrorWriter := writer{
		host:  "mirror.example.com",
		entry: basicEntry("mirror-user", "mirror-secret"),
	}

	cases := [][]writer{
		{docrWriter, mirrorWriter, docrWriter, mirrorWriter},
		{mirrorWriter, docrWriter, mirrorWriter, docrWriter},
	}

	for i, ordering := range cases {
		var state []byte
		for _, w := range ordering {
			next, err := mergeAuthsByHost(state, map[string]mergedAuthEntry{w.host: w.entry})
			if err != nil {
				t.Fatalf("case %d: merge: %v", i, err)
			}
			state = next
		}
		merged, _ := parseDockerConfigJSON(state)
		if len(merged.Auths) != 2 {
			b, _ := json.Marshal(merged)
			t.Errorf("case %d: expected 2 auths, got %d (%s)", i, len(merged.Auths), b)
		}
		if _, ok := merged.Auths["registry.digitalocean.com"]; !ok {
			t.Errorf("case %d: DOCR auth missing after convergence", i)
		}
		if _, ok := merged.Auths["mirror.example.com"]; !ok {
			t.Errorf("case %d: second registry auth missing after convergence", i)
		}
	}
}
