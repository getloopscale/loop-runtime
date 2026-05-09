// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package workqueue

import (
	"context"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/metrics"
)

// Handler processes a single work item. Return nil on success, error on failure.
type Handler func(ctx context.Context, item WorkItem) error

// Consumer polls a queue and processes items using the registered handler.
// Multiple consumers can run concurrently on different pods — SKIP LOCKED
// ensures no duplicate processing.
type Consumer struct {
	queue    *Queue
	name     QueueName
	handler  Handler
	interval time.Duration
	batch    int
	logger   *slog.Logger
}

// ConsumerOpts configures a work queue consumer.
type ConsumerOpts struct {
	Queue    QueueName
	Handler  Handler
	Interval time.Duration // Poll interval (default: 5s)
	Batch    int           // Items per dequeue (default: 10)
}

// NewConsumer creates a consumer for the given queue.
func NewConsumer(q *Queue, opts ConsumerOpts, logger *slog.Logger) *Consumer {
	interval := opts.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	batch := opts.Batch
	if batch == 0 {
		batch = 10
	}
	return &Consumer{
		queue:    q,
		name:     opts.Queue,
		handler:  opts.Handler,
		interval: interval,
		batch:    batch,
		logger:   logger.With("component", "workqueue-consumer", "queue", string(opts.Queue)),
	}
}

// Start begins polling the queue. Blocks until context is cancelled.
// Safe to call from multiple goroutines on different pods.
func (c *Consumer) Start(ctx context.Context) {
	c.logger.Info("work queue consumer started", "interval", c.interval, "batch", c.batch)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("work queue consumer stopped")
			return
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

func (c *Consumer) poll(ctx context.Context) {
	items, err := c.queue.Dequeue(ctx, c.name, c.batch)
	if err != nil {
		c.logger.Error("dequeue failed", "error", err)
		return
	}

	for _, item := range items {
		// T1.7 of deep-review-remediation-plan (closes find_workqueue_silent_db_errors HIGH).
		// Previously: `_ = c.queue.Fail(...)` and `_ = c.queue.Complete(...)`
		// silently discarded DB errors. If the terminal-state write failed:
		//   - Complete fail → item stays InFlight → re-dequeued → duplicate
		//                     handler invocation (e.g., duplicate Argo Workflow
		//                     submissions)
		//   - Fail fail     → orphan rows accumulate unbounded
		// Now: error is logged + metric incremented. Operator can alert on
		// `loopzero_workqueue_terminal_write_fail_total` rate > 0.
		if handlerErr := c.handler(ctx, item); handlerErr != nil {
			c.logger.Warn("work item failed",
				"item_id", item.ID,
				"attempts", item.Attempts,
				"error", handlerErr)
			if failErr := c.queue.Fail(ctx, item.ID, handlerErr.Error()); failErr != nil {
				c.logger.Error("queue.Fail terminal write failed — item may be re-dequeued and produce orphan row",
					"item_id", item.ID,
					"handler_error", handlerErr,
					"db_error", failErr)
				metrics.WorkqueueTerminalWriteFailTotal.WithLabelValues("fail").Inc()
			}
		} else {
			if completeErr := c.queue.Complete(ctx, item.ID); completeErr != nil {
				c.logger.Error("queue.Complete terminal write failed — handler succeeded but item may be re-dequeued causing duplicate side-effects",
					"item_id", item.ID,
					"db_error", completeErr)
				metrics.WorkqueueTerminalWriteFailTotal.WithLabelValues("complete").Inc()
			}
		}
	}
}
