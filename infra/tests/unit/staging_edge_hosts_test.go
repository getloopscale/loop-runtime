// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Staging's edge (infra/staging/edge/Caddyfile) opens one site block for every hostname
// in EDGE_HOSTS through `{$EDGE_HOSTS} {`. Caddy expands the variable before it parses
// the file, and refuses a site address with a bare comma ("Site addresses cannot contain
// a comma"), so the value must separate the hostnames with ", ". `task staging:edge`
// writes it into /etc/loopruntime-edge/edge.env, which both systemd (EnvironmentFile) and
// the task's own `caddy validate` read; a value with spaces must be quoted for the shell
// and must not go through `env $(cat ...)`, which splits it into words.

func stagingEdgeTask(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatalf("read taskfiles/staging.yml: %v", err)
	}
	text := string(data)
	start := strings.Index(text, "\n  edge:\n")
	end := strings.Index(text, "\n  edge:verify:\n")
	if start < 0 || end < start {
		t.Fatal("no edge task in taskfiles/staging.yml")
	}
	return text[start:end]
}

var edgeEnvHostsLineRe = regexp.MustCompile(`(?m)^\s*EDGE_HOSTS=(.*)$`)

func TestStagingEdgeWritesTheHostsAsACaddySiteAddressList(t *testing.T) {
	task := stagingEdgeTask(t)

	m := edgeEnvHostsLineRe.FindStringSubmatch(task)
	if m == nil {
		t.Fatal("the edge task writes no EDGE_HOSTS line into edge.env")
	}
	value := m[1]
	if value == "{{.EDGE_HOSTS}}" {
		t.Errorf("edge.env gets EDGE_HOSTS verbatim (%s): a comma-separated list is not a Caddy site address list", value)
	}
	if !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, `"`) {
		t.Errorf("EDGE_HOSTS=%s is not quoted: the shell that sources edge.env splits a value with spaces", value)
	}
	if !strings.Contains(task, `sed 's/,/, /g'`) {
		t.Error("the edge task does not turn the comma-separated EDGE_HOSTS into a \", \"-separated list")
	}
}

func TestStagingEdgeValidatesWithTheEnvironmentFileAsSystemdReadsIt(t *testing.T) {
	task := stagingEdgeTask(t)

	if strings.Contains(task, "env $(cat /etc/loopruntime-edge/edge.env)") {
		t.Error("caddy validate reads edge.env through `env $(cat ...)`, which splits a value with spaces into words")
	}
	if !strings.Contains(task, ". /etc/loopruntime-edge/edge.env") {
		t.Error("caddy validate does not source edge.env")
	}
}
