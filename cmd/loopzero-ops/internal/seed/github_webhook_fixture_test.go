// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// ghAPIStub records every gh api invocation + returns canned bytes per path.
type ghAPIStub struct {
	responses map[string][]byte // key = URL path in argv
	calls     [][]string
	errOnPath map[string]error
}

func (s *ghAPIStub) call(args ...string) ([]byte, error) {
	s.calls = append(s.calls, args)
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

func stubGhAPI(t *testing.T, stub *ghAPIStub) func() {
	t.Helper()
	orig := ghAPIFn
	ghAPIFn = stub.call
	return func() { ghAPIFn = orig }
}

func stubGhAuth(t *testing.T, err error) func() {
	t.Helper()
	orig := ghAuthCheckFn
	ghAuthCheckFn = func() error { return err }
	return func() { ghAuthCheckFn = orig }
}

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
	defer stubGhAuth(t, errors.New("not logged in"))()
	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "gh not authenticated") {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestGitHubWebhookFixture_FailWhenNotAdmin(t *testing.T) {
	defer stubGhAuth(t, nil)()
	notAdmin, _ := json.Marshal(ghRepoPermissions{Permissions: struct {
		Admin bool `json:"admin"`
	}{Admin: false}})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example": notAdmin,
	}}
	defer stubGhAPI(t, stub)()
	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "admin permission") {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestGitHubWebhookFixture_CreatesNewHook(t *testing.T) {
	defer stubGhAuth(t, nil)()
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
	orig := ghAPIFn
	defer func() { ghAPIFn = orig }()
	ghAPIFn = func(args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "POST" {
				return createdHook, nil
			}
		}
		return stub.call(args...)
	}

	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
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
	defer stubGhAuth(t, nil)()
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
	defer stubGhAPI(t, stub)()

	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
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
	defer stubGhAuth(t, nil)()
	admin, _ := json.Marshal(ghRepoPermissions{Permissions: struct {
		Admin bool `json:"admin"`
	}{Admin: true}})
	emptyHooks, _ := json.Marshal([]ghWebhook{})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example":       admin,
		"/repos/getloopscale/loopzerokit-example/hooks": emptyHooks,
	}}
	defer stubGhAPI(t, stub)()
	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc"), DryRun: true}
	got := NewGitHubWebhookFixtureSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "dry-run") {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestGitHubWebhookFixture_Verify_PassWhenHookExists(t *testing.T) {
	defer stubGhAuth(t, nil)()
	existing, _ := json.Marshal([]ghWebhook{
		{ID: 7, Config: ghWebhookConfig{URL: ghFixtureDefaultWebhookURL}},
	})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example/hooks": existing,
	}}
	defer stubGhAPI(t, stub)()
	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Verify(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestGitHubWebhookFixture_Verify_FailWhenNoHook(t *testing.T) {
	defer stubGhAuth(t, nil)()
	emptyHooks, _ := json.Marshal([]ghWebhook{})
	stub := &ghAPIStub{responses: map[string][]byte{
		"/repos/getloopscale/loopzerokit-example/hooks": emptyHooks,
	}}
	defer stubGhAPI(t, stub)()
	deps := Deps{Ctx: context.Background(), Creds: makeGhWebhookCreds("whsec_abc")}
	got := NewGitHubWebhookFixtureSeed().Verify(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestGhHookMutateArgs_ContainsCanonicalFields(t *testing.T) {
	args := ghHookMutateArgs("/repos/x/y/hooks/1", "PATCH", "https://example/webhook", "whsec_secret")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-X PATCH",
		"/repos/x/y/hooks/1",
		"active=true",
		"events[]=push",
		"events[]=pull_request",
		"config[url]=https://example/webhook",
		"config[secret]=whsec_secret",
		"config[insecure_ssl]=0",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q; got: %s", want, joined)
		}
	}
}

func TestGitHubWebhookFixture_Name(t *testing.T) {
	if got := NewGitHubWebhookFixtureSeed().Name(); got != "github-webhook-fixture" {
		t.Errorf("Name = %q", got)
	}
}
