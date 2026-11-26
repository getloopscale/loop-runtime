// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/builds"
	"github.com/getloopscale/loop-runtime/api/internal/builds/argo"
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/release"
	"github.com/getloopscale/loop-runtime/pkg/naming"

	forge "github.com/getloopscale/argo-workflows-go"
)

// fakeSignedImageFinder answers with the signed images it holds per commit and
// records every lookup.
type fakeSignedImageFinder struct {
	byCommit map[string]map[string]db.ReusedImage
	err      error
	lookups  []string // "tenant/project/commit"
}

func (f *fakeSignedImageFinder) FindSignedImages(_ context.Context, _ db.DBTX, tenantID, projectID, commitSHA string) (map[string]db.ReusedImage, error) {
	f.lookups = append(f.lookups, tenantID+"/"+projectID+"/"+commitSHA)
	if f.err != nil {
		return nil, f.err
	}
	return f.byCommit[commitSHA], nil
}

func signedImage(digest string) db.ReusedImage {
	return db.ReusedImage{
		Digest:             digest,
		Signature:          "registry.test/sig@" + digest,
		SignedAt:           time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC),
		SBOMS3Key:          "sboms/api.cdx.json",
		SBOMFormat:         "cyclonedx-json",
		SBOMComponentCount: 42,
	}
}

const (
	digestAPI = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestWeb = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func orchestratorWithFinder(f *orchestratorFakes, finder builds.SignedImageFinder) *builds.BuildOrchestrator {
	return builds.NewBuildOrchestrator(builds.BuildOrchestratorDeps{
		BuildStore:     f.buildStore,
		ReleaseStore:   f.releaseStore,
		EnvTargetStore: f.envTargetStore,
		EventStore:     f.eventStore,
		Workflow:       f.workflow,
		Sync:           f.sync,
		Locker:         f.locker,
		GitOps:         f.gitops,
		Delivery: release.NewContainerDeliveryActivator(release.ContainerDeliveryDeps{
			ReleaseStore: f.releaseStore, EnvTargetStore: f.envTargetStore, EventStore: f.eventStore,
			Sync: f.sync, Locker: f.locker, GitOps: f.gitops,
		}),
		SignedImages: finder,
	})
}

func twoAppSubmitReq(commit string) deploycontracts.SubmitBuildRequest {
	req := defaultSubmitReq()
	req.CommitSHA = commit
	req.Apps = []deploycontracts.AppSpec{
		{Name: "api", Type: "server", Port: 3000, Path: "apps/api", Runtime: "node"},
		{Name: "web", Type: "server", Port: 3001, Path: "apps/web", Runtime: "node"},
	}
	return req
}

// submitAndBuildWorkflow submits req and renders the submitted spec with the
// production workflow builder.
func submitAndBuildWorkflow(t *testing.T, f *orchestratorFakes, orch *builds.BuildOrchestrator, req deploycontracts.SubmitBuildRequest) (string, deploycontracts.WorkflowSpec, *forge.DAG) {
	t.Helper()
	resp, err := orch.SubmitBuild(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitBuild: %v", err)
	}
	if len(f.workflow.submitted) != 1 {
		t.Fatalf("submitted workflows = %d, want 1", len(f.workflow.submitted))
	}
	spec := f.workflow.submitted[0]
	wf, err := argo.NewWorkflowBuilder(argo.InfraParams{RegistryURL: "registry.test"}).Build(spec)
	if err != nil {
		t.Fatalf("build workflow: %v", err)
	}
	for _, tmpl := range wf.Templates {
		if dag, ok := tmpl.(*forge.DAG); ok && dag.Name == "build-pipeline" {
			return resp.BuildID, spec, dag
		}
	}
	t.Fatal("main DAG not found")
	return "", spec, nil
}

func dagHasTask(dag *forge.DAG, name string) bool {
	for _, task := range dag.Tasks {
		if task.Name == name {
			return true
		}
	}
	return false
}

func planFor(t *testing.T, spec deploycontracts.WorkflowSpec, app string) deploycontracts.AppBuildPlan {
	t.Helper()
	for _, p := range spec.Plan.Apps {
		if p.AppName == app {
			return p
		}
	}
	t.Fatalf("no plan for app %q", app)
	return deploycontracts.AppBuildPlan{}
}

func storedSpecFor(t *testing.T, f *orchestratorFakes, buildID, app string) db.BuildAppSpec {
	t.Helper()
	for _, s := range f.buildStore.appsSpec[buildID] {
		if s.Name == app {
			return s
		}
	}
	t.Fatalf("no stored spec for app %q", app)
	return db.BuildAppSpec{}
}

func assertImageStepsAbsent(t *testing.T, dag *forge.DAG, app string) {
	t.Helper()
	for _, name := range []string{naming.BuildImageStepName(app), naming.SignStepName(app), naming.SBOMStepName(app)} {
		if dagHasTask(dag, name) {
			t.Errorf("task %q emitted for app %q, whose signed image already exists", name, app)
		}
	}
}

func assertImageStepsPresent(t *testing.T, dag *forge.DAG, app string) {
	t.Helper()
	for _, name := range []string{naming.BuildImageStepName(app), naming.SignStepName(app), naming.SBOMStepName(app)} {
		if !dagHasTask(dag, name) {
			t.Errorf("task %q missing for app %q, which has no image yet", name, app)
		}
	}
}

func TestSubmitBuild_CommitWithSignedImageSkipsImageStepsAndCarriesDigest(t *testing.T) {
	f := newOrchestratorFakes()
	finder := &fakeSignedImageFinder{byCommit: map[string]map[string]db.ReusedImage{
		"c1": {"api": signedImage(digestAPI)},
	}}
	req := defaultSubmitReq()
	req.CommitSHA = "c1"

	buildID, spec, dag := submitAndBuildWorkflow(t, f, orchestratorWithFinder(f, finder), req)

	assertImageStepsAbsent(t, dag, "api")
	if got := planFor(t, spec, "api").ReusedDigest; got != digestAPI {
		t.Errorf("plan ReusedDigest = %q, want %q", got, digestAPI)
	}
	stored := storedSpecFor(t, f, buildID, "api")
	if stored.ReusedImage == nil || *stored.ReusedImage != signedImage(digestAPI) {
		t.Errorf("stored ReusedImage = %+v, want %+v", stored.ReusedImage, signedImage(digestAPI))
	}
}

func TestSubmitBuild_NewCommitStillBuildsItsImage(t *testing.T) {
	f := newOrchestratorFakes()
	finder := &fakeSignedImageFinder{byCommit: map[string]map[string]db.ReusedImage{
		"old": {"api": signedImage(digestAPI)},
	}}
	req := defaultSubmitReq()
	req.CommitSHA = "new"

	buildID, spec, dag := submitAndBuildWorkflow(t, f, orchestratorWithFinder(f, finder), req)

	want := req.TenantID + "/" + req.ProjectID + "/new"
	if len(finder.lookups) != 1 || finder.lookups[0] != want {
		t.Errorf("signed image lookups = %v, want [%s]", finder.lookups, want)
	}
	assertImageStepsPresent(t, dag, "api")
	if got := planFor(t, spec, "api").ReusedDigest; got != "" {
		t.Errorf("plan ReusedDigest = %q, want empty for a new commit", got)
	}
	if stored := storedSpecFor(t, f, buildID, "api"); stored.ReusedImage != nil {
		t.Errorf("stored ReusedImage = %+v, want nil for a new commit", stored.ReusedImage)
	}
}

func TestSubmitBuild_MixedBuildReusesOneAppAndBuildsTheOther(t *testing.T) {
	f := newOrchestratorFakes()
	finder := &fakeSignedImageFinder{byCommit: map[string]map[string]db.ReusedImage{
		"c1": {"api": signedImage(digestAPI)},
	}}

	buildID, spec, dag := submitAndBuildWorkflow(t, f, orchestratorWithFinder(f, finder), twoAppSubmitReq("c1"))

	assertImageStepsAbsent(t, dag, "api")
	assertImageStepsPresent(t, dag, "web")
	if got := planFor(t, spec, "api").ReusedDigest; got != digestAPI {
		t.Errorf("api ReusedDigest = %q, want %q", got, digestAPI)
	}
	if got := planFor(t, spec, "web").ReusedDigest; got != "" {
		t.Errorf("web ReusedDigest = %q, want empty", got)
	}
	if stored := storedSpecFor(t, f, buildID, "api"); stored.ReusedImage == nil || stored.ReusedImage.Digest != digestAPI {
		t.Errorf("api stored ReusedImage = %+v, want digest %s", stored.ReusedImage, digestAPI)
	}
	if stored := storedSpecFor(t, f, buildID, "web"); stored.ReusedImage != nil {
		t.Errorf("web stored ReusedImage = %+v, want nil", stored.ReusedImage)
	}
}

func TestSubmitBuild_SignedImageLookupFailureFailsTheSubmit(t *testing.T) {
	f := newOrchestratorFakes()
	lookupErr := errors.New("connection reset")
	finder := &fakeSignedImageFinder{err: lookupErr}

	_, err := orchestratorWithFinder(f, finder).SubmitBuild(context.Background(), defaultSubmitReq())

	if !errors.Is(err, lookupErr) {
		t.Fatalf("SubmitBuild error = %v, want it to wrap %v", err, lookupErr)
	}
	sbe, ok := builds.AsSubmitBuildError(err)
	if !ok || sbe.Stage != builds.StageFindSignedImages {
		t.Errorf("SubmitBuild error = %#v, want a SubmitBuildError at stage %q", err, builds.StageFindSignedImages)
	}
	if len(f.workflow.submitted) != 0 {
		t.Errorf("submitted workflows = %d, want 0 after a failed lookup", len(f.workflow.submitted))
	}
}
