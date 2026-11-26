// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dogfood

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSession_ReadsTheFileTheVariableNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session")
	if err := os.WriteFile(path, []byte("abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionPathEnv, path)
	got, err := LoadSession()
	if err != nil || got != "abc123" {
		t.Fatalf("LoadSession() = %q, %v; want abc123", got, err)
	}
}

func TestLoadSession_RefusesAnUnsetVariableAndAnEmptyFile(t *testing.T) {
	t.Setenv(SessionPathEnv, "")
	if _, err := LoadSession(); err == nil || !strings.Contains(err.Error(), SessionPathEnv) {
		t.Fatalf("an unset variable gave %v; want an error that names it", err)
	}
	path := filepath.Join(t.TempDir(), "session")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionPathEnv, path)
	if _, err := LoadSession(); err == nil {
		t.Fatal("an empty session file was accepted")
	}
}

func TestBrowser_SendsTheSessionAndHandsBackTheRedirect(t *testing.T) {
	var cookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("ory_kratos_session"); err == nil {
			cookie = c.Value
		}
		http.Redirect(w, r, "/next?step=2", http.StatusFound)
	}))
	defer server.Close()
	env := Env{AuthBaseURL: server.URL, CloudBaseURL: server.URL, AppBaseURL: server.URL}
	browser, err := NewBrowser(env, "the-session")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := browser.Do(context.Background(), "GET", server.URL+"/start", "text/html", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if answer.Status != http.StatusFound || answer.Location != server.URL+"/next?step=2" {
		t.Fatalf("got status %d and location %q; want 302 and the absolute redirect, not followed", answer.Status, answer.Location)
	}
	if cookie != "the-session" {
		t.Fatalf("the request carried session %q", cookie)
	}
}

func TestBrowser_RefusesAHostOutsideTheEnvironment(t *testing.T) {
	env := Env{AuthBaseURL: "https://auth.example.test", CloudBaseURL: "https://cloud.example.test", AppBaseURL: "https://app.example.test"}
	browser, err := NewBrowser(env, "the-session")
	if err != nil {
		t.Fatal(err)
	}
	_, err = browser.Do(context.Background(), "GET", "https://github.com/login/oauth/authorize", "text/html", nil, false)
	if err == nil || !strings.Contains(err.Error(), "outside the environment") {
		t.Fatalf("a request to another host gave %v; want a refusal", err)
	}
}
