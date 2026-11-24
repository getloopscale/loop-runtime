// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import (
	"reflect"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// Every field a build persists must survive the trip back to an AppSpec. A
// stored app that loses Runtime, Path, Build or Start rebuilds with the wrong
// strategy (loopzero#185: empty runtime -> unsupportedStrategy -> SYNC_FAILED), so
// this test fills every field of db.BuildAppSpec by reflection and requires the
// same-named AppSpec field to come back non-zero. A field added to the stored
// shape and forgotten here fails this test instead of a preview build.
//
// derivedAtSubmit lists the stored fields that are not part of what the user
// submitted: SubmitBuild derives them, so a rebuilt AppSpec must not carry
// them. ReusedImage is looked up again on every submission; carrying a stored
// one back would skip that lookup with a possibly stale image.
func TestAppSpecFromStored_CarriesEveryPersistedField(t *testing.T) {
	derivedAtSubmit := map[string]bool{"ReusedImage": true}
	stored := db.BuildAppSpec{
		Name: "api", Type: "server", Rendering: "server", Port: 8080,
		Start: "node server.js", Path: "apps/api", Build: "dockerfile", Runtime: "node",
		Secrets: []string{"DB_URL"}, Replicas: 2,
		Autoscaling: &db.BuildAppAutoscaling{MinReplicas: 1, MaxReplicas: 3, TargetCPU: 70},
		ReusedImage: &db.ReusedImage{Digest: "sha256:01", Signature: "sig"},
	}
	sv := reflect.ValueOf(stored)
	for i := 0; i < sv.NumField(); i++ {
		if sv.Field(i).IsZero() {
			t.Fatalf("fixture leaves db.BuildAppSpec.%s zero; fill every field so none can be dropped unseen",
				sv.Type().Field(i).Name)
		}
	}

	got := reflect.ValueOf(AppSpecFromStored(stored))
	for i := 0; i < sv.NumField(); i++ {
		name := sv.Type().Field(i).Name
		if derivedAtSubmit[name] {
			continue
		}
		f := got.FieldByName(name)
		if !f.IsValid() {
			t.Errorf("AppSpec has no field %s; a persisted field has nowhere to go", name)
			continue
		}
		if f.IsZero() {
			t.Errorf("AppSpecFromStored dropped %s", name)
		}
	}
	if a := AppSpecFromStored(stored).Autoscaling; a.MinReplicas != 1 || a.MaxReplicas != 3 || a.TargetCPU != 70 {
		t.Errorf("autoscaling = %+v, want {1 3 70}", *a)
	}
}

func TestAppSpecFromStored_NoAutoscalingStaysNil(t *testing.T) {
	if got := AppSpecFromStored(db.BuildAppSpec{Name: "worker"}).Autoscaling; got != nil {
		t.Errorf("autoscaling = %+v, want nil", got)
	}
}
