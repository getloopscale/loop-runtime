// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/cliout"
)

// fakeEdge serves every path with status and returns a leakEnv pointed at it.
func fakeEdge(t *testing.T, status func(path string) int) (leakEnv, string, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status(r.URL.Path))
	}))
	t.Cleanup(srv.Close)
	cli := srv.Client()
	cli.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var out, errOut bytes.Buffer
	env := leakEnv{
		stdout: cliout.New(&out), stderr: cliout.New(&errOut), client: cli,
		discovery:      func(context.Context, *http.Client, string) []string { return nil },
		placeholders:   func() []string { return nil },
		listeningPorts: func() ([]int, error) { return nil, nil },
	}
	return env, strings.TrimPrefix(srv.URL, "https://"), &out, &errOut
}

func writeLeakInventory(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "proxy-exposure.txt")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func alwaysDeny(string) int { return http.StatusUnauthorized }

// identityOK answers the identity routes that must be public and denies the rest.
func identityOK(path string) int {
	switch path {
	case "/.well-known/openid-configuration", "/health/alive", "/":
		return http.StatusOK
	}
	return http.StatusUnauthorized
}

func TestRunLeak_HelpExitsZero(t *testing.T) {
	env, _, out, _ := fakeEdge(t, alwaysDeny)
	if rc := runLeakWith(nil, env); rc != 0 || !strings.Contains(out.String(), "USAGE") {
		t.Fatalf("rc=%d out=%q", rc, out.String())
	}
}

func TestRunLeak_UnknownSubcommandIsAUsageError(t *testing.T) {
	env, _, _, errOut := fakeEdge(t, alwaysDeny)
	if rc := runLeakWith([]string{"bogus"}, env); rc != 1 || !strings.Contains(errOut.String(), "unknown subcommand") {
		t.Fatalf("rc=%d stderr=%q", rc, errOut.String())
	}
}

func TestRunLeakLive_EmptyHostIsRefused(t *testing.T) {
	env, _, _, errOut := fakeEdge(t, alwaysDeny)
	if rc := runLeakWith([]string{"live", "--host", ""}, env); rc != 1 || !strings.Contains(errOut.String(), "--host is empty") {
		t.Fatalf("rc=%d stderr=%q", rc, errOut.String())
	}
}

func TestRunLeakLive_MissingInventoryFails(t *testing.T) {
	env, _, _, _ := fakeEdge(t, alwaysDeny)
	if rc := runLeakWith([]string{"live", "--inventory", "/nonexistent"}, env); rc != 1 {
		t.Fatalf("rc=%d, want 1", rc)
	}
}

func TestRunLeakLive_ClosedEdgeExitsZero(t *testing.T) {
	env, host, out, _ := fakeEdge(t, identityOK)
	inv := writeLeakInventory(t, host+" /v1/* | reviewed | BFF\n")
	rc := runLeakWith([]string{"live", "--inventory", inv, "--auth-host", host}, env)
	if rc != 0 {
		t.Fatalf("rc=%d out=%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "The edge closes") {
		t.Errorf("stdout = %s", out.String())
	}
}

func TestRunLeakLive_ALeakExitsOneAndIsNamedAsALeak(t *testing.T) {
	env, host, _, errOut := fakeEdge(t, func(p string) int {
		if p == "/v1/dashboard/status/versions" {
			return http.StatusOK
		}
		return identityOK(p)
	})
	inv := writeLeakInventory(t, host+" /v1/* | reviewed | BFF\n")
	if rc := runLeakWith([]string{"live", "--inventory", inv, "--auth-host", host}, env); rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(errOut.String(), "let something through") {
		t.Errorf("stderr = %s", errOut.String())
	}
}

// A run whose only problem is a coverage hole must not announce a leak: the
// two call for different actions.
func TestRunLeakLive_CoverageHoleOnlyIsNotAnnouncedAsALeak(t *testing.T) {
	env, host, _, errOut := fakeEdge(t, identityOK)
	inv := writeLeakInventory(t, host+" /v1/unmapped/* | reviewed | new surface\n")
	if rc := runLeakWith([]string{"live", "--inventory", inv, "--auth-host", host}, env); rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if strings.Contains(errOut.String(), "let something through") {
		t.Errorf("a coverage hole was announced as a leak: %s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "Coverage holes") {
		t.Errorf("stderr = %s", errOut.String())
	}
}

// The --json field names are a stability contract.
func TestRunLeakLive_JSONKeepsItsFieldNames(t *testing.T) {
	env, host, out, _ := fakeEdge(t, identityOK)
	inv := writeLeakInventory(t, host+" /v1/* | reviewed | BFF\n")
	if rc := runLeakWith([]string{"live", "--json", "--inventory", inv, "--host", host, "--auth-host", host}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	for _, k := range []string{"pass", "fail", "skip", "uncovered", "failures", "not_measured"} {
		if _, ok := got[k]; !ok {
			t.Errorf("JSON is missing %q: %s", k, out.String())
		}
	}
}

func TestParseListeningPorts_KeepsOnlyAllInterfaceBinds(t *testing.T) {
	ss := `State  Recv-Q Send-Q Local Address:Port Peer Address:Port
LISTEN 0      4096   0.0.0.0:3002      0.0.0.0:*
LISTEN 0      4096   127.0.0.1:5432    0.0.0.0:*
LISTEN 0      4096   [::]:22           [::]:*
LISTEN 0      4096   0.0.0.0:3002      0.0.0.0:*
`
	got := parseListeningPorts(ss)
	if len(got) != 2 || got[0] != 22 || got[1] != 3002 {
		t.Errorf("ports = %v, want [22 3002]", got)
	}
}

func writePortRegistry(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "host-ports.txt")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunLeakPorts_UndeclaredPortFails(t *testing.T) {
	env, _, _, errOut := fakeEdge(t, alwaysDeny)
	env.listeningPorts = func() ([]int, error) { return []int{3002, 30082}, nil }
	reg := writePortRegistry(t, "3002 | edge | loopzero-cloud through the edge\n")
	if rc := runLeakWith([]string{"ports", "--registry", reg}, env); rc != 1 || !strings.Contains(errOut.String(), "1 port(s)") {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
}

func TestRunLeakPorts_UnreadableListingFails(t *testing.T) {
	env, _, _, _ := fakeEdge(t, alwaysDeny)
	env.listeningPorts = func() ([]int, error) { return nil, errors.New("ss: not found") }
	reg := writePortRegistry(t, "3002 | edge | loopzero-cloud through the edge\n")
	if rc := runLeakWith([]string{"ports", "--registry", reg}, env); rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
}

// The English --json keys are the contract; the original keys are still
// emitted with the same values so existing consumers keep working.
func TestRunLeakPorts_JSONCarriesEnglishAndDeprecatedKeys(t *testing.T) {
	env, _, out, _ := fakeEdge(t, alwaysDeny)
	env.listeningPorts = func() ([]int, error) { return []int{3002}, nil }
	reg := writePortRegistry(t, "3002 | edge | loopzero-cloud through the edge\n")
	if rc := runLeakWith([]string{"ports", "--json", "--registry", reg}, env); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	pairs := [][2]string{
		{"listening", "escutando"}, {"declared", "declaradas"}, // english-only: deprecated --json keys under test
		{"undeclared", "nao_declaradas"}, {"stale", "obsoletas"}, {"to_close", "a_fechar"}, // english-only: deprecated --json keys under test
	}
	for _, p := range pairs {
		a, okA := got[p[0]]
		b, okB := got[p[1]]
		if !okA || !okB {
			t.Errorf("missing %q or %q in %s", p[0], p[1], out.String())
			continue
		}
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Errorf("%s=%s but %s=%s", p[0], ja, p[1], jb)
		}
	}
}
