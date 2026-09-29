// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
)

type fakeEdgeKV struct {
	values  map[string]string // key → cellId
	puts    int
	deletes int
	putErr  error
}

func newFakeEdgeKV(seed map[string]string) *fakeEdgeKV {
	kv := &fakeEdgeKV{values: map[string]string{}}
	for k, v := range seed {
		kv.values[k] = v
	}
	return kv
}

func (f *fakeEdgeKV) ListKeys(context.Context) ([]string, error) {
	keys := make([]string, 0, len(f.values))
	for k := range f.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *fakeEdgeKV) Put(_ context.Context, key string, value interface{}) error {
	if f.putErr != nil {
		return f.putErr
	}
	raw, _ := json.Marshal(value)
	var e EdgeRouteEntry
	_ = json.Unmarshal(raw, &e)
	f.values[key] = e.CellID
	f.puts++
	return nil
}

func (f *fakeEdgeKV) Delete(_ context.Context, key string) error {
	delete(f.values, key)
	f.deletes++
	return nil
}

// The production Worker cannot parse a tenant out of a host (#448), so the engine
// publishes what it already knows: route:<label> for a platform host and route:<fqdn>
// for a custom domain, each naming the cell that serves it.
func TestEdgeKVPublisherWritesEveryRouteWithItsCell(t *testing.T) {
	kv := newFakeEdgeKV(nil)
	p := NewEdgeKVPublisher(kv, nil)

	err := p.Publish(context.Background(), map[string]string{
		"label:tn-a-web-production": "cell-1",
		"host:shop.example.com":     "cell-0",
	}, true)

	if err != nil {
		t.Fatal(err)
	}
	if kv.values["route:tn-a-web-production"] != "cell-1" || kv.values["route:shop.example.com"] != "cell-0" {
		t.Errorf("KV = %v", kv.values)
	}
}

// Unchanged routes are not rewritten: a refresh every 30s must not cost one KV write
// per route per replica.
func TestEdgeKVPublisherWritesOnlyWhatChanged(t *testing.T) {
	kv := newFakeEdgeKV(nil)
	p := NewEdgeKVPublisher(kv, nil)
	idx := map[string]string{"label:a": "cell-0", "label:b": "cell-1"}
	_ = p.Publish(context.Background(), idx, true)
	before := kv.puts

	idx["label:b"] = "cell-0" // b moved (admin pin)
	_ = p.Publish(context.Background(), idx, true)

	if kv.puts-before != 1 || kv.values["route:b"] != "cell-0" {
		t.Errorf("second publish wrote %d keys (want 1, the moved route); KV = %v", kv.puts-before, kv.values)
	}
}

// A route that no longer exists is removed — including one left by an earlier engine,
// found by listing the store on the first publish. Keys that are not routes are
// someone else's (static apps) and are never touched.
func TestEdgeKVPublisherRemovesRoutesThatNoLongerExist(t *testing.T) {
	kv := newFakeEdgeKV(map[string]string{"route:gone": "cell-1", "static-app": "x"})
	p := NewEdgeKVPublisher(kv, nil)

	_ = p.Publish(context.Background(), map[string]string{"label:kept": "cell-0"}, true)

	if _, ok := kv.values["route:gone"]; ok {
		t.Error("a route that no longer exists was left in the edge store")
	}
	if kv.values["static-app"] != "x" {
		t.Error("a key that is not a route was touched")
	}
}

// When a tenant's placement could not be read, its routes are missing from this refresh
// for a transient reason. Deleting them would turn a database hiccup into an outage at the
// edge, so no deletion happens on such a round.
func TestEdgeKVPublisherNeverDeletesOnAnIncompleteRound(t *testing.T) {
	kv := newFakeEdgeKV(map[string]string{"route:tn-b-web-production": "cell-1"})
	p := NewEdgeKVPublisher(kv, nil)

	_ = p.Publish(context.Background(), map[string]string{"label:tn-a-web-production": "cell-0"}, false)

	if kv.values["route:tn-b-web-production"] != "cell-1" {
		t.Error("an incomplete round deleted a live route")
	}
}

// A write that fails is reported and retried on the next round, not remembered as done.
func TestEdgeKVPublisherRetriesAFailedWrite(t *testing.T) {
	kv := newFakeEdgeKV(nil)
	kv.putErr = errors.New("cloudflare 500")
	p := NewEdgeKVPublisher(kv, nil)
	idx := map[string]string{"label:a": "cell-0"}

	if err := p.Publish(context.Background(), idx, true); err == nil {
		t.Fatal("a failed write was not reported")
	}
	kv.putErr = nil
	_ = p.Publish(context.Background(), idx, true)
	if kv.values["route:a"] != "cell-0" {
		t.Error("the failed write was not retried on the next round")
	}
}
