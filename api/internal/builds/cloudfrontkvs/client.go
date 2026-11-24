// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package cloudfrontkvs writes the edge's routing entries to a CloudFront KeyValueStore,
// which the CloudFront Function in front of the cells reads to send a request to the
// cell that serves its host. It satisfies placement.EdgeKV, like the Cloudflare KV client
// (package cfkv).
package cloudfrontkvs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	kvs "github.com/aws/aws-sdk-go-v2/service/cloudfrontkeyvaluestore"
	"github.com/aws/aws-sdk-go-v2/service/cloudfrontkeyvaluestore/types"
)

// API is the part of the KeyValueStore client this package calls; *kvs.Client
// satisfies it.
type API interface {
	DescribeKeyValueStore(ctx context.Context, in *kvs.DescribeKeyValueStoreInput, optFns ...func(*kvs.Options)) (*kvs.DescribeKeyValueStoreOutput, error)
	ListKeys(ctx context.Context, in *kvs.ListKeysInput, optFns ...func(*kvs.Options)) (*kvs.ListKeysOutput, error)
	PutKey(ctx context.Context, in *kvs.PutKeyInput, optFns ...func(*kvs.Options)) (*kvs.PutKeyOutput, error)
	DeleteKey(ctx context.Context, in *kvs.DeleteKeyInput, optFns ...func(*kvs.Options)) (*kvs.DeleteKeyOutput, error)
}

// Client writes to one key value store, named by its ARN.
//
// Every write to a key value store names the store's current ETag (If-Match) and answers
// the next one, so the client keeps the last ETag it saw and reads it again only when a
// write finds it stale. Writes are serialized for the same reason.
type Client struct {
	api API
	arn string

	mu   sync.Mutex
	etag string
}

// New returns a client for the store arn over api.
func New(api API, arn string) *Client {
	return &Client{api: api, arn: arn}
}

// NewFromEnvironment returns a client for the store arn with the default AWS
// configuration: the pod's IRSA role, AWS_REGION, and AWS_ENDPOINT_URL when it is set
// (the AWS emulator on staging). An empty arn returns nil, nil: the store is not
// configured.
func NewFromEnvironment(ctx context.Context, arn string) (*Client, error) {
	if arn == "" {
		return nil, nil
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration for key value store %s: %w", arn, err)
	}
	return New(kvs.NewFromConfig(cfg), arn), nil
}

// ListKeys returns every key the store holds.
func (c *Client) ListKeys(ctx context.Context) ([]string, error) {
	var keys []string
	var next *string
	for {
		out, err := c.api.ListKeys(ctx, &kvs.ListKeysInput{KvsARN: aws.String(c.arn), NextToken: next})
		if err != nil {
			return nil, fmt.Errorf("list keys of key value store %s: %w", c.arn, err)
		}
		for _, item := range out.Items {
			keys = append(keys, aws.ToString(item.Key))
		}
		if out.NextToken == nil {
			return keys, nil
		}
		next = out.NextToken
	}
}

// Put stores value, encoded as JSON, under key.
func (c *Client) Put(ctx context.Context, key string, value interface{}) error {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode the value of %s: %w", key, err)
	}
	return c.write(ctx, "put "+key, func(etag *string) (*string, error) {
		out, err := c.api.PutKey(ctx, &kvs.PutKeyInput{
			KvsARN: aws.String(c.arn), Key: aws.String(key), Value: aws.String(string(body)), IfMatch: etag,
		})
		if err != nil {
			return nil, err
		}
		return out.ETag, nil
	})
}

// Delete removes key. A key the store does not hold is already deleted.
func (c *Client) Delete(ctx context.Context, key string) error {
	return c.write(ctx, "delete "+key, func(etag *string) (*string, error) {
		out, err := c.api.DeleteKey(ctx, &kvs.DeleteKeyInput{KvsARN: aws.String(c.arn), Key: aws.String(key), IfMatch: etag})
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return out.ETag, nil
	})
}

// write runs one write under the store's ETag. A ConflictException means another writer
// moved the store on: the ETag is read again and the write retried once.
func (c *Client) write(ctx context.Context, what string, op func(etag *string) (*string, error)) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for attempt := 0; ; attempt++ {
		if c.etag == "" {
			out, err := c.api.DescribeKeyValueStore(ctx, &kvs.DescribeKeyValueStoreInput{KvsARN: aws.String(c.arn)})
			if err != nil {
				return fmt.Errorf("%s: describe key value store %s: %w", what, c.arn, err)
			}
			c.etag = aws.ToString(out.ETag)
		}
		next, err := op(aws.String(c.etag))
		var conflict *types.ConflictException
		if errors.As(err, &conflict) && attempt == 0 {
			c.etag = ""
			continue
		}
		if err != nil {
			c.etag = ""
			return fmt.Errorf("%s in key value store %s: %w", what, c.arn, err)
		}
		// A delete of an absent key changes nothing and answers no ETag; the one held is
		// still current.
		if next != nil {
			c.etag = aws.ToString(next)
		}
		return nil
	}
}
