// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// edgeRouteKeyPrefix namespaces the route entries in the edge store, apart from the
// static-app entries that share it.
const edgeRouteKeyPrefix = "route:"

// EdgeKV is the edge's key-value store — the Cloudflare KV the production Worker reads.
// *cfkv.Client satisfies it.
type EdgeKV interface {
	ListKeys(ctx context.Context) ([]string, error)
	Put(ctx context.Context, key string, value interface{}) error
	Delete(ctx context.Context, key string) error
}

// EdgeRouteEntry is the value stored under route:<label|fqdn>: the cell that serves
// the host. The edge maps a cell to its own ingress (ingress:<cellId>).
type EdgeRouteEntry struct {
	CellID string `json:"cellId"`
}

// EdgeKVPublisher mirrors the host → cell index into the edge store, so the production
// Worker routes by lookup instead of parsing a tenant out of the host, which the
// platform's hostnames do not allow (#448). It writes only what changed.
type EdgeKVPublisher struct {
	kv     EdgeKV
	logger *slog.Logger

	mu        sync.Mutex
	published map[string]string // kv key → cell last written ("" = present, value unknown)
	listed    bool
}

// NewEdgeKVPublisher builds a publisher over kv.
func NewEdgeKVPublisher(kv EdgeKV, logger *slog.Logger) *EdgeKVPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &EdgeKVPublisher{kv: kv, logger: logger.With("component", "edge-kv-publisher"), published: map[string]string{}}
}

// Publish writes every route whose cell changed and, when complete is true, removes
// the routes that no longer exist. complete is false when some tenant's placement
// could not be read: its routes are missing for a transient reason, and deleting them
// would turn that into an outage at the edge. Failed writes are retried next round.
func (p *EdgeKVPublisher) Publish(ctx context.Context, index map[string]string, complete bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.listed {
		keys, err := p.kv.ListKeys(ctx)
		if err != nil {
			return fmt.Errorf("list edge store keys: %w", err)
		}
		for _, k := range keys {
			if strings.HasPrefix(k, edgeRouteKeyPrefix) {
				p.published[k] = ""
			}
		}
		p.listed = true
	}

	desired := make(map[string]string, len(index))
	for key, cell := range index {
		if kvKey := edgeRouteKVKey(key); kvKey != "" {
			desired[kvKey] = cell
		}
	}

	var errs []error
	written := 0
	for key, cell := range desired {
		if p.published[key] == cell {
			continue
		}
		if err := p.kv.Put(ctx, key, EdgeRouteEntry{CellID: cell}); err != nil {
			errs = append(errs, fmt.Errorf("put %s: %w", key, err))
			continue
		}
		p.published[key] = cell
		written++
	}
	removed := 0
	if complete {
		for key := range p.published {
			if _, ok := desired[key]; ok {
				continue
			}
			if err := p.kv.Delete(ctx, key); err != nil {
				errs = append(errs, fmt.Errorf("delete %s: %w", key, err))
				continue
			}
			delete(p.published, key)
			removed++
		}
	}
	if written > 0 || removed > 0 {
		p.logger.Info("edge routes published", "written", written, "removed", removed, "routes", len(desired))
	}
	return errors.Join(errs...)
}

// edgeRouteKVKey maps an index key ("label:<label>" or "host:<fqdn>") to its store key.
// A label has no dot and an fqdn always has one, so the two never collide.
func edgeRouteKVKey(indexKey string) string {
	if v, ok := strings.CutPrefix(indexKey, "label:"); ok {
		return edgeRouteKeyPrefix + v
	}
	if v, ok := strings.CutPrefix(indexKey, "host:"); ok {
		return edgeRouteKeyPrefix + v
	}
	return ""
}
