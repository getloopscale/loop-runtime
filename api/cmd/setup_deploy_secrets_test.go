// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/k8s/secretsync"
)

// The delivery waits for the environment's secrets only when it is given the
// syncer that writes them (#119). Left out of the composition, every deploy
// would again race the secret store without a test noticing.
func TestDeployWaitsForSecretsThroughTheSecretSyncer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	syncer := &secretsync.SecretK8sManager{}

	got := deployEnvironmentSecrets(&K8sDeps{SecretK8sSync: syncer}, logger)

	if got == nil {
		t.Fatal("no secrets confirmation for the delivery although the secret syncer is wired")
	}
	if got != any(syncer) {
		t.Errorf("the delivery waits on %T, want the syncer that writes the ExternalSecrets", got)
	}
}

// Without the syncer there is nothing to wait on. The result is a nil
// interface, not a typed nil the delivery would call.
func TestDeployDoesNotWaitForSecretsWithoutTheSecretSyncer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, deps := range map[string]*K8sDeps{"no cluster clients": nil, "no secret syncer": {}} {
		if got := deployEnvironmentSecrets(deps, logger); got != nil {
			t.Errorf("%s: got %T, want nil", name, got)
		}
	}
}
