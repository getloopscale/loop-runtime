// Package adapters provides bridge types between infrastructure and domain interfaces.
//
// These adapters exist because the lock package uses pgx.Tx (infrastructure),
// while stores use db.DBTX (domain). Similarly, the route handlers use
// routes.BuildOrchestratorService while the real orchestrator lives in services.
// The SSE stream handler uses sse.BuildStoreReader while the store uses db.BuildStore.
//
// All adapters follow DIP: they translate between layers without leaking
// infrastructure details into business logic.
package adapters

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/sse"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// --- pgx.Tx to db.DBTX adapter ---

// PgxTxDBTX adapts a pgx.Tx to the db.DBTX interface.
// This bridges the lock package (which uses pgx.Tx) with the store layer
// (which uses db.DBTX).
type PgxTxDBTX struct {
	Tx pgx.Tx
}

func (a *PgxTxDBTX) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	rows, err := a.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &pgxRowsAdapter{rows: rows}, nil
}

func (a *PgxTxDBTX) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	tag, err := a.Tx.Exec(ctx, sql, args...)
	if err != nil {
		return db.ExecResult{}, err
	}
	return db.ExecResult{RowsAffected: tag.RowsAffected()}, nil
}

func (a *PgxTxDBTX) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	return &pgxRowAdapter{row: a.Tx.QueryRow(ctx, sql, args...)}
}

// pgxRowsAdapter adapts pgx.Rows to db.Rows.
type pgxRowsAdapter struct {
	rows pgx.Rows
}

func (r *pgxRowsAdapter) Next() bool           { return r.rows.Next() }
func (r *pgxRowsAdapter) Scan(dest ...any) error { return r.rows.Scan(dest...) }
func (r *pgxRowsAdapter) Err() error            { return r.rows.Err() }
func (r *pgxRowsAdapter) Close()                { r.rows.Close() }

// pgxRowAdapter adapts pgx.Row to db.Row.
type pgxRowAdapter struct {
	row pgx.Row
}

func (r *pgxRowAdapter) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if err != nil {
		if err == pgx.ErrNoRows {
			return db.ErrNoRows
		}
		return err
	}
	return nil
}

// --- PoolEnvironmentLocker: bridges pgxpool.Pool to services.EnvironmentLocker ---

// PoolEnvironmentLocker implements services.EnvironmentLocker using a real pgxpool.Pool.
// It acquires a transaction-scoped advisory lock (pg_advisory_xact_lock) and
// adapts the pgx.Tx to db.DBTX before calling the provided function.
//
// WHY this adapter exists: lock.WithEnvironmentLock returns pgx.Tx, but
// services.EnvironmentLocker expects db.DBTX. This adapter bridges that gap.
type PoolEnvironmentLocker struct {
	Pool *pgxpool.Pool
}

func (l *PoolEnvironmentLocker) WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	tx, err := l.Pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("lock: begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return fmt.Errorf("lock: set lock_timeout: %w", err)
	}

	lockKey := fmt.Sprintf("%s:%s:%s", tenant, project, env)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", lockKey); err != nil {
		return err
	}

	adapter := &PgxTxDBTX{Tx: tx}
	if err := fn(adapter); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("lock: commit transaction: %w", err)
	}

	return nil
}

// --- BuildOrchestratorAdapter: bridges services.BuildOrchestrator to routes.BuildOrchestratorService ---

// BuildOrchestratorAdapter implements routes.BuildOrchestratorService by
// delegating to the real services.BuildOrchestrator. It translates between
// route-level types and service-level types.
type BuildOrchestratorAdapter struct {
	Orch       *services.BuildOrchestrator
	BuildStore db.BuildStore
}

func (a *BuildOrchestratorAdapter) SubmitBuild(ctx context.Context, req routes.SubmitBuildInput) (*routes.SubmitBuildResult, error) {
	var apps []services.AppSpec
	for _, app := range req.Apps {
		appType := app.Type
		if appType == "" {
			appType = "server"
		}
		appPort := app.Port
		if appPort == 0 && appType == "server" {
			appPort = 8080
		}
		apps = append(apps, services.AppSpec{
			Name: app.Name,
			Type: appType,
			Port: appPort,
		})
	}

	resp, err := a.Orch.SubmitBuild(ctx, services.SubmitBuildRequest{
		TenantID:    req.TenantID,
		ProjectID:   req.ProjectID,
		Environment: req.Environment,
		CommitSHA:   "dev-local",
		Apps:        apps,
		CreatedBy:   req.CreatedBy,
		CallbackURL: "http://localhost/callback",
	})
	if err != nil {
		return nil, err
	}

	return &routes.SubmitBuildResult{
		BuildID:   resp.BuildID,
		StreamURL: resp.StreamURL,
	}, nil
}

func (a *BuildOrchestratorAdapter) GetBuildStatus(ctx context.Context, buildID string) (*routes.BuildStatusResponse, error) {
	build, err := a.BuildStore.GetByID(ctx, buildID)
	if err != nil {
		return nil, err
	}
	return &routes.BuildStatusResponse{
		BuildID:     build.ID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		Status:      build.Status,
		CommitSHA:   build.CommitSHA,
		CreatedBy:   build.CreatedBy,
	}, nil
}

func (a *BuildOrchestratorAdapter) OnBuildCompleted(ctx context.Context, buildID string, result routes.BuildCallbackInput) error {
	var apps []services.AppRelease
	for _, app := range result.Apps {
		apps = append(apps, services.AppRelease{
			Name:        app.Name,
			ImageDigest: app.ImageDigest,
			URL:         app.URL,
			DurationMs:  app.DurationMs,
		})
	}
	return a.Orch.OnBuildCompleted(ctx, buildID, services.BuildCallbackRequest{
		Status: result.Status,
		Apps:   apps,
	})
}

func (a *BuildOrchestratorAdapter) IsBuildTerminal(ctx context.Context, buildID string) (bool, error) {
	build, err := a.BuildStore.GetByID(ctx, buildID)
	if err != nil {
		return false, err
	}
	switch statemachine.State(build.Status) {
	case statemachine.BuildSucceeded, statemachine.BuildFailed, statemachine.BuildCancelled:
		return true, nil
	}
	return false, nil
}

// --- BuildStoreReaderAdapter: bridges db.BuildStore to sse.BuildStoreReader ---

// BuildStoreReaderAdapter implements sse.BuildStoreReader using db.BuildStore.
// The SSE stream handler needs only GetByID returning sse.BuildInfo,
// while db.BuildStore returns the full db.Build struct.
type BuildStoreReaderAdapter struct {
	Store db.BuildStore
}

func (a *BuildStoreReaderAdapter) GetByID(ctx context.Context, id string) (*sse.BuildInfo, error) {
	build, err := a.Store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &sse.BuildInfo{
		ID:     build.ID,
		Status: build.Status,
	}, nil
}

// NOTE: All Fake* types have been REMOVED from production code.
// Real implementations are required:
//   - WorkflowSubmitter → api/internal/argo/workflow_submitter.go
//   - GitOpsPusher      → api/internal/gitea/gitops_pusher.go
//   - HealthProber      → api/internal/health/prober.go
//   - SyncClient        → api/internal/argocd/sync_client.go
//   - SourceStore       → api/internal/source/gitea_store.go
