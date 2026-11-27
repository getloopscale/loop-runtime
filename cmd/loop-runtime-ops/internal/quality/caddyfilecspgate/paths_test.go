// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package caddyfilecspgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func violationsFor(t *testing.T, content string) []string {
	t.Helper()
	out, err := Check(Config{RepoRoot: makeCaddyfile(t, content)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass != (len(out.Violations) == 0) {
		t.Fatalf("Pass=%v disagrees with %d violation(s)", out.Pass, len(out.Violations))
	}
	return out.Violations
}

func TestCheck_UnreadableCaddyfileIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "infra", "local", "Caddyfile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: dir}); err == nil {
		t.Fatal("a Caddyfile that cannot be read must be an error")
	}
}

func TestCheck_AppDevBlockWithoutHeaderFailsTheSanityCheck(t *testing.T) {
	// Every canonical line mentioning a header carries the substring, so the
	// only block that trips the sanity check is one with no header at all.
	bare := "app-dev.getloopscale.com {\n  file_server\n}\n"
	if !containsViolation(violationsFor(t, bare), "assert_app_dev_block_bounded") {
		t.Fatal("want the bounded-block sanity violation")
	}
}

func TestCheck_AuthDevRemoteHostKeyIsRefused(t *testing.T) {
	bad := strings.Replace(canonicalAppDevBlock, "key {http.request.header.cf-connecting-ip}\n      window 1m\n      events 10", "key {remote_host}\n      window 1m\n      events 10", 1)
	if !containsViolation(violationsFor(t, bad), "key {remote_host}") {
		t.Fatal("want the remote_host key violation")
	}
}

func TestCheck_AuthDevWithoutCFKeyIsRefused(t *testing.T) {
	bad := strings.ReplaceAll(canonicalAppDevBlock, "key {http.request.header.cf-connecting-ip}", "key {remote_host}")
	if !containsViolation(violationsFor(t, bad), "no `key {http.request.header.cf-connecting-ip}`") {
		t.Fatal("want the missing cf-connecting-ip key violation")
	}
}

func TestCheck_RegisterRouteToWrongTargetIsRefused(t *testing.T) {
	bad := strings.Replace(canonicalAppDevBlock, "handle /self-service/registration/methods/password {\n    reverse_proxy localhost:3002", "handle /self-service/registration/methods/password {\n    reverse_proxy localhost:14433", 1)
	if !containsViolation(violationsFor(t, bad), "does NOT reverse_proxy to localhost:3002") {
		t.Fatal("want the wrong-target violation")
	}
}

func TestCheck_UnbalancedTrustedProxiesBlockIsRefused(t *testing.T) {
	bad := strings.Replace(canonicalAppDevBlock, "trusted_proxies static \\\n      173.245.48.0/20", "trusted_proxies static {\n      173.245.48.0/20", 1)
	bad = strings.Replace(bad, "2c0f:f248::/32\n  }\n}", "2c0f:f248::/32\n", 1)
	if !containsViolation(violationsFor(t, bad), "assert_trusted_proxies_block_extractable") {
		t.Fatal("want the unextractable trusted_proxies violation")
	}
}
