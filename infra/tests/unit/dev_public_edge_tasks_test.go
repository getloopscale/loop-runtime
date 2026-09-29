// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// taskBlock returns the body of a task in taskfiles/infra.yml, up to the next task.
func taskBlock(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "infra.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	start := strings.Index(body, "\n  "+name+":\n")
	if start < 0 {
		t.Fatalf("task %s not found", name)
	}
	rest := body[start+len(name)+4:]
	if end := regexp.MustCompile(`\n  [a-z][a-z0-9:-]*:\n`).FindStringIndex(rest); end != nil {
		rest = rest[:end[0]]
	}
	return rest
}

// dev-public:dns required the Cloudflare token and the droplet IP in the environment, and
// no cleanroom step puts them there, so it failed its precondition on every run (#455).
// The token comes from the operator's credentials file, and the IP from the droplet's own
// metadata service when the operator did not set one.
func TestDevPublicDNSReadsItsInputsWithoutAHandExportedEnvironment(t *testing.T) {
	b := taskBlock(t, "dev-public:dns")
	if strings.Contains(b, `test -n "${CLOUDFLARE_API_TOKEN:-}"`) {
		t.Error("dev-public:dns still demands CLOUDFLARE_API_TOKEN as a precondition, before anything could load it")
	}
	if !strings.Contains(b, "go run . creds env") {
		t.Error("dev-public:dns does not load the operator's credentials file")
	}
	if !strings.Contains(b, "169.254.169.254/metadata/v1/interfaces/public/0/ipv4/address") {
		t.Error("dev-public:dns does not fall back to the droplet metadata service for its IP")
	}
}

// The edge issues wildcard certificates by DNS-01 with the same token, and the Caddy it
// starts reads it from its environment.
func TestDevPublicCaddyLoadsTheCloudflareToken(t *testing.T) {
	if !strings.Contains(taskBlock(t, "dev-public:caddy"), "go run . creds env") {
		t.Error("dev-public:caddy does not load the operator's credentials file")
	}
}

// caddyRunningWith runs the script's real is_caddy_running with a fake curl answering
// the admin API with the given exit code.
func caddyRunningWith(t *testing.T, curlExit string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "scripts", "local", "caddy-up-dev-public.sh"))
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?s)\nis_caddy_running\(\) \{.*?\n\}`).FindString(string(data))
	if fn == "" {
		t.Fatal("is_caddy_running not found in caddy-up-dev-public.sh")
	}
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "curl"), []byte("#!/bin/sh\nexit "+curlExit+"\n"), 0o755)
	_ = os.WriteFile(filepath.Join(bin, "pgrep"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	cmd := exec.Command("bash", "-c", "RUNTIME=/nowhere/Caddyfile\n"+fn+"\nis_caddy_running")
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin"}
	return cmd.Run() == nil
}

// The script found "its" Caddy by a process pattern on its own config path. The droplet's
// live edge was started elsewhere, so the script saw nothing, started a second Caddy on the
// same ports, and failed — and the new edge token never reached the live edge (#455). The
// admin API answers for whichever Caddy owns the edge; reload goes through it.
func TestTheLiveEdgeIsFoundByItsAdminAPINotItsProcessName(t *testing.T) {
	if !caddyRunningWith(t, "0") {
		t.Error("a Caddy answering the admin API was not seen as running")
	}
	if caddyRunningWith(t, "7") {
		t.Error("with no admin API answering, a Caddy was reported running")
	}
}
