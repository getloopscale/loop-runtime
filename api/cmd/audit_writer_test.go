// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

// The audit writer runs after the handler, detached from the request, and
// writes a row attributed to the tenant it is given. audit_log is fail-closed
// under Row Level Security, so the write must carry that tenant's scope; with
// none, every mutating request's audit record was refused and only logged.
func TestTheAuditWriterScopesItsWriteToTheAuditedTenant(t *testing.T) {
	var scope string
	var scoped bool
	writer := newAuditWriter(func(ctx context.Context, _ store.AuditEntry) error {
		scope, scoped = dbtx.TenantScope(ctx)
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	writer("tn-audited", "project.created", "u-1", "project", "p-1", "corr-1", nil)

	if !scoped || scope != "tn-audited" {
		t.Fatalf("audit write scope = %q (scoped=%v); want tn-audited", scope, scoped)
	}
}
