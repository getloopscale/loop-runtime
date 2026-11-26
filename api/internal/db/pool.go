// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig holds configurable database pool parameters.
// All values have sensible defaults set by config.Load().
type PoolConfig struct {
	MaxConns          int32
	MinConns          int32
	MaxConnIdleTime   time.Duration
	MaxConnLifetime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
	StatementTimeout  string // milliseconds as string for PostgreSQL runtime param
	// TenantScoped makes every connection the pool hands out carry the Row
	// Level Security scope of the caller's context (dbtx.WithTenantScope).
	// Set on the request pool only; see tenantScoper.
	TenantScoped bool
}

// NewPool creates a PgxAdapter wrapping a new pgxpool.Pool connected to the given URL.
//
// Pool configuration follows spec §7.8:
//   - MaxConns: upper bound for concurrent connections
//   - MinConns: keep warm connections to avoid cold-start latency
//   - MaxConnIdleTime: reclaim idle connections to prevent pool bloat
//   - MaxConnLifetime: rotate connections to pick up DNS/config changes
//   - HealthCheckPeriod: detect stale connections before they're used
//   - ConnectTimeout: fail fast if PostgreSQL is unreachable
//   - StatementTimeout: safety net against runaway queries
func NewPool(ctx context.Context, databaseURL string, poolCfg PoolConfig) (*PgxAdapter, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: failed to parse database URL: %w", err)
	}

	cfg.MaxConns = poolCfg.MaxConns
	cfg.MinConns = poolCfg.MinConns
	cfg.MaxConnIdleTime = poolCfg.MaxConnIdleTime
	cfg.MaxConnLifetime = poolCfg.MaxConnLifetime
	cfg.HealthCheckPeriod = poolCfg.HealthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = poolCfg.ConnectTimeout
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = poolCfg.StatementTimeout

	// Sprint 6: Distributed tracing for database queries.
	// otelpgx hooks into pgx to create spans for every query, batch, and prepare.
	// Spans include db.statement, db.operation, and timing. Zero-overhead when
	// the global TracerProvider is a no-op (LOOPRUNTIME_TRACING_ENABLED=false).
	cfg.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithTrimSQLInSpanName(),
	)

	// MEET-2026-04-09-043 P0: TCP keepalive for PG failover detection.
	// Without keepalive, a silent PG failover (managed DB, firewall timeout)
	// leaves zombie connections undetected for up to 2 hours (Linux default).
	// With KeepAlive=15s, dead connections are detected in ~90s (3 probes).
	// This is critical for the leader election: the dedicated leader connection
	// must detect PG death quickly to trigger os.Exit and re-election.
	cfg.ConnConfig.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		d := &net.Dialer{
			Timeout:   poolCfg.ConnectTimeout,
			KeepAlive: 15 * time.Second, // OS sends keepalive probes every 15s
		}
		return d.DialContext(ctx, network, addr)
	}

	if poolCfg.TenantScoped {
		scoper := newTenantScoper()
		cfg.PrepareConn = scoper.prepare
		cfg.BeforeClose = scoper.forget
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: failed to create connection pool: %w", err)
	}
	return &PgxAdapter{Pool: pool}, nil
}
