package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePagination_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/items", nil)
	p := ParsePagination(r)

	if p.Limit != DefaultPageSize {
		t.Errorf("expected default limit %d, got %d", DefaultPageSize, p.Limit)
	}
	if p.Cursor != "" {
		t.Errorf("expected empty cursor, got %q", p.Cursor)
	}
}

func TestParsePagination_CustomLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/items?limit=25", nil)
	p := ParsePagination(r)

	if p.Limit != 25 {
		t.Errorf("expected limit 25, got %d", p.Limit)
	}
}

func TestParsePagination_LimitExceedsMax(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/items?limit=500", nil)
	p := ParsePagination(r)

	if p.Limit != MaxPageSize {
		t.Errorf("expected max limit %d, got %d", MaxPageSize, p.Limit)
	}
}

func TestParsePagination_InvalidLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/items?limit=abc", nil)
	p := ParsePagination(r)

	if p.Limit != DefaultPageSize {
		t.Errorf("expected default limit %d on invalid input, got %d", DefaultPageSize, p.Limit)
	}
}

func TestParsePagination_ZeroLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/items?limit=0", nil)
	p := ParsePagination(r)

	if p.Limit != DefaultPageSize {
		t.Errorf("expected default limit %d on zero, got %d", DefaultPageSize, p.Limit)
	}
}

func TestParsePagination_NegativeLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/items?limit=-5", nil)
	p := ParsePagination(r)

	if p.Limit != DefaultPageSize {
		t.Errorf("expected default limit %d on negative, got %d", DefaultPageSize, p.Limit)
	}
}

func TestParsePagination_WithCursor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/items?cursor=abc123&limit=10", nil)
	p := ParsePagination(r)

	if p.Cursor != "abc123" {
		t.Errorf("expected cursor %q, got %q", "abc123", p.Cursor)
	}
	if p.Limit != 10 {
		t.Errorf("expected limit 10, got %d", p.Limit)
	}
}
