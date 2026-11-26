// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scaling

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

type fakeActiveReleaseReader struct {
	release *db.Release
	err     error
}

func (f fakeActiveReleaseReader) GetActiveByEnv(_ context.Context, _, _, _ string) (*db.Release, error) {
	return f.release, f.err
}

func TestDeployedAppNamesAreTheAppsOfTheActiveRelease(t *testing.T) {
	apps, _ := json.Marshal([]db.BuildAppSpec{{Name: "web"}, {Name: "worker"}})
	l := ReleaseAppLookup{Releases: fakeActiveReleaseReader{release: &db.Release{Apps: apps}}}

	got, err := l.DeployedAppNames(context.Background(), "tn-1", "hello", "production")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"web", "worker"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DeployedAppNames = %v, want %v", got, want)
	}
}

func TestDeployedAppNamesAreEmptyWithoutAnActiveRelease(t *testing.T) {
	l := ReleaseAppLookup{Releases: fakeActiveReleaseReader{}}

	got, err := l.DeployedAppNames(context.Background(), "tn-1", "hello", "production")
	if err != nil || got != nil {
		t.Errorf("DeployedAppNames = %v, %v; want nil, nil", got, err)
	}
}

func TestDeployedAppNamesFailWhenTheReleaseCannotBeRead(t *testing.T) {
	l := ReleaseAppLookup{Releases: fakeActiveReleaseReader{err: errors.New("database unavailable")}}

	if _, err := l.DeployedAppNames(context.Background(), "tn-1", "hello", "production"); err == nil {
		t.Error("want an error when the release cannot be read")
	}
}

func TestDeployedAppNamesFailWhenTheReleaseAppsDoNotParse(t *testing.T) {
	l := ReleaseAppLookup{Releases: fakeActiveReleaseReader{release: &db.Release{ID: "rel-1", Apps: json.RawMessage(`{`)}}}

	if _, err := l.DeployedAppNames(context.Background(), "tn-1", "hello", "production"); err == nil {
		t.Error("want an error when the release's apps do not parse")
	}
}
