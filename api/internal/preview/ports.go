// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package preview

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// PreviewCreator is the optional service used by the preview Create handler;
// PreviewService is its production implementation.
// It left routes/ for the domain package (T1.3 of deep-review-remediation-plan,
// find_arch_001) and came to the preview feature, its only consumer, when
// api/internal was organized by feature (2026-09-25).
type PreviewCreator interface {
	CreatePreview(ctx context.Context, req ports.CreatePreviewRequest) (*ports.Preview, error)
}
