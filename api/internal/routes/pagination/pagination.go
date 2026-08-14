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
//
// `pagination.go` was already this leaf in everything but location: it declared
// ParsePagination and nothing else, and no route reached it as a handler. The
// split only forced the file to admit what it was.
package pagination

import (
	"net/http"
	"regexp"
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
// defaults (limit=50, offset=0) and cap (limit ≤ 100). Negative or
// non-numeric values fall back to defaults. Per plan
// 2026-06-08-dashboard-screens-review-fixes T2.3 + ADR D8.
func ParsePaginationParams(r *http.Request) (limit, offset int) {
	limit = ParseIntParam(r.URL.Query().Get("limit"), 50)
	offset = ParseIntParam(r.URL.Query().Get("offset"), 0)
	if limit < 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
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

// rfc1123Name is the Kubernetes name rule: lowercase, alphanumeric, hyphens, max
// 63 characters, no leading or trailing hyphen.
var rfc1123Name = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// IsRFC1123Name reports whether s is a valid Kubernetes resource name.
//
// It moved here from the projects family when two families were split apart and
// the compiler refused the shared regexp (B-080, slice-36). Two consumers is
// below the rule-of-three that usually gates an abstraction — but the fence
// forbids a family reaching into a sibling, so a leaf is the only legal home for
// something both need, and duplicating a VALIDATION RULE duplicates knowledge
// rather than lines.
//
// Deliberately NOT replaced by pkg/naming.ValidateKubernetesName, which is close
// but not the same: it returns an error, checks emptiness and length separately,
// and would change what these handlers reject and what they say when they do.
// Consolidating the two is a validation change, and this slice is a move.
func IsRFC1123Name(s string) bool {
	return rfc1123Name.MatchString(s)
}
