// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubHydraAdminServer models GET/POST/PUT on /admin/clients/{id}. `existing`
// controls whether the client is initially present. `puts` and `posts` are
// return-parameter counters so tests can assert which path fired.
func stubHydraAdminServer(t *testing.T, existing bool) (server *httptest.Server, gets, posts, puts *int) {
	t.Helper()
	getsRef, postsRef, putsRef := new(int), new(int), new(int)
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health/ready":
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/admin/clients/") && r.Method == http.MethodGet:
			*getsRef++
			if existing {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"client_id": "irrelevant"})
			} else {
				http.NotFound(w, r)
			}
		case r.URL.Path == "/admin/clients" && r.Method == http.MethodPost:
			*postsRef++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"client_id": "created"})
		case strings.HasPrefix(r.URL.Path, "/admin/clients/") && r.Method == http.MethodPut:
			*putsRef++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"client_id": "updated"})
		default:
			http.NotFound(w, r)
		}
	}))
	return server, getsRef, postsRef, putsRef
}

func TestAdminHydraRegisterOrRefresh_CreatesNew(t *testing.T) {
	server, _, posts, puts := stubHydraAdminServer(t, false /* existing */)
	defer server.Close()
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "sec")

	got, err := adminHydraRegisterOrRefresh(context.Background(), server.URL,
		"user-uuid-1", secretPath)

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == "" {
		t.Fatal("empty secret returned")
	}
	if *posts != 1 || *puts != 0 {
		t.Errorf("posts=%d puts=%d, want 1 posts 0 puts", *posts, *puts)
	}
}

func TestAdminHydraRegisterOrRefresh_RefreshesExisting(t *testing.T) {
	server, _, posts, puts := stubHydraAdminServer(t, true)
	defer server.Close()
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "sec")

	got, err := adminHydraRegisterOrRefresh(context.Background(), server.URL,
		"user-uuid-1", secretPath)

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == "" {
		t.Fatal("empty secret returned")
	}
	if *posts != 0 || *puts != 1 {
		t.Errorf("posts=%d puts=%d, want 0 posts 1 puts", *posts, *puts)
	}
}

func TestAdminHydraRegisterOrRefresh_ReusesPersistedSecret(t *testing.T) {
	server, _, _, _ := stubHydraAdminServer(t, true)
	defer server.Close()
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "sec")
	if err := os.WriteFile(secretPath, []byte("existing-secret"), 0o600); err != nil {
		t.Fatalf("seed existing secret: %v", err)
	}

	got, err := adminHydraRegisterOrRefresh(context.Background(), server.URL,
		"user-uuid-1", secretPath)

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "existing-secret" {
		t.Errorf("secret = %q, want existing-secret (reuse)", got)
	}
}

func TestAdminHydraRegisterOrRefresh_ServerErrorIsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "server exploded", http.StatusInternalServerError)
	}))
	defer server.Close()
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "sec")

	_, err := adminHydraRegisterOrRefresh(context.Background(), server.URL,
		"user-uuid-1", secretPath)

	if err == nil {
		t.Fatal("want error on 500, got nil")
	}
}

func TestWriteSecretFile_Mode0600(t *testing.T) {
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "nested", "dir", "sec")

	if err := writeSecretFile(secretPath, "hunter2"); err != nil {
		t.Fatalf("writeSecretFile: %v", err)
	}
	info, err := os.Stat(secretPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
	got, _ := os.ReadFile(secretPath)
	if string(got) != "hunter2" {
		t.Errorf("content = %q", got)
	}
}

func TestMustHexSecret_LengthAndHex(t *testing.T) {
	got := mustHexSecret(32)
	if len(got) != 64 { // hex encoding = 2 chars per byte
		t.Errorf("hex len = %d, want 64", len(got))
	}
	for _, r := range got {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Errorf("non-hex rune %q", r)
			break
		}
	}
}

func TestAdminHydraClientSeed_Name(t *testing.T) {
	if got := NewAdminHydraClientSeed().Name(); got != "admin-hydra-client" {
		t.Errorf("Name = %q, want admin-hydra-client", got)
	}
}
