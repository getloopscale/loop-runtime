// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/release/argocd"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// A rollback was confirmed by the health of the whole ArgoCD Application. One
// resource that never becomes ready, a Certificate for one, keeps the
// Application Progressing for ever: the cluster ran the target a second after
// the request, and five minutes later the API reported the rollback failed and
// kept the newer release active (#121). A deploy is confirmed by the Rollouts
// of its own apps, and a rollback now is too.

// argoApplication answers the ArgoCD API for one Application.
func argoApplication(t *testing.T, health string, resources []map[string]any) *argocd.SyncClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{
			"sync":      map[string]any{"status": "Synced", "revision": "abc123"},
			"health":    map[string]any{"status": health},
			"resources": resources,
		}})
	}))
	t.Cleanup(srv.Close)
	client, err := argocd.NewSyncClient(argocd.SyncClientConfig{BaseURL: srv.URL, Token: "test-token", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("argocd client: %v", err)
	}
	return client
}

func argoResource(kind, name, health string) map[string]any {
	return map[string]any{"kind": kind, "name": name, "health": map[string]any{"status": health}}
}

// rollbackOverArgo is a rollback of project shop, whose target release runs
// the app api, delivered by the container delivery against sync.
func rollbackOverArgo(t *testing.T, sync *argocd.SyncClient) (*RollbackOrchestrator, *fakeRollbackReleaseStore) {
	t.Helper()
	apps := json.RawMessage(`[{"name":"api","type":"server","port":3000}]`)
	rs := newFakeRollbackReleaseStore()
	current := &db.Release{
		ID: "rel-current", TenantID: "tn-1", ProjectID: "shop", Environment: "production",
		Status: string(statemachine.ReleaseActive), GitCommit: "def456", Apps: apps,
	}
	target := &db.Release{
		ID: "rel-target", TenantID: "tn-1", ProjectID: "shop", Environment: "production",
		Status: string(statemachine.ReleaseSuperseded), GitCommit: "abc123", Apps: apps,
	}
	rs.releases["rel-current"], rs.releases["rel-target"] = current, target
	rs.activeByEnv["tn-1-shop-production"] = current
	rs.previousByEnv["tn-1-shop-production"] = target
	delivery := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		Sync: sync, GitOps: &fakeGitOpsContainer{commitSHA: "sha"},
		GitReader: &fakeGitReaderContainer{manifests: []byte("m")}, Locker: &fakeLockerContainer{},
		ArgocdSyncTimeout: time.Minute,
	})
	return NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases: rs, EnvTargets: newFakeRollbackEnvTargetStore(), Locker: &fakeRollbackLocker{},
		Delivery: delivery, Logger: slog.Default(),
		// The wait polls every ten seconds; an unconfirmed rollback is
		// abandoned by this deadline instead.
		CompletionTimeout: 200 * time.Millisecond,
	}), rs
}

var shopRollback = RollbackRequest{TenantID: "tn-1", ProjectID: "shop", Environment: "production", UserID: "user-1"}

func TestRollback_CompletesWhileAnotherResourceKeepsTheApplicationProgressing(t *testing.T) {
	orch, releases := rollbackOverArgo(t, argoApplication(t, "Progressing", []map[string]any{
		argoResource("Rollout", "shop-api", "Healthy"),
		argoResource("Service", "shop-api", "Healthy"),
		argoResource("Certificate", "tls-shop-api", "Progressing"),
	}))

	if _, err := orch.Rollback(context.Background(), shopRollback); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	orch.Wait()

	if got := releases.status("rel-target"); got != string(statemachine.ReleaseActive) {
		t.Errorf("target release is %s, want ACTIVE: its Rollout is healthy and the Application is synced", got)
	}
	if got := releases.status("rel-current"); got != string(statemachine.ReleaseRolledBack) {
		t.Errorf("the release rolled back from is %s, want ROLLED_BACK", got)
	}
}

// The other direction stays closed: a target whose own Rollout is not healthy
// is not confirmed, whatever the rest of the Application says.
func TestRollback_IsNotConfirmedWhileTheTargetsRolloutIsNotHealthy(t *testing.T) {
	orch, releases := rollbackOverArgo(t, argoApplication(t, "Degraded", []map[string]any{
		argoResource("Rollout", "shop-api", "Degraded"),
		argoResource("Service", "shop-api", "Healthy"),
	}))

	if _, err := orch.Rollback(context.Background(), shopRollback); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	orch.Wait()

	if got := releases.status("rel-current"); got != string(statemachine.ReleaseActive) {
		t.Errorf("the release rolled back from is %s, want ACTIVE", got)
	}
	if got := releases.status("rel-target"); got == string(statemachine.ReleaseActive) {
		t.Error("the target is ACTIVE although its Rollout is Degraded")
	}
}

func TestRollbackTarget_NamesTheWorkloadsOfTheReleasesApps(t *testing.T) {
	target := rollbackTargetFrom(&db.Release{
		ID: "rel-1", ProjectID: "shop", GitCommit: "abc123",
		Apps: json.RawMessage(`[{"name":"api","type":"server"},{"name":"docs","type":"server","rendering":"static"},{"name":"worker","type":"worker"}]`),
	})

	// <project>-<app> is the name the renderer gives a Rollout; a static app
	// has none.
	if want := []string{"shop-api", "shop-worker"}; !reflect.DeepEqual(target.Apps, want) {
		t.Errorf("Apps = %v, want %v", target.Apps, want)
	}
}

// An abandoned rollback left the target's manifests in the GitOps repository
// while the API kept the other release active: the cluster could converge on
// the target later, or at the next sync (#118, #121). The manifests of the
// release kept active are delivered again.
func TestRollback_AbandonedRestoresTheManifestsOfTheReleaseKeptActive(t *testing.T) {
	f := newRollbackFixture(t)
	f.delivery.awaitErr = errors.New("argocd wait: sync timeout")
	close(f.delivery.release)

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForCompletion(t)

	if want := []string{"rel-target", "rel-current"}; !reflect.DeepEqual(f.delivery.deliveries(), want) {
		t.Errorf("delivered %v, want %v: the release kept active was not delivered again", f.delivery.deliveries(), want)
	}
	if active := f.activeReleases(); !reflect.DeepEqual(active, []string{"rel-current"}) {
		t.Errorf("active releases = %v, want [rel-current]", active)
	}
}

func TestRollback_CompletedDeliversNothingElse(t *testing.T) {
	f := newRollbackFixture(t)
	close(f.delivery.release)

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForCompletion(t)

	if want := []string{"rel-target"}; !reflect.DeepEqual(f.delivery.deliveries(), want) {
		t.Errorf("delivered %v, want %v", f.delivery.deliveries(), want)
	}
}

// A deploy that finished while the rollback waited owns the environment now.
// Its manifests are not overwritten with those of the release the rollback
// started from.
func TestRollback_AbandonedDoesNotRestoreOverADeployThatMovedTheEnvironmentOn(t *testing.T) {
	f := newRollbackFixture(t)
	f.delivery.awaitErr = errors.New("argocd wait: sync timeout")

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForTheSyncWaitToBegin(t)
	f.releases.mu.Lock()
	f.releases.releases["rel-current"].Status = string(statemachine.ReleaseSuperseded)
	f.releases.mu.Unlock()
	close(f.delivery.release)
	f.waitForCompletion(t)

	if want := []string{"rel-target"}; !reflect.DeepEqual(f.delivery.deliveries(), want) {
		t.Errorf("delivered %v, want %v: a newer deploy's manifests were overwritten", f.delivery.deliveries(), want)
	}
}

func TestRollback_AFailedRestoreIsLoggedWithBothReleases(t *testing.T) {
	f := newRollbackFixture(t)
	var logs bytes.Buffer
	f.orch.deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	f.delivery.awaitErr = errors.New("argocd wait: sync timeout")
	f.delivery.restoreErr = errors.New("gitea is down")
	close(f.delivery.release)

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForCompletion(t)

	var restore map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) == nil && strings.Contains(entry["msg"].(string), "could not be delivered again") {
			restore = entry
		}
	}
	if restore == nil {
		t.Fatalf("no log entry for the failed restore:\n%s", logs.String())
	}
	if restore["level"] != "ERROR" || restore["current_release"] != "rel-current" || restore["target_release"] != "rel-target" ||
		!strings.Contains(restore["error"].(string), "gitea is down") {
		t.Errorf("restore failure logged as %v, want level ERROR with both releases and the cause", restore)
	}
}
