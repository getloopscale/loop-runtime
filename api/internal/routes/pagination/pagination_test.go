// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package pagination

import (
	"net/http/httptest"
	"testing"
)

func TestParsePaginationParams(t *testing.T) {
	cases := []struct {
		name, query           string
		wantLimit, wantOffset int
	}{
		{"defaults", "", DefaultPageSize, 0},
		{"custom limit and offset", "?limit=20&offset=40", 20, 40},
		{"limit capped at max", "?limit=500", MaxPageSize, 0},
		{"non-numeric limit falls back", "?limit=abc", DefaultPageSize, 0},
		{"negative limit falls back", "?limit=-5", DefaultPageSize, 0},
		{"zero limit is kept", "?limit=0", 0, 0},
		{"negative offset clamps to zero", "?offset=-3", DefaultPageSize, 0},
		{"non-numeric offset falls back", "?offset=x", DefaultPageSize, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limit, offset := ParsePaginationParams(httptest.NewRequest("GET", "/items"+c.query, nil))
			if limit != c.wantLimit || offset != c.wantOffset {
				t.Errorf("ParsePaginationParams(%q) = (%d, %d), want (%d, %d)", c.query, limit, offset, c.wantLimit, c.wantOffset)
			}
		})
	}
}

func TestSliceWindow(t *testing.T) {
	cases := []struct {
		total, limit, offset, from, to int
	}{
		{10, 3, 0, 0, 3},
		{10, 3, 8, 8, 10},
		{10, 3, 12, 10, 10},
		{0, 50, 0, 0, 0},
	}
	for _, c := range cases {
		from, to := SliceWindow(c.total, c.limit, c.offset)
		if from != c.from || to != c.to {
			t.Errorf("SliceWindow(%d, %d, %d) = [%d, %d), want [%d, %d)", c.total, c.limit, c.offset, from, to, c.from, c.to)
		}
	}
}
