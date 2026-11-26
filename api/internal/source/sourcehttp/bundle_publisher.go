// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sourcehttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/getloopscale/loop-runtime/api/internal/source"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// bundlePublisher stores a bundle that passed the wire contract: first in the
// tenant's source repository, then — when object storage is configured — at
// the key the build cluster's clone step downloads from. Each failure comes
// back as the typed error the handler renders; the handler only decodes,
// authorizes and answers.
type bundlePublisher struct {
	store   source.SourceStore
	objects ObjectUploader // nil: object storage not configured
	bucket  string
}

// publish returns the source id (commit SHA, or "ch-{hash}" for a tar.gz).
func (p bundlePublisher) publish(ctx context.Context, tenantID, project string, bundle *os.File, correlationID string) (string, *loopzeroerrors.LoopZeroError) {
	result, err := p.store.PushBundle(ctx, tenantID, project, bundle)
	if err != nil {
		return "", pushFailure(err, tenantID, project, correlationID)
	}
	// A tar.gz push converts to a git bundle in a temp dir the caller cleans.
	if result.GitBundlePath != "" {
		defer func() { _ = os.RemoveAll(filepath.Dir(result.GitBundlePath)) }()
	}
	if p.objects == nil {
		return result.SourceID, nil
	}
	if terr := p.uploadForBuild(ctx, tenantID, result, bundle); terr != nil {
		return "", terr
	}
	return result.SourceID, nil
}

// uploadForBuild writes the bundle the build cluster clones: the converted
// git bundle for a tar.gz source, the uploaded one otherwise. With object
// storage configured this is part of the build contract — a missing object
// fails the clone step opaquely later — so a failure fails the request
// (CHG-2026-04-29-DOGFOOD iter 9; Rule 10, local equals production).
func (p bundlePublisher) uploadForBuild(ctx context.Context, tenantID string, result source.PushResult, uploaded *os.File) *loopzeroerrors.LoopZeroError {
	body, closeBody, err := buildBundleReader(result, uploaded)
	if err != nil {
		slog.Error("source upload: cannot read the bundle for object storage", "error", err)
		return loopzeroerrors.ErrInternalError
	}
	defer closeBody()
	objectName := fmt.Sprintf("%s/source/%s.bundle", tenantID, result.SourceID)
	if err := p.objects.Upload(ctx, p.bucket, objectName, body); err != nil {
		slog.Error("object store upload failed", "error", err, "key", objectName)
		return loopzeroerrors.ErrSourceUploadFailed
	}
	return nil
}

func buildBundleReader(result source.PushResult, uploaded *os.File) (io.Reader, func(), error) {
	if result.GitBundlePath != "" {
		f, err := os.Open(result.GitBundlePath)
		if err != nil {
			return nil, nil, fmt.Errorf("opening converted bundle: %w", err)
		}
		return f, func() { _ = f.Close() }, nil
	}
	if _, err := uploaded.Seek(0, io.SeekStart); err != nil {
		return nil, nil, fmt.Errorf("rewinding uploaded bundle: %w", err)
	}
	return uploaded, func() {}, nil
}

// pushFailure maps a PushBundle error to what the client is told. The ORDER
// is load-bearing (Sprint 4 T3.3, EC-3):
//
//  1. a DNS-1123 violation on tenant or project is the caller's input → 400;
//  2. concurrent pushes to one repository ("cannot lock ref" and kin, even
//     inside a typed PushBundleError) are the same fact as a deploy already
//     in progress → 409 DEPLOY_IN_PROGRESS (F-03), checked BEFORE the typed
//     mapping, which would otherwise call it a 500;
//  3. a typed PushBundleError gets its reason-specific message, carrying the
//     correlation id so a scrubbed detail still names a reference;
//  4. anything else is the neutral SOURCE_UPLOAD_FAILED.
func pushFailure(err error, tenantID, project, correlationID string) *loopzeroerrors.LoopZeroError {
	if errors.Is(err, source.ErrInvalidTenantOrProject) {
		slog.Info("source upload rejected: invalid tenant or project ID",
			"team_id", tenantID, "project", project, "error", err.Error())
		return &loopzeroerrors.LoopZeroError{Code: "INVALID_TENANT_OR_PROJECT", Message: err.Error()}
	}
	if source.IsGiteaRaceCondition(err) {
		slog.Info("source upload race resolved as DEPLOY_IN_PROGRESS",
			"team_id", tenantID, "project", project, "error", err.Error())
		return loopzeroerrors.ErrDeployInProgress
	}
	if pushErr, ok := source.AsPushBundleError(err); ok {
		slog.Error("source upload failed (typed)",
			"team_id", tenantID, "project", project,
			"reason", pushErr.Reason.String(), "detail", pushErr.Detail, "cause", err.Error())
		return pushErr.UserError(correlationID)
	}
	slog.Error("source upload failed", "error", err, "team_id", tenantID)
	return loopzeroerrors.ErrSourceUploadFailed
}
