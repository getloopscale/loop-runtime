package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolStatsCollector periodically exports pgx connection pool stats as Prometheus gauges.
type PoolStatsCollector struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	stop   chan struct{}
}

// NewPoolStatsCollector creates a collector that exports pool stats every interval.
func NewPoolStatsCollector(pool *pgxpool.Pool, logger *slog.Logger) *PoolStatsCollector {
	return &PoolStatsCollector{
		pool:   pool,
		logger: logger,
		stop:   make(chan struct{}),
	}
}

// Start begins periodic collection. Call Stop() to terminate.
func (c *PoolStatsCollector) Start(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.collect()
			case <-c.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop terminates the periodic collection.
func (c *PoolStatsCollector) Stop() {
	close(c.stop)
}

func (c *PoolStatsCollector) collect() {
	stats := c.pool.Stat()
	DBConnectionsActive.Set(float64(stats.AcquiredConns()))
	DBConnectionsIdle.Set(float64(stats.IdleConns()))
	DBConnectionsTotal.Set(float64(stats.TotalConns()))
}
