// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package pagination holds the request-parsing helpers every handler family
// needs: pagination parameters, integer query params with a default, and the
// window slice that applies an offset and a limit.
//
// A LEAF, not a family: it serves no route. It exists because the split of
// `projects` and `environments` (B-080, slice-36) made the compiler refuse what
// had been invisible — three helpers declared in one family and reached by
// another, plus one declared in the parent and reached by a family.
package pagination

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

// ParseIntParam parses a string to int, returning defaultVal on failure or empty string.
func ParseIntParam(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

// ParsePaginationParams reads ?limit= and ?offset= query params with
// defaults (limit=DefaultPageSize, offset=0) and cap (limit ≤ MaxPageSize).
// Negative or non-numeric values fall back to defaults. Per plan
// 2026-06-08-dashboard-screens-review-fixes T2.3 + ADR D8.
func ParsePaginationParams(r *http.Request) (limit, offset int) {
	limit = ParseIntParam(r.URL.Query().Get("limit"), DefaultPageSize)
	offset = ParseIntParam(r.URL.Query().Get("offset"), 0)
	if limit < 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// SliceWindow clamps offset/limit against total and returns the [from, to)
// window. Both indices are safe for slice indexing on a slice of length
// total. Plan 2026-06-08-dashboard-screens-review-fixes T2.3 helper.
func SliceWindow(total, paginationLimit, paginationOffset int) (int, int) {
	from := paginationOffset
	if from > total {
		from = total
	}
	to := from + paginationLimit
	if to > total {
		to = total
	}
	return from, to
}
