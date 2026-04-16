// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package workqueue

import (
	"context"
	"log/slog"
	"time"
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
		if err := c.handler(ctx, item); err != nil {
			c.logger.Warn("work item failed",
				"item_id", item.ID,
				"attempts", item.Attempts,
				"error", err)
			_ = c.queue.Fail(ctx, item.ID, err.Error())
		} else {
			_ = c.queue.Complete(ctx, item.ID)
		}
	}
}
