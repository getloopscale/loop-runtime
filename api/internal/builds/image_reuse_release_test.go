// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/release"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// recordingAttestationStore keeps the attestation rows the release activation
// writes.
type recordingAttestationStore struct {
	saved []store.AppAttestation
}

func (r *recordingAttestationStore) SaveAppAttestations(_ context.Context, _, _ string, atts []store.AppAttestation) error {
	r.saved = append(r.saved, atts...)
	return nil
}

func (r *recordingAttestationStore) SaveAppAttestationsTx(_ context.Context, _ db.DBTX, _, _ string, atts []store.AppAttestation) error {
	r.saved = append(r.saved, atts...)
	return nil
}

func (r *recordingAttestationStore) GetBuildAppAttestations(_ context.Context, _, _ string) ([]store.AppAttestationInfo, error) {
	return nil, nil
}

func (r *recordingAttestationStore) UpdateAppSBOM(_ context.Context, _, _, _, _ string, _ int) error {
	return nil
}

// TestReconcile_ReusedImageIsRecordedOnTheReleaseAndAttestation drives a build
// whose "api" image was reused through the reconcile loop and the real
// orchestrator and container delivery. The workflow ran no image step for the
// app, so its digest, signature and SBOM can only come from the stored reuse.
func TestReconcile_ReusedImageIsRecordedOnTheReleaseAndAttestation(t *testing.T) {
	signedAt := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	reused := db.ReusedImage{
		Digest:             "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Signature:          "registry.test/sig@sha256:cccc",
		SignedAt:           signedAt,
		SBOMS3Key:          "sboms/api.cdx.json",
		SBOMFormat:         "cyclonedx-json",
		SBOMComponentCount: 7,
	}
	buildStore := newFakeBuildStoreRL()
	b := defaultBuild("bld-reuse", string(statemachine.BuildRunning))
	buildStore.builds[b.ID] = b
	buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000, ReusedImage: &reused}}

	releases := newFakeReleaseStoreContainer()
	envTargets := newFakeEnvTargetStoreContainer()
	events := &fakeEventStoreContainer{}
	attestations := &recordingAttestationStore{}
	audit := &captureAuditLogger{}
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: buildStore, ReleaseStore: releases, EnvTargetStore: envTargets,
		EventStore: events, Workflow: &fakeWorkflowRL{}, Sync: &fakeSyncContainer{},
		Locker: &fakeLockerContainer{}, GitOps: &fakeGitOpsContainer{commitSHA: "gitops-sha"},
		AuditLogger: audit,
		Delivery: release.NewContainerDeliveryActivator(release.ContainerDeliveryDeps{
			ReleaseStore: releases, EnvTargetStore: envTargets, EventStore: events,
			Sync: &fakeSyncContainer{}, Locker: &fakeLockerContainer{},
			GitOps: &fakeGitOpsContainer{commitSHA: "gitops-sha"}, AppAttestationStore: attestations,
		}),
	})
	workflow := &fakeWorkflowRL{statusResult: &deploycontracts.WorkflowStatus{
		Phase: "Succeeded", FinishedAt: time.Now().UTC().Format(time.RFC3339),
		Outputs: map[string]map[string]string{},
	}}
	loop := NewBuildReconcileLoop(buildStore, workflow, orch, &fakeLockerContainer{},
		&fakeDomainQuerier{}, &fakeAppCRMgr{}, events,
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		BuildReconcileLoopConfig{RegistryURL: "registry.test", BaseDomain: "test.local", ClusterIssuer: "test-issuer"})

	loop.reconcileBuild(context.Background(), b)

	if len(releases.releases) != 1 {
		t.Fatalf("releases created = %d, want 1 (build status %q, error %q)",
			len(releases.releases), buildStore.builds[b.ID].Status, buildStore.builds[b.ID].ErrorMessage)
	}
	for _, rel := range releases.releases {
		if rel.ImageDigest == nil || *rel.ImageDigest != reused.Digest {
			t.Errorf("release image digest = %v, want %s", rel.ImageDigest, reused.Digest)
		}
		if rel.ImageSignature == nil || *rel.ImageSignature != reused.Signature {
			t.Errorf("release image signature = %v, want %s", rel.ImageSignature, reused.Signature)
		}
	}
	if len(attestations.saved) != 1 {
		t.Fatalf("attestation rows = %d, want 1", len(attestations.saved))
	}
	att := attestations.saved[0]
	if att.AppName != "api" || att.ImageDigest != reused.Digest {
		t.Errorf("attestation = %s %s, want api %s", att.AppName, att.ImageDigest, reused.Digest)
	}
	if att.ImageSignature == nil || *att.ImageSignature != reused.Signature {
		t.Errorf("attestation signature = %v, want %s", att.ImageSignature, reused.Signature)
	}
	if att.SignedAt == nil || !att.SignedAt.Equal(signedAt) {
		t.Errorf("attestation signed_at = %v, want the original signing time %v", att.SignedAt, signedAt)
	}
	if att.SBOMS3Key == nil || *att.SBOMS3Key != reused.SBOMS3Key ||
		att.SBOMFormat == nil || *att.SBOMFormat != reused.SBOMFormat ||
		att.SBOMComponentCount == nil || *att.SBOMComponentCount != reused.SBOMComponentCount {
		t.Errorf("attestation SBOM = %v %v %v, want %s %s %d", att.SBOMS3Key, att.SBOMFormat,
			att.SBOMComponentCount, reused.SBOMS3Key, reused.SBOMFormat, reused.SBOMComponentCount)
	}
	if len(audit.entries) != 0 {
		t.Errorf("image.signed audit entries = %d, want 0: this build signed nothing", len(audit.entries))
	}
}

// TestReconcileRetry_ReusedImageReachesTheActivationRetry covers the RELEASING
// retry path, which rebuilds the activation input from the stored apps spec.
func TestReconcileRetry_ReusedImageReachesTheActivationRetry(t *testing.T) {
	reused := db.ReusedImage{
		Digest:    "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Signature: "registry.test/sig@sha256:dddd",
		SignedAt:  time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC),
	}
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-reuse-retry", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000, ReusedImage: &reused}}
	fakes.workflow.statusResult = &deploycontracts.WorkflowStatus{Phase: "Succeeded", Outputs: map[string]map[string]string{}}

	fakes.buildLoop(BuildReconcileLoopConfig{}).reconcileBuild(context.Background(), b)

	if fakes.orch.retryCalls != 1 {
		t.Fatalf("RetryReleaseActivation calls = %d, want 1 (build status %q, error %q)",
			fakes.orch.retryCalls, fakes.buildStore.builds[b.ID].Status, fakes.buildStore.builds[b.ID].ErrorMessage)
	}
	apps := fakes.orch.retryResult.Apps
	if len(apps) != 1 || apps[0].ImageDigestSHA256 != reused.Digest || apps[0].ImageSignature != reused.Signature || !apps[0].ImageReused {
		t.Errorf("retry apps = %+v, want api reusing %s", apps, reused.Digest)
	}
}
