// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package postgresrestore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// fakePlatform is a cluster whose backups either hold what was written or do not.
type fakePlatform struct {
	source Source
	// rows of the marker table, on the source and in the recovered cluster.
	sourceMarkers   map[string]bool
	restoredMarkers map[string]bool
	// what the bucket holds: nothing recovers from an empty one.
	backupCompleted bool
	archiveStuck    bool
	recoveryPhase   string
	tablesSource    string
	tablesRestored  string

	applied []byte
	deleted []string
	exists  bool
}

func newFake() *fakePlatform {
	return &fakePlatform{
		source: Source{
			Name: "platform-postgres", Namespace: "loop-runtime-data-plane",
			Image:       "registry.example/postgresql:19.0",
			StorageSize: "5Gi", StorageClass: "standard",
			ObjectStore: "platform-postgres", ServerName: "platform-postgres",
			RoleARN: "arn:aws:iam::000000000000:role/test",
			Primary: "platform-postgres-1",
		},
		sourceMarkers: map[string]bool{}, restoredMarkers: map[string]bool{},
		backupCompleted: true, recoveryPhase: healthyPhase,
		tablesSource: "20", tablesRestored: "20",
	}
}

func (f *fakePlatform) Source(context.Context) (Source, error) { return f.source, nil }

func (f *fakePlatform) HasCompletedBackup(context.Context) (bool, error) {
	return f.backupCompleted, nil
}

func (f *fakePlatform) SQL(_ context.Context, pod, _ string, query string) (string, error) {
	restored := strings.HasPrefix(pod, DrillClusterName(f.source.Name))
	switch {
	case strings.Contains(query, "INSERT INTO"):
		run := query[strings.Index(query, "VALUES ('")+9:]
		f.sourceMarkers[run[:strings.Index(run, "'")]] = true
		return "", nil
	case strings.Contains(query, "pg_switch_wal"):
		// The recovered cluster holds what the source held when its WAL was archived.
		if f.backupCompleted {
			for run := range f.sourceMarkers {
				f.restoredMarkers[run] = true
			}
		}
		return "000000010000000000000007", nil
	case strings.Contains(query, "last_archived_wal"):
		if f.archiveStuck {
			return "000000010000000000000001", nil
		}
		return "000000010000000000000007", nil
	case strings.Contains(query, "FROM "+markerTable):
		markers := f.sourceMarkers
		if restored {
			markers = f.restoredMarkers
		}
		run := query[strings.Index(query, "run = '")+7:]
		if markers[run[:strings.Index(run, "'")]] {
			return "1", nil
		}
		return "0", nil
	case strings.Contains(query, "pg_tables"):
		if restored {
			return f.tablesRestored, nil
		}
		return f.tablesSource, nil
	case strings.Contains(query, "pg_database"):
		return "looptrace", nil
	}
	return "", nil
}

func (f *fakePlatform) Apply(_ context.Context, manifest []byte) error {
	f.applied, f.exists = manifest, true
	return nil
}

func (f *fakePlatform) Phase(context.Context, string) (string, error) { return f.recoveryPhase, nil }

func (f *fakePlatform) Delete(_ context.Context, name string) error {
	f.deleted, f.exists = append(f.deleted, name), false
	return nil
}

func fastOptions() Options {
	return Options{Timeout: 200 * time.Millisecond, Poll: time.Millisecond, RunID: "run-1"}
}

func TestDrillRecoversWhatWasWrittenAndRemovesItsCluster(t *testing.T) {
	f := newFake()

	report, err := Run(context.Background(), f, fastOptions())

	if err != nil {
		t.Fatalf("the drill failed: %v", err)
	}
	if !report.MarkerRecovered || report.Tables != "looptrace=20" {
		t.Errorf("report = %+v", report)
	}
	if f.applied == nil || f.exists {
		t.Errorf("the drill cluster was not created and removed: deleted %v", f.deleted)
	}
	for _, name := range f.deleted {
		if name != "platform-postgres-restore-drill" {
			t.Errorf("the drill deleted %s", name)
		}
	}
}

// The recovered cluster reads the source's backups and writes nowhere: a second archiver
// under the source's server name would corrupt the archive it is proving.
func TestDrillClusterReadsTheBackupsAndArchivesNothing(t *testing.T) {
	f := newFake()
	if _, err := Run(context.Background(), f, fastOptions()); err != nil {
		t.Fatal(err)
	}
	var cluster struct {
		Metadata struct{ Name, Namespace string }
		Spec     struct {
			Instances        int
			ImageName        string `yaml:"imageName"`
			Plugins          []any
			Bootstrap        struct{ Recovery struct{ Source string } }
			ExternalClusters []struct {
				Plugin struct {
					Name       string
					Parameters map[string]string
				}
			} `yaml:"externalClusters"`
			ServiceAccountTemplate struct {
				Metadata struct{ Annotations map[string]string }
			} `yaml:"serviceAccountTemplate"`
		}
	}
	if err := yaml.Unmarshal(f.applied, &cluster); err != nil {
		t.Fatalf("the applied manifest is not YAML: %v\n%s", err, f.applied)
	}
	if cluster.Metadata.Name != "platform-postgres-restore-drill" || cluster.Metadata.Namespace != "loop-runtime-data-plane" {
		t.Errorf("drill cluster is %s/%s", cluster.Metadata.Namespace, cluster.Metadata.Name)
	}
	if len(cluster.Spec.Plugins) != 0 {
		t.Errorf("the drill cluster archives through %v", cluster.Spec.Plugins)
	}
	if cluster.Spec.Instances != 1 || cluster.Spec.ImageName != f.source.Image {
		t.Errorf("instances %d, image %q", cluster.Spec.Instances, cluster.Spec.ImageName)
	}
	if len(cluster.Spec.ExternalClusters) != 1 {
		t.Fatalf("externalClusters = %v", cluster.Spec.ExternalClusters)
	}
	params := cluster.Spec.ExternalClusters[0].Plugin.Parameters
	if params["barmanObjectName"] != "platform-postgres" || params["serverName"] != "platform-postgres" {
		t.Errorf("the drill recovers from %v", params)
	}
	if got := cluster.Spec.ServiceAccountTemplate.Metadata.Annotations["eks.amazonaws.com/role-arn"]; got != f.source.RoleARN {
		t.Errorf("the drill cluster assumes %q", got)
	}
}

func TestDrillFailsWhenTheBucketHoldsNoBackup(t *testing.T) {
	f := newFake()
	f.backupCompleted = false

	_, err := Run(context.Background(), f, fastOptions())

	if !errors.Is(err, ErrNoBackup) {
		t.Errorf("err = %v, want ErrNoBackup", err)
	}
	if f.applied != nil {
		t.Error("a cluster was created with nothing to recover from")
	}
}

func TestDrillFailsWhenWALIsNotArchived(t *testing.T) {
	f := newFake()
	f.archiveStuck = true

	_, err := Run(context.Background(), f, fastOptions())

	if !errors.Is(err, ErrNotArchived) {
		t.Errorf("err = %v, want ErrNotArchived", err)
	}
}

func TestDrillFailsAndCleansUpWhenTheRecoveryNeverBecomesHealthy(t *testing.T) {
	f := newFake()
	f.recoveryPhase = "Setting up primary"

	_, err := Run(context.Background(), f, fastOptions())

	if !errors.Is(err, ErrRecoveryTimeout) {
		t.Errorf("err = %v, want ErrRecoveryTimeout", err)
	}
	if f.applied == nil || f.exists {
		t.Errorf("the stuck drill cluster was left behind: deleted %v", f.deleted)
	}
}

func TestDrillFailsWhenTheRecoveredDataIsNotWhatWasWritten(t *testing.T) {
	t.Run("marker missing", func(t *testing.T) {
		f := newFake()
		f.restoredMarkers = map[string]bool{}
		f.backupCompleted = true
		// The archive answers, but the recovered cluster stops short of the marker.
		drop := &dropMarkers{fakePlatform: f}
		_, err := Run(context.Background(), drop, fastOptions())
		if !errors.Is(err, ErrDataMismatch) {
			t.Errorf("err = %v, want ErrDataMismatch", err)
		}
	})
	t.Run("tables differ", func(t *testing.T) {
		f := newFake()
		f.tablesRestored = "19"
		_, err := Run(context.Background(), f, fastOptions())
		if !errors.Is(err, ErrDataMismatch) {
			t.Errorf("err = %v, want ErrDataMismatch", err)
		}
	})
}

type dropMarkers struct{ *fakePlatform }

func (d *dropMarkers) SQL(ctx context.Context, pod, db, query string) (string, error) {
	out, err := d.fakePlatform.SQL(ctx, pod, db, query)
	d.restoredMarkers = map[string]bool{}
	return out, err
}
