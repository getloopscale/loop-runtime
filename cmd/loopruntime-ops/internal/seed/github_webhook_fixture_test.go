// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ghAPIStub records every gh api invocation + returns canned bytes per path.
type ghAPIStub struct {
	responses map[string][]byte // key = URL path in argv
	calls     [][]string
	lastBody  []byte
	errOnPath map[string]error
}

func (s *ghAPIStub) call(body []byte, args ...string) ([]byte, error) {
	s.calls = append(s.calls, args)
	if body != nil {
		s.lastBody = body
	}
	// find path — either bare `path` or `-X VERB path`.
	var path string
	for i, a := range args {
		if a == "-X" && i+2 < len(args) {
			path = args[i+2]
			break
		}
	}
	if path == "" && len(args) > 0 {
		path = args[0]
	}
	if err, ok := s.errOnPath[path]; ok {
		return nil, err
	}
	if b, ok := s.responses[path]; ok {
		return b, nil
	}
	return []byte(`{}`), nil
}

// ghAuthReturns is a Host.GHAuthCheck that always answers err.
func ghAuthReturns(err error) func() error { return func() error { return err } }

func makeGhWebhookCreds(whsec string) *Creds {
	c := &Creds{}
	c.GitHub.WebhookSecret = whsec
	return c
}

func TestGitHubWebhookFixture_SkipWhenCredsAbsent(t *testing.T) {
	deps := Deps{Ctx: context.Background(), Creds: nil}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestGitHubWebhookFixture_SkipWhenWebhookSecretEmpty(t *testing.T) {
	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("")}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestGitHubWebhookFixture_SkipWhenPlaceholder(t *testing.T) {
	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("REPLACE_ME")}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestGitHubWebhookFixture_FailWhenGhNotAuthed(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(errors.New("not logged in"))
	deps := Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "gh not authenticated") {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestGitHubWebhookFixture_FailWhenNotAdmin(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(nil)
	notAdmin, _ := json.Marshal(ghRepoPermissions{Permissions: struct {
		Admin bool `json:"admin"`
	}{Admin: false}})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example": notAdmin,
	}}
	host.GHAPI = stub.call
	deps := Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "admin permission") {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestGitHubWebhookFixture_CreatesNewHook(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(nil)
	admin, _ := json.Marshal(ghRepoPermissions{Permissions: struct {
		Admin bool `json:"admin"`
	}{Admin: true}})
	emptyHooks, _ := json.Marshal([]ghWebhook{})
	createdHook, _ := json.Marshal(ghWebhook{ID: 42, Config: ghWebhookConfig{URL: ghFixtureDefaultWebhookURL}})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example":       admin,
		"/repos/getloopscale/loopzerokit-example/hooks": emptyHooks,
	}}
	// The POST path is same as GET — override via error-then-response. Simpler:
	// intercept a second call to /hooks by re-mapping after first call. Instead,
	// we drive the POST by returning createdHook when the argv contains `-X POST`.
	host.GHAPI = func(body []byte, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "POST" {
				return createdHook, nil
			}
		}
		return stub.call(body, args...)
	}

	deps := Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "POST ok") {
		t.Errorf("Reason = %q, want 'POST ok'", got.Reason)
	}
	if !strings.Contains(got.Reason, "42") {
		t.Errorf("Reason = %q, want hook 42", got.Reason)
	}
}

func TestGitHubWebhookFixture_PatchesExistingHook(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(nil)
	admin, _ := json.Marshal(ghRepoPermissions{Permissions: struct {
		Admin bool `json:"admin"`
	}{Admin: true}})
	existing, _ := json.Marshal([]ghWebhook{
		{ID: 99, Config: ghWebhookConfig{URL: ghFixtureDefaultWebhookURL}},
	})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example":       admin,
		"/repos/getloopscale/loopzerokit-example/hooks": existing,
	}}
	host.GHAPI = stub.call

	deps := Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "PATCH ok") {
		t.Errorf("Reason = %q, want 'PATCH ok'", got.Reason)
	}
	// Assert PATCH was actually sent to the hook path.
	patched := false
	for _, argv := range stub.calls {
		for i, a := range argv {
			if a == "-X" && i+2 < len(argv) && argv[i+1] == "PATCH" && strings.HasSuffix(argv[i+2], "/hooks/99") {
				patched = true
			}
		}
	}
	if !patched {
		t.Errorf("PATCH /hooks/99 not called; calls=%v", stub.calls)
	}
}

func TestGitHubWebhookFixture_DryRunReports(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(nil)
	admin, _ := json.Marshal(ghRepoPermissions{Permissions: struct {
		Admin bool `json:"admin"`
	}{Admin: true}})
	emptyHooks, _ := json.Marshal([]ghWebhook{})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example":       admin,
		"/repos/getloopscale/loopzerokit-example/hooks": emptyHooks,
	}}
	host.GHAPI = stub.call
	deps := Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc"), DryRun: true}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "dry-run") {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestGitHubWebhookFixture_Verify_PassWhenHookExists(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(nil)
	existing, _ := json.Marshal([]ghWebhook{
		{ID: 7, Config: ghWebhookConfig{URL: ghFixtureDefaultWebhookURL}},
	})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example/hooks": existing,
	}}
	host.GHAPI = stub.call
	deps := Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Verify(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestGitHubWebhookFixture_Verify_FailWhenNoHook(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(nil)
	emptyHooks, _ := json.Marshal([]ghWebhook{})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example/hooks": emptyHooks,
	}}
	host.GHAPI = stub.call
	deps := Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Verify(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestGhHookBody_ContainsCanonicalFields(t *testing.T) {
	body, err := ghHookBody("https://example/webhook", "whsec_secret", false)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got["active"] != true {
		t.Errorf("active = %v, want true", got["active"])
	}
	if _, named := got["name"]; named {
		t.Errorf("PATCH body must not carry name; got %v", got)
	}
	cfg := got["config"].(map[string]any)
	for k, want := range map[string]string{
		"url": "https://example/webhook", "secret": "whsec_secret",
		"content_type": "json", "insecure_ssl": "0",
	} {
		if cfg[k] != want {
			t.Errorf("config[%s] = %v, want %q", k, cfg[k], want)
		}
	}
	events := got["events"].([]any)
	if len(events) != 2 || events[0] != "push" || events[1] != "pull_request" {
		t.Errorf("events = %v", events)
	}
}

func TestGhHookBody_CreateCarriesName(t *testing.T) {
	body, err := ghHookBody("https://example/webhook", "whsec_secret", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"name":"web"`) {
		t.Errorf("create body missing name=web: %s", body)
	}
}

func TestGitHubWebhookFixture_Name(t *testing.T) {
	if got := NewGitHubWebhookFixtureSeed().Name(); got != "github-webhook-fixture" {
		t.Errorf("Name = %q", got)
	}
}

// The webhook secret is an HMAC key. It must never reach the gh argv — argv is
// world-readable in the process table and was echoed verbatim into the error a
// failing PATCH/POST returned, which Run copied into Result.Reason and the seed
// printed to stdout and --json.
func TestGhHookMutation_KeepsSecretOutOfArgv(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(nil)
	const whsec = "whsec_must_not_leak_0123456789"
	admin, _ := json.Marshal(ghRepoPermissions{Permissions: struct {
		Admin bool `json:"admin"`
	}{Admin: true}})
	existing, _ := json.Marshal([]ghWebhook{
		{ID: 99, Config: ghWebhookConfig{URL: ghFixtureDefaultWebhookURL}},
	})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example":       admin,
		"/repos/getloopscale/loopzerokit-example/hooks": existing,
	}}
	host.GHAPI = stub.call

	got := NewGitHubWebhookFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds(whsec)})
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	for _, argv := range stub.calls {
		if strings.Contains(strings.Join(argv, " "), whsec) {
			t.Fatalf("webhook secret reached gh argv: %v", argv)
		}
	}
	if !strings.Contains(string(stub.lastBody), whsec) {
		t.Fatalf("PATCH body on stdin does not carry the secret; body=%q", stub.lastBody)
	}
}

func TestGitHubWebhookFixture_FailedPatchReasonOmitsSecret(t *testing.T) {
	var host Host
	host.GHAuthCheck = ghAuthReturns(nil)
	const whsec = "whsec_must_not_leak_0123456789"
	admin, _ := json.Marshal(ghRepoPermissions{Permissions: struct {
		Admin bool `json:"admin"`
	}{Admin: true}})
	existing, _ := json.Marshal([]ghWebhook{
		{ID: 99, Config: ghWebhookConfig{URL: ghFixtureDefaultWebhookURL}},
	})
	stub := &ghAPIStub{
		responses: map[string][]byte{
			"/repos/getloopscale/loopzerokit-example":       admin,
			"/repos/getloopscale/loopzerokit-example/hooks": existing,
		},
		errOnPath: map[string]error{"/repos/getloopscale/loopzerokit-example/hooks/99": errors.New("HTTP 422")},
	}
	host.GHAPI = stub.call

	got := NewGitHubWebhookFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), Creds: makeGhWebhookCreds(whsec)})
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if strings.Contains(got.Reason, whsec) {
		t.Fatalf("Reason leaks the webhook secret: %q", got.Reason)
	}
}

// defaultGhAPI is exercised against a fake `gh` on PATH that fails, so the real
// error formatting is what gets asserted.
func TestDefaultGhAPI_ErrorNamesVerbAndPathOnly(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "gh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'Validation Failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	const whsec = "whsec_must_not_leak_0123456789"
	_, err := defaultGhAPI([]byte(`{"config":{"secret":"`+whsec+`"}}`), "-X", "PATCH", "/repos/o/r/hooks/1", "--input", "-")
	if err == nil {
		t.Fatal("expected an error from the failing fake gh")
	}
	msg := err.Error()
	if strings.Contains(msg, whsec) {
		t.Fatalf("error leaks the secret: %q", msg)
	}
	for _, want := range []string{"PATCH", "/repos/o/r/hooks/1", "Validation Failed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
}
