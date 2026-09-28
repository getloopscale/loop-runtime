// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/getloopscale/loopzero/pkg/gitearepo"
)

// recordingGitea answers the repository API from status codes and records the
// create body.
type recordingGitea struct {
	mu        sync.Mutex
	created   map[string]any
	calls     []string
	getStatus int
}

func (g *recordingGitea) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/loopzero/loopzero-source-tn-acme-web":
		w.WriteHeader(g.getStatus)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/loopzero/repos":
		_ = json.NewDecoder(r.Body).Decode(&g.created)
		w.WriteHeader(http.StatusCreated)
	default:
		// git's smart-HTTP clone/push of the empty repository: refuse, the
		// test is about what happened before it.
		w.WriteHeader(http.StatusNotFound)
	}
}

func newRecordingStore(t *testing.T, getStatus int) (*GiteaSourceStore, *recordingGitea) {
	t.Helper()
	g := &recordingGitea{getStatus: getStatus}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{BaseURL: srv.URL, Token: "tok", OrgName: "loopzero"})
	if err != nil {
		t.Fatalf("NewGiteaSourceStore: %v", err)
	}
	return store, g
}

// A source repository is created empty: the bundle brings its own history.
func TestPushGitBundle_CreatesAMissingSourceRepoWithoutAutoInit(t *testing.T) {
	store, g := newRecordingStore(t, http.StatusNotFound)

	_, _ = store.pushGitBundle(context.Background(), "tn-acme", "web", []byte("not a real bundle"))

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.created == nil {
		t.Fatalf("no repository was created; calls = %v", g.calls)
	}
	if g.created["auto_init"] != false || g.created["default_branch"] != gitearepo.DefaultBranch || g.created["private"] != true {
		t.Errorf("create body = %v, want auto_init=false default_branch=%s private=true", g.created, gitearepo.DefaultBranch)
	}
}

// A failed lookup says nothing about whether the repository exists, so the
// push stops there instead of attempting a create.
func TestPushGitBundle_StopsWithoutCreatingWhenTheRepoLookupFails(t *testing.T) {
	store, g := newRecordingStore(t, http.StatusInternalServerError)

	_, err := store.pushGitBundle(context.Background(), "tn-acme", "web", []byte("bundle"))

	var status *gitearepo.StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusInternalServerError {
		t.Fatalf("pushGitBundle err = %v, want a StatusError carrying 500", err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.created != nil {
		t.Errorf("a repository was created after a failed lookup; calls = %v", g.calls)
	}
}
