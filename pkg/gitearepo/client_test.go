// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitearepo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeGitea records every request and answers from a script keyed by
// "METHOD path".
type fakeGitea struct {
	t       *testing.T
	mu      sync.Mutex
	calls   []string
	bodies  map[string]map[string]any
	answers map[string]fakeAnswer
}

type fakeAnswer struct {
	status int
	body   string
}

func newFakeGitea(t *testing.T, answers map[string]fakeAnswer) (*fakeGitea, *Client) {
	t.Helper()
	f := &fakeGitea{t: t, bodies: map[string]map[string]any{}, answers: answers}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, New(srv.URL+"/", "tok", "loopzero", srv.Client())
}

func (f *fakeGitea) serve(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.calls = append(f.calls, key)
	if r.Body != nil && r.ContentLength != 0 {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			f.bodies[key] = body
		}
	}
	f.mu.Unlock()
	if got := r.Header.Get("Authorization"); got != "token tok" {
		f.t.Errorf("%s: Authorization = %q, want %q", key, got, "token tok")
	}
	a, ok := f.answers[key]
	if !ok {
		f.t.Errorf("unexpected request %s", key)
		w.WriteHeader(http.StatusTeapot)
		return
	}
	w.WriteHeader(a.status)
	_, _ = w.Write([]byte(a.body))
}

func (f *fakeGitea) called(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == key {
			return true
		}
	}
	return false
}

const (
	repoPath   = "/api/v1/repos/loopzero/acme-api"
	createPath = "/api/v1/orgs/loopzero/repos"
)

func TestEnsureRepo_LeavesAnActiveRepositoryAlone(t *testing.T) {
	f, c := newFakeGitea(t, map[string]fakeAnswer{
		"GET " + repoPath: {http.StatusOK, `{"archived":false}`},
	})
	if err := c.EnsureRepo(context.Background(), "acme-api", EnsureOptions{}); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if f.called("PATCH "+repoPath) || f.called("POST "+createPath) {
		t.Errorf("an active repository must not be modified; calls = %v", f.calls)
	}
}

func TestEnsureRepo_UnarchivesARepositoryArchivedByAnEarlierDeletion(t *testing.T) {
	f, c := newFakeGitea(t, map[string]fakeAnswer{
		"GET " + repoPath:   {http.StatusOK, `{"archived":true}`},
		"PATCH " + repoPath: {http.StatusOK, `{"archived":false}`},
	})
	if err := c.EnsureRepo(context.Background(), "acme-api", EnsureOptions{}); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if got := f.bodies["PATCH "+repoPath]["archived"]; got != false {
		t.Errorf("PATCH body archived = %v, want false", got)
	}
}

func TestEnsureRepo_FailsWhenUnarchivingFails(t *testing.T) {
	_, c := newFakeGitea(t, map[string]fakeAnswer{
		"GET " + repoPath:   {http.StatusOK, `{"archived":true}`},
		"PATCH " + repoPath: {http.StatusForbidden, ``},
	})
	err := c.EnsureRepo(context.Background(), "acme-api", EnsureOptions{})
	var status *StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusForbidden {
		t.Fatalf("EnsureRepo err = %v, want a StatusError carrying 403", err)
	}
}

func TestEnsureRepo_CreatesAMissingRepositoryPrivateOnTheDefaultBranch(t *testing.T) {
	f, c := newFakeGitea(t, map[string]fakeAnswer{
		"GET " + repoPath:    {http.StatusNotFound, ``},
		"POST " + createPath: {http.StatusCreated, `{}`},
	})
	if err := c.EnsureRepo(context.Background(), "acme-api", EnsureOptions{AutoInit: true}); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	body := f.bodies["POST "+createPath]
	if body["name"] != "acme-api" || body["private"] != true || body["auto_init"] != true || body["default_branch"] != DefaultBranch {
		t.Errorf("create body = %v, want name=acme-api private=true auto_init=true default_branch=%s", body, DefaultBranch)
	}
}

func TestEnsureRepo_CreatesOnTheBranchTheCallerNames(t *testing.T) {
	f, c := newFakeGitea(t, map[string]fakeAnswer{
		"GET " + repoPath:    {http.StatusNotFound, ``},
		"POST " + createPath: {http.StatusCreated, `{}`},
	})
	if err := c.EnsureRepo(context.Background(), "acme-api", EnsureOptions{DefaultBranch: "trunk"}); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if got := f.bodies["POST "+createPath]["default_branch"]; got != "trunk" {
		t.Errorf("default_branch = %v, want trunk", got)
	}
	if got := f.bodies["POST "+createPath]["auto_init"]; got != false {
		t.Errorf("auto_init = %v, want false", got)
	}
}

func TestEnsureRepo_TreatsACreateConflictAsAlreadyCreated(t *testing.T) {
	_, c := newFakeGitea(t, map[string]fakeAnswer{
		"GET " + repoPath:    {http.StatusNotFound, ``},
		"POST " + createPath: {http.StatusConflict, ``},
	})
	if err := c.EnsureRepo(context.Background(), "acme-api", EnsureOptions{}); err != nil {
		t.Fatalf("EnsureRepo on 409 = %v, want nil (a concurrent caller created it)", err)
	}
}

func TestEnsureRepo_ReportsACreateRejectionWithItsStatus(t *testing.T) {
	_, c := newFakeGitea(t, map[string]fakeAnswer{
		"GET " + repoPath:    {http.StatusNotFound, ``},
		"POST " + createPath: {http.StatusInsufficientStorage, ``},
	})
	err := c.EnsureRepo(context.Background(), "acme-api", EnsureOptions{})
	var status *StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("EnsureRepo err = %v, want a StatusError carrying 507", err)
	}
}

func TestEnsureRepo_DoesNotCreateWhenTheLookupFails(t *testing.T) {
	// A 5xx on the lookup says nothing about whether the repository exists;
	// creating on it would turn an outage into a spurious create attempt.
	f, c := newFakeGitea(t, map[string]fakeAnswer{
		"GET " + repoPath: {http.StatusInternalServerError, ``},
	})
	err := c.EnsureRepo(context.Background(), "acme-api", EnsureOptions{})
	var status *StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusInternalServerError {
		t.Fatalf("EnsureRepo err = %v, want a StatusError carrying 500", err)
	}
	if f.called("POST " + createPath) {
		t.Error("EnsureRepo must not create after a failed lookup")
	}
}

func TestArchiveRepo_SendsArchivedTrue(t *testing.T) {
	f, c := newFakeGitea(t, map[string]fakeAnswer{
		"PATCH " + repoPath: {http.StatusOK, `{"archived":true}`},
	})
	if err := c.ArchiveRepo(context.Background(), "acme-api"); err != nil {
		t.Fatalf("ArchiveRepo: %v", err)
	}
	if got := f.bodies["PATCH "+repoPath]["archived"]; got != true {
		t.Errorf("PATCH body archived = %v, want true", got)
	}
}

func TestArchiveRepo_IsIdempotentForAMissingRepository(t *testing.T) {
	_, c := newFakeGitea(t, map[string]fakeAnswer{
		"PATCH " + repoPath: {http.StatusNotFound, ``},
	})
	if err := c.ArchiveRepo(context.Background(), "acme-api"); err != nil {
		t.Fatalf("ArchiveRepo on 404 = %v, want nil", err)
	}
}

func TestRepoURL_IsBuiltFromTheConfiguredBaseURL(t *testing.T) {
	c := New("http://gitea.loopzero-system.svc:3000/", "tok", "loopzero", nil)
	if got, want := c.RepoURL("acme-api"), "http://gitea.loopzero-system.svc:3000/loopzero/acme-api.git"; got != want {
		t.Errorf("RepoURL = %q, want %q", got, want)
	}
}

func TestNew_DefaultsToAClientWithATimeout(t *testing.T) {
	c := New("http://gitea", "tok", "loopzero", nil)
	if c.http == http.DefaultClient || c.http.Timeout <= 0 {
		t.Fatalf("default client has no timeout (%v): a hung server would block the caller forever", c.http.Timeout)
	}
}
