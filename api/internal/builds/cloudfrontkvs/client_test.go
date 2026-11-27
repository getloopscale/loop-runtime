// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cloudfrontkvs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	kvs "github.com/aws/aws-sdk-go-v2/service/cloudfrontkeyvaluestore"
	"github.com/aws/aws-sdk-go-v2/service/cloudfrontkeyvaluestore/types"
)

const testARN = "arn:aws:cloudfront::000000000000:key-value-store/2b5a3c1e-0000-4000-8000-000000000001"

// fakeStore behaves like a key value store: every write needs the store's current ETag
// and answers a new one; a stale ETag is a ConflictException.
type fakeStore struct {
	items     map[string]string
	version   int
	pageSize  int
	describes int
	// conflictOnce makes the next write fail as if another writer had just moved the
	// store on, so the client has to read the ETag again.
	conflictOnce bool
}

func newFakeStore() *fakeStore { return &fakeStore{items: map[string]string{}, pageSize: 2} }

func (f *fakeStore) etag() string { return fmt.Sprintf("etag-%d", f.version) }

func (f *fakeStore) checkWrite(in *string) error {
	if f.conflictOnce {
		f.conflictOnce = false
		f.version++
	}
	if aws.ToString(in) != f.etag() {
		return &types.ConflictException{Message: aws.String("stale etag")}
	}
	return nil
}

func (f *fakeStore) DescribeKeyValueStore(_ context.Context, in *kvs.DescribeKeyValueStoreInput, _ ...func(*kvs.Options)) (*kvs.DescribeKeyValueStoreOutput, error) {
	if aws.ToString(in.KvsARN) != testARN {
		return nil, &types.ResourceNotFoundException{Message: aws.String("no such store")}
	}
	f.describes++
	return &kvs.DescribeKeyValueStoreOutput{ETag: aws.String(f.etag()), KvsARN: in.KvsARN}, nil
}

func (f *fakeStore) PutKey(_ context.Context, in *kvs.PutKeyInput, _ ...func(*kvs.Options)) (*kvs.PutKeyOutput, error) {
	if err := f.checkWrite(in.IfMatch); err != nil {
		return nil, err
	}
	f.items[aws.ToString(in.Key)] = aws.ToString(in.Value)
	f.version++
	return &kvs.PutKeyOutput{ETag: aws.String(f.etag())}, nil
}

func (f *fakeStore) DeleteKey(_ context.Context, in *kvs.DeleteKeyInput, _ ...func(*kvs.Options)) (*kvs.DeleteKeyOutput, error) {
	if err := f.checkWrite(in.IfMatch); err != nil {
		return nil, err
	}
	key := aws.ToString(in.Key)
	if _, ok := f.items[key]; !ok {
		return nil, &types.ResourceNotFoundException{Message: aws.String("no such key")}
	}
	delete(f.items, key)
	f.version++
	return &kvs.DeleteKeyOutput{ETag: aws.String(f.etag())}, nil
}

func (f *fakeStore) ListKeys(_ context.Context, in *kvs.ListKeysInput, _ ...func(*kvs.Options)) (*kvs.ListKeysOutput, error) {
	keys := make([]string, 0, len(f.items))
	for k := range f.items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	start := 0
	if in.NextToken != nil {
		fmt.Sscanf(*in.NextToken, "%d", &start)
	}
	end := min(start+f.pageSize, len(keys))
	out := &kvs.ListKeysOutput{}
	for _, k := range keys[start:end] {
		out.Items = append(out.Items, types.ListKeysResponseListItem{Key: aws.String(k), Value: aws.String(f.items[k])})
	}
	if end < len(keys) {
		out.NextToken = aws.String(fmt.Sprint(end))
	}
	return out, nil
}

func TestPutStoresTheValueAsJSONUnderTheStoresCurrentETag(t *testing.T) {
	store := newFakeStore()
	c := New(store, testARN)

	if err := c.Put(context.Background(), "route:app.example.com", map[string]string{"cellId": "cell-0"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := store.items["route:app.example.com"]; got != `{"cellId":"cell-0"}` {
		t.Fatalf("stored value = %q, want the JSON of the entry", got)
	}
	if err := c.Put(context.Background(), "route:b.example.com", map[string]string{"cellId": "cell-1"}); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if store.describes != 1 {
		t.Fatalf("DescribeKeyValueStore called %d times, want 1: a write's answer carries the next ETag", store.describes)
	}
}

func TestAWriteRacedByAnotherWriterIsRetriedWithAFreshETag(t *testing.T) {
	store := newFakeStore()
	c := New(store, testARN)
	if err := c.Put(context.Background(), "route:a.example.com", "x"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	store.conflictOnce = true
	if err := c.Put(context.Background(), "route:b.example.com", "y"); err != nil {
		t.Fatalf("Put after a concurrent write: %v", err)
	}
	if _, ok := store.items["route:b.example.com"]; !ok {
		t.Fatal("the raced write was not retried")
	}
}

func TestDeletingAKeyTheStoreDoesNotHoldSucceeds(t *testing.T) {
	store := newFakeStore()
	c := New(store, testARN)
	if err := c.Delete(context.Background(), "route:gone.example.com"); err != nil {
		t.Fatalf("Delete of an absent key: %v", err)
	}
}

func TestDeleteRemovesTheKey(t *testing.T) {
	store := newFakeStore()
	c := New(store, testARN)
	ctx := context.Background()
	if err := c.Put(ctx, "route:a.example.com", "x"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := c.Delete(ctx, "route:a.example.com"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.items["route:a.example.com"]; ok {
		t.Fatal("the key is still in the store")
	}
}

func TestListKeysReadsEveryPage(t *testing.T) {
	store := newFakeStore()
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		store.items[k] = "v"
	}
	keys, err := New(store, testARN).ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if fmt.Sprint(keys) != "[a b c d e]" {
		t.Fatalf("ListKeys = %v, want every key across the pages", keys)
	}
}

func TestAStoreThatDoesNotExistIsReportedWithItsARN(t *testing.T) {
	err := New(newFakeStore(), testARN+"-missing").Put(context.Background(), "k", "v")
	var notFound *types.ResourceNotFoundException
	if !errors.As(err, &notFound) {
		t.Fatalf("Put on a missing store: %v, want a ResourceNotFoundException", err)
	}
	if want := testARN + "-missing"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not name the store %s", err, want)
	}
}
