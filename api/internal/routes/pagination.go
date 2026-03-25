// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"net/http"
	"strconv"
)

const (
	// DefaultPageSize is the default number of items per page when limit is not specified.
	DefaultPageSize = 50

	// MaxPageSize is the maximum allowed items per page.
	MaxPageSize = 100
)

// PaginationParams holds parsed pagination query parameters.
type PaginationParams struct {
	Limit  int
	Cursor string
}

// ParsePagination extracts pagination parameters from a request's query string.
// - limit: number of items (default 50, max 100). Invalid or non-positive values use default.
// - cursor: opaque cursor for cursor-based pagination (empty string if not provided).
func ParsePagination(r *http.Request) PaginationParams {
	limit := DefaultPageSize

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	return PaginationParams{
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	}
}
