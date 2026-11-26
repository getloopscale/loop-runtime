// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

// The claim asks GitHub with the App's OAuth client credentials from the
// environment and the GitHub roots from the configuration.
func TestNewUserInstallationSource_AsksTheConfiguredGitHubWithTheAppsCredentials(t *testing.T) {
	var gotClientID, gotSecret string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotClientID, gotSecret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		_, _ = w.Write([]byte(`{"access_token":"ghu_x"}`))
	})
	mux.HandleFunc("GET /user/installations", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"total_count":2,"installations":[
			{"id":101,"app_id":77,"account":{"login":"acme","type":"Organization"}},
			{"id":202,"app_id":78,"account":{"login":"acme","type":"Organization"}}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_APP_CLIENT_ID", "Iv1.client")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "s3cret")

	source := newUserInstallationSource(77, config.Config{GitHubWebBaseURL: srv.URL, GitHubAPIBaseURL: srv.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if source == nil {
		t.Fatal("no source although the client id and secret are set")
	}
	got, err := source.InstallationsForCode(context.Background(), "code-1")

	if err != nil || len(got) != 1 || got[0].ID != 101 || got[0].AccountLogin != "acme" {
		t.Fatalf("InstallationsForCode = %+v, %v; want only installation 101 of app 77", got, err)
	}
	if gotClientID != "Iv1.client" || gotSecret != "s3cret" {
		t.Error("the exchange did not carry GITHUB_APP_CLIENT_ID and GITHUB_APP_CLIENT_SECRET")
	}
}

// Without the credentials there is no source, and the claim endpoint answers
// 503 instead of binding anything.
func TestNewUserInstallationSource_IsNilWithoutTheOAuthCredentials(t *testing.T) {
	for name, env := range map[string][2]string{
		"neither":   {"", ""},
		"no secret": {"Iv1.client", ""},
		"no id":     {"", "s3cret"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GITHUB_APP_CLIENT_ID", env[0])
			t.Setenv("GITHUB_APP_CLIENT_SECRET", env[1])

			if source := newUserInstallationSource(77, config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil))); source != nil {
				t.Fatalf("source = %v, want nil", source)
			}
		})
	}
}
