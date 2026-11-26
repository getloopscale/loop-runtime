// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// notifyingProvider records OnAuthFailure calls, as the minting provider
// uses them to mint a fresh token.
type notifyingProvider struct{ failures int }

func (p *notifyingProvider) Token() (string, error) { return "expired", nil }
func (p *notifyingProvider) Refresh() error         { return nil }
func (p *notifyingProvider) OnAuthFailure()         { p.failures++ }

// Every call the convergence loop makes must tell the token provider when the
// token is rejected; otherwise an expired token observed there is never
// refreshed.
func TestEveryMethodSignalsATokenRejection(t *testing.T) {
	calls := map[string]func(c *SyncClient) error{
		"TriggerSync": func(c *SyncClient) error { return c.TriggerSync(context.Background(), "app") },
		"ForceReconcile": func(c *SyncClient) error {
			return c.ForceReconcile(context.Background(), "app")
		},
		"TerminateOperation": func(c *SyncClient) error {
			return c.TerminateOperation(context.Background(), "app")
		},
		"GetAppStatus": func(c *SyncClient) error {
			_, _, err := c.GetAppStatus(context.Background(), "app", nil)
			return err
		},
		"GetSyncRevision": func(c *SyncClient) error {
			_, err := c.GetSyncRevision(context.Background(), "app")
			return err
		},
		"ValidateToken": func(c *SyncClient) error { return c.ValidateToken(context.Background()) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			// Arrange
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer srv.Close()
			provider := &notifyingProvider{}
			client := mustNewSyncClient(t, SyncClientConfig{BaseURL: srv.URL, TokenProvider: provider, Timeout: 5 * time.Second})

			// Act
			err := call(client)

			// Assert
			if !errors.Is(err, loopzeroerrors.ErrDeployServiceAuthFailed) {
				t.Errorf("err = %v, want ErrDeployServiceAuthFailed", err)
			}
			if provider.failures == 0 {
				t.Error("the token provider was not told the token was rejected")
			}
		})
	}
}

// A server error on GetSyncRevision must keep the body, like every other call.
func TestGetSyncRevisionReportsTheServerMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("repo-server unavailable"))
	}))
	defer srv.Close()

	_, err := mustNewSyncClient(t, SyncClientConfig{BaseURL: srv.URL, Token: "t"}).GetSyncRevision(context.Background(), "app")

	if err == nil || !contains(err.Error(), "repo-server unavailable") {
		t.Fatalf("err = %v, want the server's message", err)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
