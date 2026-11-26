// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scaling

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/manifests"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

type fakeReleases struct {
	active *db.Release
	latest []db.Release
}

func (f fakeReleases) GetActiveByEnv(context.Context, string, string, string) (*db.Release, error) {
	return f.active, nil
}

func (f fakeReleases) ListByEnv(context.Context, string, string, string, int) ([]db.Release, error) {
	return f.latest, nil
}

type fakeBuilds struct {
	build *db.Build
	apps  []db.BuildAppSpec
}

func (f fakeBuilds) GetByID(context.Context, string) (*db.Build, error) { return f.build, nil }

func (f fakeBuilds) GetBuildApps(context.Context, string) ([]db.BuildAppSpec, error) {
	return f.apps, nil
}

type inlineLocker struct{}

func (inlineLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

type recordingGitOps struct {
	pushes []deploycontracts.GitOpsPushOpts
	err    error
}

func (g *recordingGitOps) Push(_ context.Context, opts deploycontracts.GitOpsPushOpts) (string, error) {
	if g.err != nil {
		return "", g.err
	}
	g.pushes = append(g.pushes, opts)
	return "c0ffee", nil
}

func (g *recordingGitOps) Remove(context.Context, deploycontracts.GitOpsRemoveOpts) (string, error) {
	return "", nil
}

type recordingSync struct {
	apps []string
	err  error
}

func (s *recordingSync) TriggerSync(_ context.Context, appName string) error {
	if s.err != nil {
		return s.err
	}
	s.apps = append(s.apps, appName)
	return nil
}

// runningShop is project "shop" of tenant "tn-1" running build-1 with two
// replicas of "api" and one "worker", and a stored config of row for "api".
func runningShop(row *store.ScalingConfig) (*ReleaseScaler, *recordingGitOps, *recordingSync) {
	apps, _ := json.Marshal([]deploycontracts.AppRelease{
		{Name: "api", ImageDigestSHA256: "sha256:" + strings.Repeat("a", 64)},
		{Name: "worker", ImageDigestSHA256: "sha256:" + strings.Repeat("b", 64)},
	})
	active := &db.Release{ID: "rel-1", BuildID: "build-1", TenantID: "tn-1", ProjectID: "shop", Environment: "production", Status: "ACTIVE", Apps: apps}
	gitops, sync := &recordingGitOps{}, &recordingSync{}
	scaler := &ReleaseScaler{
		Releases: fakeReleases{active: active, latest: []db.Release{*active}},
		Builds: fakeBuilds{
			build: &db.Build{ID: "build-1", TenantID: "tn-1", ProjectID: "shop", Environment: "production", CommitSHA: "abc123"},
			apps: []db.BuildAppSpec{
				{Name: "api", Type: "server", Port: 8080, Replicas: 2},
				{Name: "worker", Type: "worker", Replicas: 1},
			},
		},
		Configs: &fakeScalingStore{get: func(_ context.Context, _, _, _, app string) (*store.ScalingConfig, error) {
			if row == nil || app != "api" {
				return nil, store.ErrScalingConfigNotFound
			}
			return row, nil
		}},
		Locker: inlineLocker{},
		GitOps: gitops,
		Sync:   sync,
		Render: RenderSettings{
			RegistryURL: "registry.example.test", BaseDomain: "apps.example.test",
			ClusterIssuer: "letsencrypt", ProgressiveDelivery: true,
		},
	}
	return scaler, gitops, sync
}

// workload is the part of a rendered Rollout these tests read.
type workload struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Replicas int `json:"replicas"`
		Template struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"template"`
	} `json:"spec"`
}

func rolloutIn(t *testing.T, rendered, name string) workload {
	t.Helper()
	for _, doc := range strings.Split(rendered, "\n---\n") {
		var w workload
		if err := yaml.Unmarshal([]byte(doc), &w); err != nil {
			t.Fatalf("parse manifest: %v", err)
		}
		if w.Kind == "Rollout" && w.Metadata.Name == name {
			return w
		}
	}
	t.Fatalf("no Rollout %q in the pushed manifests", name)
	return workload{}
}

// The scale endpoint stored the config and nothing carried it to the cluster:
// only the next deploy read it. The stored config now reaches the running
// Rollout the way a deploy does, through the GitOps repository and a sync.
func TestScalerDeliversTheStoredReplicasOfTheRunningRelease(t *testing.T) {
	// Arrange
	scaler, gitops, sync := runningShop(&store.ScalingConfig{MinReplicas: 1, MaxReplicas: 1})

	// Act
	delivered, err := scaler.Deliver(context.Background(), "tn-1", "shop", "production")

	// Assert
	if err != nil || !delivered {
		t.Fatalf("Deliver = %v, %v; want true, nil", delivered, err)
	}
	if len(gitops.pushes) != 1 {
		t.Fatalf("manifests pushed %d times, want 1", len(gitops.pushes))
	}
	push := gitops.pushes[0]
	if got := rolloutIn(t, push.Manifests, "shop-api").Spec.Replicas; got != 1 {
		t.Errorf("Rollout shop-api replicas = %d, want the stored 1", got)
	}
	if got := rolloutIn(t, push.Manifests, "shop-worker").Spec.Replicas; got != 1 {
		t.Errorf("Rollout shop-worker replicas = %d, want the 1 it was deployed with", got)
	}
	if push.BuildID != "build-1" || push.ReleaseID != "rel-1" {
		t.Errorf("pushed as build %q release %q, want the running build-1 and rel-1", push.BuildID, push.ReleaseID)
	}
	if len(sync.apps) != 1 || sync.apps[0] != "tn-1-shop-production" {
		t.Errorf("synced %v, want [tn-1-shop-production]: the push alone changes no workload", sync.apps)
	}
}

// Scaling must change the replica count and nothing else: the pod template of
// the running build is rendered as the deploy rendered it, so no pod restarts.
func TestScalerKeepsThePodTemplateOfTheRunningBuild(t *testing.T) {
	// Arrange
	scaler, gitops, _ := runningShop(&store.ScalingConfig{MinReplicas: 3, MaxReplicas: 3})
	deployed, err := manifests.Render(manifests.RenderInput{
		TenantID: "tn-1", ProjectID: "shop", Environment: "production", BuildID: "build-1",
		BaseDomain: "apps.example.test", ClusterIssuerName: "letsencrypt", ProgressiveDelivery: true,
		Apps: []manifests.AppManifestInput{{
			Name: "api", Type: "server", Port: 8080, Replicas: 2,
			ImageRef: naming.ImageDigestRef("registry.example.test", "tn-1", "shop", "api", "sha256:"+strings.Repeat("a", 64)),
		}},
	})
	if err != nil {
		t.Fatalf("render the deploy's manifests: %v", err)
	}

	// Act
	if _, err := scaler.Deliver(context.Background(), "tn-1", "shop", "production"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	// Assert
	if len(gitops.pushes) != 1 {
		t.Fatalf("manifests pushed %d times, want 1", len(gitops.pushes))
	}
	want := templateOf(t, deployed, "shop-api")
	got := templateOf(t, gitops.pushes[0].Manifests, "shop-api")
	if got != want {
		t.Errorf("the scaled Rollout's pod template differs from the deployed one: the app would restart\ndeployed:\n%s\nscaled:\n%s", want, got)
	}
}

func templateOf(t *testing.T, rendered, name string) string {
	t.Helper()
	for _, doc := range strings.Split(rendered, "\n---\n") {
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("parse manifest: %v", err)
		}
		meta, _ := obj["metadata"].(map[string]interface{})
		if obj["kind"] != "Rollout" || meta["name"] != name {
			continue
		}
		spec, _ := obj["spec"].(map[string]interface{})
		out, err := yaml.Marshal(spec["template"])
		if err != nil {
			t.Fatalf("marshal pod template: %v", err)
		}
		return string(out)
	}
	t.Fatalf("no Rollout %q", name)
	return ""
}

func TestScalerDeliversNothingWithoutARunningRelease(t *testing.T) {
	// Arrange
	scaler, gitops, sync := runningShop(&store.ScalingConfig{MinReplicas: 1, MaxReplicas: 1})
	scaler.Releases = fakeReleases{}

	// Act
	delivered, err := scaler.Deliver(context.Background(), "tn-1", "shop", "production")

	// Assert
	if err != nil || delivered {
		t.Fatalf("Deliver = %v, %v; want false, nil: the first deploy reads the stored config", delivered, err)
	}
	if len(gitops.pushes)+len(sync.apps) != 0 {
		t.Errorf("pushed %d and synced %d times with nothing running", len(gitops.pushes), len(sync.apps))
	}
}

// While a deploy is being verified its manifests are already in the GitOps
// repository and the running release is still the previous one. Rendering that
// one over them would undo the deploy.
func TestScalerRefusesWhileADeployIsInFlight(t *testing.T) {
	for _, status := range []string{"READY", "PENDING", "ROLLING_BACK"} {
		t.Run(status, func(t *testing.T) {
			// Arrange
			scaler, gitops, _ := runningShop(&store.ScalingConfig{MinReplicas: 1, MaxReplicas: 1})
			releases := scaler.Releases.(fakeReleases)
			releases.latest = []db.Release{{ID: "rel-2", BuildID: "build-2", Status: status}, *releases.active}
			scaler.Releases = releases

			// Act
			delivered, err := scaler.Deliver(context.Background(), "tn-1", "shop", "production")

			// Assert
			if !errors.Is(err, ErrDeployInFlight) || delivered {
				t.Fatalf("Deliver = %v, %v; want false, ErrDeployInFlight", delivered, err)
			}
			if len(gitops.pushes) != 0 {
				t.Error("the running release's manifests were pushed over the deploy in flight")
			}
		})
	}
}

func TestScalerFailsWhenTheManifestsCannotBePushed(t *testing.T) {
	// Arrange
	scaler, gitops, sync := runningShop(&store.ScalingConfig{MinReplicas: 1, MaxReplicas: 1})
	gitops.err = errors.New("gitea unreachable")

	// Act
	delivered, err := scaler.Deliver(context.Background(), "tn-1", "shop", "production")

	// Assert
	if err == nil || delivered {
		t.Fatalf("Deliver = %v, %v; want false and the push error", delivered, err)
	}
	if len(sync.apps) != 0 {
		t.Error("a sync was triggered for manifests that were not pushed")
	}
}

func TestScalerFailsWhenTheSyncIsRefused(t *testing.T) {
	// Arrange
	scaler, _, sync := runningShop(&store.ScalingConfig{MinReplicas: 1, MaxReplicas: 1})
	sync.err = errors.New("argocd unreachable")

	// Act
	delivered, err := scaler.Deliver(context.Background(), "tn-1", "shop", "production")

	// Assert
	if err == nil || delivered {
		t.Fatalf("Deliver = %v, %v; want false and the sync error: ArgoCD does not sync on its own", delivered, err)
	}
}

// A rollback pushes the manifests of an older release from the GitOps history,
// with the replica count of its day. DeliverSaved puts the saved config back,
// and leaves a rollback of an environment that was never scaled as it is.
func TestScalerDeliverSavedDeliversOnlyWhenAConfigIsSaved(t *testing.T) {
	t.Run("a saved config is delivered", func(t *testing.T) {
		scaler, gitops, sync := runningShop(&store.ScalingConfig{MinReplicas: 3, MaxReplicas: 3})

		delivered, err := scaler.DeliverSaved(context.Background(), "tn-1", "shop", "production")

		if err != nil || !delivered {
			t.Fatalf("DeliverSaved = %v, %v; want true, nil", delivered, err)
		}
		if len(gitops.pushes) != 1 || len(sync.apps) != 1 {
			t.Fatalf("pushes = %d, syncs = %d; want 1 and 1", len(gitops.pushes), len(sync.apps))
		}
		if got := rolloutIn(t, gitops.pushes[0].Manifests, "shop-api").Spec.Replicas; got != 3 {
			t.Errorf("Rollout shop-api replicas = %d, want the saved 3", got)
		}
	})

	t.Run("without a saved config nothing is pushed", func(t *testing.T) {
		scaler, gitops, sync := runningShop(nil)

		delivered, err := scaler.DeliverSaved(context.Background(), "tn-1", "shop", "production")

		if err != nil || delivered {
			t.Fatalf("DeliverSaved = %v, %v; want false, nil", delivered, err)
		}
		if len(gitops.pushes) != 0 || len(sync.apps) != 0 {
			t.Errorf("pushes = %d, syncs = %d; want none: the rolled back manifests already hold", len(gitops.pushes), len(sync.apps))
		}
	})
}
