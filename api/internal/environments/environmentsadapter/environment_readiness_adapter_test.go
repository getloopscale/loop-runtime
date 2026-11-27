// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environmentsadapter

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	"github.com/getloopscale/loop-runtime/api/internal/environments/environmentshttp"
)

func TestEnvReadinessReportsOnlyTheEnvironmentsThatWereObserved(t *testing.T) {
	two := int32(2)
	at := time.Date(2026, 11, 27, 14, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	rows := []generated.EnvironmentTarget{
		{ProjectID: "web", Environment: "production", LastReplicasReadyValue: &two, LastReplicasReadyAt: pgtype.Timestamptz{Time: at, Valid: true}},
		{ProjectID: "web", Environment: "preview"},
	}
	out := map[environmentshttp.EnvRef]environmentshttp.EnvReadiness{}

	addEnvReadiness(out, rows)

	got, ok := out[environmentshttp.EnvRef{Project: "web", Name: "production"}]
	if !ok || got.Replicas != 2 || got.At != "2026-11-27T17:00:00Z" {
		t.Errorf("web/production = %+v (present %v), want 2 replicas at 17:00:00Z", got, ok)
	}
	if _, ok := out[environmentshttp.EnvRef{Project: "web", Name: "preview"}]; ok {
		t.Error("web/preview is reported although the poller never observed it")
	}
}
