package argocd

import (
	"context"
	"os"
	"testing"
	"time"
)

// These tests run against real ArgoCD.
// Set ARGOCD_TEST_URL and ARGOCD_TEST_TOKEN to enable.
// Without them, tests are skipped — not faked.

func skipIfNoArgoCD(t *testing.T) (string, string) {
	t.Helper()
	url := os.Getenv("ARGOCD_TEST_URL")
	if url == "" {
		t.Skip("ARGOCD_TEST_URL not set — skipping ArgoCD integration test")
	}
	token := os.Getenv("ARGOCD_TEST_TOKEN")
	if token == "" {
		t.Skip("ARGOCD_TEST_TOKEN not set — skipping ArgoCD integration test")
	}
	return url, token
}

func newTestSyncClient(t *testing.T) *SyncClient {
	t.Helper()
	url, token := skipIfNoArgoCD(t)

	return NewSyncClient(SyncClientConfig{
		BaseURL:  url,
		Token:    token,
		Insecure: true, // k3d uses self-signed certs
	})
}

func TestSyncClient_TriggerSyncNonExistentApp(t *testing.T) {
	client := newTestSyncClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.TriggerSync(ctx, "nonexistent-app-12345")
	if err == nil {
		t.Fatal("expected error for non-existent application")
	}
	t.Logf("expected error: %v", err)
}

func TestSyncClient_WaitForSyncTimeout(t *testing.T) {
	client := newTestSyncClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.WaitForSync(ctx, "nonexistent-app-12345", 2*time.Second)
	if err == nil {
		t.Fatal("expected error for non-existent application")
	}
	t.Logf("expected error: %v", err)
}
