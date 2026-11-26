// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package leader implements the generation-based fencing primitive used by
// every extracted background loop (roadmap 02 — API Loop Extraction).
//
// # Why a generation counter?
//
// Leader election used to rest on `pg_try_advisory_lock`. Advisory locks
// taken that way are session-scoped — the lock is released when the Postgres
// connection closes — and re-entrant, so re-running the lock on the holder's
// connection cannot tell a live lease from a lost one. It also has one
// failure mode: a pod that has been network-isolated
// can still hold the advisory lock until Postgres detects the dead
// connection via TCP keepalive (11–90s depending on kernel tuning). During
// that window, a new leader can be elected while the old leader still
// believes it is live — a classic split brain.
//
// The generation counter solves this by making every side-effecting write
// conditional on the caller's generation matching the current generation
// in PG. When a new leader acquires the lease, it atomically increments
// the generation. Any write from the old leader uses the old (lower)
// generation and silently no-ops at the WHERE clause. The old leader's
// next Heartbeat() will fail, and it exits with os.Exit(1) so its goroutines
// stop producing work.
//
// # Contract
//
// A Lease returned by Acquire is valid until Heartbeat() or Validate()
// returns an error. The caller must:
//
//  1. Call Heartbeat() at HeartbeatInterval (default 5s).
//  2. Pass Lease.GenerationID into every state-mutating SQL WHERE clause.
//  3. Release() the lease on clean shutdown (best effort; not required
//     for correctness because a missed heartbeat is an adequate signal).
//  4. Stop everything started under the lease if Heartbeat() returns
//     ErrFenced — another leader has taken over. The controller exits; the
//     API cancels the context its loops run under and re-enters the election.
//
// The package deliberately does NOT start goroutines. The caller owns the
// heartbeat loop so tests and the controller main can drive it explicitly.
package leader

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/domain"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// BackgroundLoopsLeaseName is the lease that owns the platform's background
// loops (build reconcile, reapers, convergence, pruners). The controller
// binary and the API take the SAME lease, so the two can never run the loops
// at once. The value predates the API using it and is kept so existing
// leader_generations rows keep their meaning.
const BackgroundLoopsLeaseName = "loop-runtime-controller"

// ErrFenced is returned by Heartbeat or Validate when the lease has been
// taken over by another holder (a higher generation exists in PG, or the
// row belongs to a different holder). The caller MUST treat this as a
// fatal signal and stop all side-effecting work.
var ErrFenced = errors.New("leader: lease fenced — another holder owns a higher generation")

// ErrNotLeader is returned by Acquire when a live leader already holds the
// lease and the caller cannot take over. Callers should back off and retry.
var ErrNotLeader = errors.New("leader: lease held by a live peer")

// Lease describes a successfully acquired leadership lease. The
// GenerationID field must be passed into every side-effecting write as the
// fencing token.
type Lease struct {
	Name         string
	CellID       string
	Holder       string
	GenerationID int64
	AcquiredAt   time.Time
}

// Config tunes acquire/heartbeat semantics. Zero values give reasonable
// defaults; overrides are intended for tests.
type Config struct {
	// StaleAfter is the duration after which an existing leader is
	// considered dead and its lease can be taken over. Must be strictly
	// greater than HeartbeatInterval. Default: 30s.
	StaleAfter time.Duration
	// HeartbeatInterval is the expected cadence of Heartbeat() calls.
	// Used only for documentation and validation. Default: 5s.
	HeartbeatInterval time.Duration
}

// DefaultConfig returns conservative defaults.
func DefaultConfig() Config {
	return Config{
		StaleAfter:        30 * time.Second,
		HeartbeatInterval: 5 * time.Second,
	}
}

// Acquirer acquires and maintains leader leases. It is the single entry
// point the controller binary uses for fencing.
type Acquirer struct {
	tx  db.DBTX
	cfg Config
}

// NewAcquirer creates an Acquirer bound to the given DBTX (usually a
// pgxpool) and config.
func NewAcquirer(tx db.DBTX, cfg Config) *Acquirer {
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 30 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 5 * time.Second
	}
	return &Acquirer{tx: tx, cfg: cfg}
}

// Acquire attempts to take over the lease for (name, cellID). Behavior:
//
//   - If no row exists: INSERT at generation 1. Success.
//   - If a row exists whose heartbeat_at is older than StaleAfter: bump the
//     generation_id, rewrite holder, and reset heartbeat_at to now(). Success.
//   - If a row exists with a fresh heartbeat: return ErrNotLeader. The
//     caller should back off.
//
// The function is atomic via a single UPSERT that combines the three cases
// above into one statement, so two concurrent pods cannot both observe a
// stale row and both succeed.
func (a *Acquirer) Acquire(ctx context.Context, name, cellID, holder string) (*Lease, error) {
	if cellID == "" {
		cellID = domain.DefaultCellID
	}
	// The UPSERT uses ON CONFLICT DO UPDATE with a predicate (WHERE
	// leader_generations.heartbeat_at < now() - interval). On conflict
	// without a stale heartbeat, RETURNING is empty and we report
	// ErrNotLeader.
	//
	// Using $3::interval lets us pass Go durations without string formatting
	// that could be misread by lint/security scanners.
	const query = `
INSERT INTO leader_generations (name, cell_id, holder, generation_id, acquired_at, heartbeat_at)
VALUES ($1, $2, $4, 1, now(), now())
ON CONFLICT (name, cell_id) DO UPDATE
  SET holder        = EXCLUDED.holder,
      generation_id = leader_generations.generation_id + 1,
      acquired_at   = now(),
      heartbeat_at  = now()
  WHERE leader_generations.heartbeat_at < now() - ($3 * interval '1 second')
RETURNING generation_id, acquired_at
`
	var (
		genID      int64
		acquiredAt time.Time
	)
	staleSec := int64(a.cfg.StaleAfter.Seconds())
	row := a.tx.QueryRow(ctx, query, name, cellID, staleSec, holder)
	if err := row.Scan(&genID, &acquiredAt); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return nil, ErrNotLeader
		}
		return nil, fmt.Errorf("leader acquire (%s/%s): %w", name, cellID, err)
	}
	return &Lease{
		Name:         name,
		CellID:       cellID,
		Holder:       holder,
		GenerationID: genID,
		AcquiredAt:   acquiredAt,
	}, nil
}

// Heartbeat refreshes the lease's heartbeat_at column if and only if the
// row in PG still matches the lease's holder and generation. Any mismatch
// means another worker has taken over; the function returns ErrFenced and
// the caller MUST exit.
func (a *Acquirer) Heartbeat(ctx context.Context, lease *Lease) error {
	if lease == nil {
		// Audit gap M02 #13 fix: typed LoopRuntimeError — `nil lease` is a programming
		// error in the heartbeat goroutine wiring, not a runtime business condition.
		return loopruntimeerrors.ErrInternalError.Wrap("leader heartbeat: nil lease")
	}
	const query = `
UPDATE leader_generations
   SET heartbeat_at = now()
 WHERE name          = $1
   AND cell_id       = $2
   AND holder        = $3
   AND generation_id = $4
`
	res, err := a.tx.Exec(ctx, query, lease.Name, lease.CellID, lease.Holder, lease.GenerationID)
	if err != nil {
		return fmt.Errorf("leader heartbeat (%s/%s gen=%d): %w", lease.Name, lease.CellID, lease.GenerationID, err)
	}
	if res.RowsAffected == 0 {
		return ErrFenced
	}
	return nil
}

// Validate returns nil if the lease is still the current generation for
// (name, cell_id). Used by state-transition helpers that want to fail
// fast before issuing a larger write.
func (a *Acquirer) Validate(ctx context.Context, lease *Lease) error {
	if lease == nil {
		// Audit gap M02 #13 fix: typed LoopRuntimeError — `nil lease` is programming.
		return loopruntimeerrors.ErrInternalError.Wrap("leader validate: nil lease")
	}
	const query = `
SELECT generation_id, holder
  FROM leader_generations
 WHERE name    = $1
   AND cell_id = $2
`
	var (
		currentGen    int64
		currentHolder string
	)
	row := a.tx.QueryRow(ctx, query, lease.Name, lease.CellID)
	if err := row.Scan(&currentGen, &currentHolder); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return ErrFenced
		}
		return fmt.Errorf("leader validate (%s/%s): %w", lease.Name, lease.CellID, err)
	}
	if currentGen != lease.GenerationID || currentHolder != lease.Holder {
		return ErrFenced
	}
	return nil
}

// Release best-effort drops the lease row. If the lease is already held by
// someone else, Release returns ErrFenced (but does nothing destructive).
// Release is idempotent on "row gone" — the next Acquire by another holder
// will INSERT fresh at generation 1.
//
// Release does NOT delete the row — it zeroes the heartbeat so the next
// Acquire treats the slot as stale. This preserves the generation counter
// for observability and prevents a spurious "restart from generation 1"
// after a graceful shutdown.
func (a *Acquirer) Release(ctx context.Context, lease *Lease) error {
	if lease == nil {
		return nil
	}
	const query = `
UPDATE leader_generations
   SET heartbeat_at = to_timestamp(0)
 WHERE name          = $1
   AND cell_id       = $2
   AND holder        = $3
   AND generation_id = $4
`
	res, err := a.tx.Exec(ctx, query, lease.Name, lease.CellID, lease.Holder, lease.GenerationID)
	if err != nil {
		return fmt.Errorf("leader release (%s/%s): %w", lease.Name, lease.CellID, err)
	}
	if res.RowsAffected == 0 {
		return ErrFenced
	}
	return nil
}
