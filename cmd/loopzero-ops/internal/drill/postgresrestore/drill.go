// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package postgresrestore proves that the platform's PostgreSQL can be recovered from
// its backups. It writes a marker row to the running cluster, waits for the WAL that
// carries it to reach the bucket, recovers a second, temporary cluster from the bucket
// and checks that the marker and the tables came back. The running cluster is only
// written to (one row); the temporary cluster archives nothing and is deleted at the end.
package postgresrestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	healthyPhase = "Cluster in healthy state"
	markerTable  = "loopzero_restore_drill"
	markerDB     = "postgres"
	pluginName   = "barman-cloud.cloudnative-pg.io"
	// A WAL segment file name: timeline, log and segment, 8 hex digits each.
	walNameLength = 24
)

var (
	// ErrNoBackup: the cluster has no completed base backup to recover from.
	ErrNoBackup = errors.New("no completed backup")
	// ErrNotArchived: the WAL segment holding the marker did not reach the archive.
	ErrNotArchived = errors.New("WAL not archived")
	// ErrRecoveryTimeout: the recovered cluster did not become healthy in time.
	ErrRecoveryTimeout = errors.New("recovery timed out")
	// ErrDataMismatch: the recovered cluster does not hold what the source held.
	ErrDataMismatch = errors.New("recovered data differs")
)

// Source is what the drill reads from the running cluster to build the temporary one.
type Source struct {
	Name, Namespace string
	Image           string
	StorageSize     string
	StorageClass    string
	// ObjectStore and ServerName locate the backups: the ObjectStore resource the
	// cluster archives through, and the server name it archives under.
	ObjectStore, ServerName string
	// RoleARN is the IAM role the instances assume to reach the bucket.
	RoleARN string
	Primary string
}

// Platform is the cluster the drill runs against.
type Platform interface {
	Source(ctx context.Context) (Source, error)
	HasCompletedBackup(ctx context.Context) (bool, error)
	// SQL runs one statement as the superuser in a pod and returns the trimmed output.
	SQL(ctx context.Context, pod, database, query string) (string, error)
	Apply(ctx context.Context, manifest []byte) error
	// Phase is the status phase of a Cluster, empty while it has none.
	Phase(ctx context.Context, cluster string) (string, error)
	// Delete removes a Cluster and waits until it is gone. Deleting one that does not
	// exist succeeds.
	Delete(ctx context.Context, cluster string) error
}

// Options bound the drill.
type Options struct {
	// Timeout bounds each wait: for the WAL to be archived, and for the recovery.
	Timeout time.Duration
	Poll    time.Duration
	// RunID identifies the marker row. It is spliced into SQL, so the caller derives it
	// from the clock, never from input.
	RunID string
}

// Report is what one drill observed.
type Report struct {
	Cluster         string        `json:"cluster"`
	RunID           string        `json:"run_id"`
	Segment         string        `json:"wal_segment"`
	ArchiveWait     time.Duration `json:"archive_wait_ns"`
	RecoveryTime    time.Duration `json:"recovery_time_ns"`
	MarkerRecovered bool          `json:"marker_recovered"`
	// Tables is the number of user tables per database, the same on both clusters.
	Tables string `json:"tables"`
}

// DrillClusterName is the temporary cluster's name for a source cluster.
func DrillClusterName(source string) string { return source + "-restore-drill" }

// Run executes the drill. The temporary cluster is removed on every path that created it.
func Run(ctx context.Context, p Platform, opts Options) (Report, error) {
	src, err := p.Source(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("read the source cluster: %w", err)
	}
	report := Report{Cluster: src.Name, RunID: opts.RunID}

	// A cluster that was just created is still taking its first backup.
	err = poll(ctx, opts, func() (bool, error) { return p.HasCompletedBackup(ctx) })
	if errors.Is(err, errPollTimeout) {
		return report, fmt.Errorf("%w for cluster %s after %s", ErrNoBackup, src.Name, opts.Timeout)
	}
	if err != nil {
		return report, fmt.Errorf("list the backups of %s: %w", src.Name, err)
	}

	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS " + markerTable + " (run text PRIMARY KEY, at timestamptz NOT NULL DEFAULT now())",
		"DELETE FROM " + markerTable + " WHERE at < now() - interval '30 days'",
		"INSERT INTO " + markerTable + " (run) VALUES ('" + opts.RunID + "')",
	} {
		if _, err := p.SQL(ctx, src.Primary, markerDB, q); err != nil {
			return report, fmt.Errorf("write the marker on %s: %w", src.Primary, err)
		}
	}
	tables, err := tableCounts(ctx, p, src.Primary)
	if err != nil {
		return report, err
	}

	report.Segment, err = p.SQL(ctx, src.Primary, markerDB, "SELECT pg_walfile_name(pg_switch_wal())")
	if err != nil {
		return report, fmt.Errorf("switch WAL on %s: %w", src.Primary, err)
	}
	start := time.Now()
	if err := waitArchived(ctx, p, src.Primary, report.Segment, opts); err != nil {
		return report, err
	}
	report.ArchiveWait = time.Since(start)

	drill := DrillClusterName(src.Name)
	manifest, err := drillManifest(src)
	if err != nil {
		return report, err
	}
	// A cluster left by an interrupted run would be adopted by the apply instead of
	// recovered again.
	if err := p.Delete(ctx, drill); err != nil {
		return report, fmt.Errorf("remove a leftover %s: %w", drill, err)
	}
	start = time.Now()
	if err := p.Apply(ctx, manifest); err != nil {
		return report, fmt.Errorf("create %s: %w", drill, err)
	}
	checkErr := verify(ctx, p, src, drill, tables, opts, &report, start)
	// The caller's context may be what ended the drill; the cleanup still has to run.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	if err := p.Delete(cleanupCtx, drill); err != nil {
		return report, errors.Join(checkErr, fmt.Errorf("remove %s: %w", drill, err))
	}
	return report, checkErr
}

func verify(ctx context.Context, p Platform, src Source, drill, tables string, opts Options, report *Report, start time.Time) error {
	if err := waitHealthy(ctx, p, drill, opts); err != nil {
		return err
	}
	report.RecoveryTime = time.Since(start)

	pod := drill + "-1"
	found, err := p.SQL(ctx, pod, markerDB, "SELECT count(*) FROM "+markerTable+" WHERE run = '"+opts.RunID+"'")
	if err != nil {
		return fmt.Errorf("read the marker on %s: %w", pod, err)
	}
	report.MarkerRecovered = found == "1"
	if !report.MarkerRecovered {
		return fmt.Errorf("%w: marker %s is not in the cluster recovered from %s", ErrDataMismatch, opts.RunID, src.ServerName)
	}
	restored, err := tableCounts(ctx, p, pod)
	if err != nil {
		return err
	}
	report.Tables = restored
	if restored != tables {
		return fmt.Errorf("%w: tables per database are %q on %s and %q recovered", ErrDataMismatch, tables, src.Name, restored)
	}
	return nil
}

// tableCounts is "db=n" for every user database, in name order.
func tableCounts(ctx context.Context, p Platform, pod string) (string, error) {
	dbs, err := p.SQL(ctx, pod, markerDB,
		"SELECT datname FROM pg_database WHERE NOT datistemplate AND datname <> 'postgres' ORDER BY datname")
	if err != nil {
		return "", fmt.Errorf("list the databases on %s: %w", pod, err)
	}
	var counts []string
	for _, db := range strings.Fields(dbs) {
		n, err := p.SQL(ctx, pod, db,
			"SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema')")
		if err != nil {
			return "", fmt.Errorf("count the tables of %s on %s: %w", db, pod, err)
		}
		counts = append(counts, db+"="+n)
	}
	return strings.Join(counts, " "), nil
}

func waitArchived(ctx context.Context, p Platform, pod, segment string, opts Options) error {
	last := ""
	err := poll(ctx, opts, func() (bool, error) {
		out, err := p.SQL(ctx, pod, markerDB, "SELECT last_archived_wal FROM pg_stat_archiver")
		if err != nil {
			return false, fmt.Errorf("read the archiver status on %s: %w", pod, err)
		}
		// Backup labels and timeline history files are archived too; only a segment
		// name compares with a segment name.
		if len(out) >= walNameLength {
			last = out[:walNameLength]
		}
		return last >= segment, nil
	})
	if errors.Is(err, errPollTimeout) {
		return fmt.Errorf("%w: segment %s after %s, last archived %q", ErrNotArchived, segment, opts.Timeout, last)
	}
	return err
}

func waitHealthy(ctx context.Context, p Platform, cluster string, opts Options) error {
	phase := ""
	err := poll(ctx, opts, func() (bool, error) {
		var err error
		phase, err = p.Phase(ctx, cluster)
		if err != nil {
			return false, fmt.Errorf("read the phase of %s: %w", cluster, err)
		}
		return phase == healthyPhase, nil
	})
	if errors.Is(err, errPollTimeout) {
		return fmt.Errorf("%w: %s is in phase %q after %s", ErrRecoveryTimeout, cluster, phase, opts.Timeout)
	}
	return err
}

var errPollTimeout = errors.New("poll timed out")

func poll(ctx context.Context, opts Options, done func() (bool, error)) error {
	deadline := time.Now().Add(opts.Timeout)
	for {
		ok, err := done()
		if err != nil || ok {
			return err
		}
		if time.Now().After(deadline) {
			return errPollTimeout
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.Poll):
		}
	}
}

// drillManifest is a one-instance Cluster recovered from the source's backups. It has no
// `plugins` entry: a second archiver under the source's server name would write into
// the archive being proved.
func drillManifest(src Source) ([]byte, error) {
	storage := map[string]any{"size": src.StorageSize}
	if src.StorageClass != "" {
		storage["storageClass"] = src.StorageClass
	}
	cluster := map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Cluster",
		"metadata": map[string]any{
			"name": DrillClusterName(src.Name), "namespace": src.Namespace,
			"labels": map[string]any{"loopzero.io/restore-drill": src.Name},
		},
		"spec": map[string]any{
			"instances": 1,
			"imageName": src.Image,
			"enablePDB": false,
			"storage":   storage,
			"bootstrap": map[string]any{"recovery": map[string]any{"source": "source"}},
			"externalClusters": []any{map[string]any{
				"name": "source",
				"plugin": map[string]any{
					"name": pluginName,
					"parameters": map[string]any{
						"barmanObjectName": src.ObjectStore, "serverName": src.ServerName,
					},
				},
			}},
			"serviceAccountTemplate": map[string]any{"metadata": map[string]any{
				"annotations": map[string]any{"eks.amazonaws.com/role-arn": src.RoleARN},
			}},
		},
	}
	out, err := yaml.Marshal(cluster)
	if err != nil {
		return nil, fmt.Errorf("encode the drill cluster: %w", err)
	}
	return out, nil
}
