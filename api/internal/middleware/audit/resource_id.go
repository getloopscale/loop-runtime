// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package audit exposes the context surface handlers use to declare the
// resource identifier the audit trail should record.
//
// Why this exists: the old middleware `AuditLog` buffered up to 64 KiB of every
// mutating request body to guess a `resource_id` from a hardcoded list of body
// keys — "project_id", "project", "projectId" — kept by hand in the transport.
// Every new domain that spelt its id a fourth way lost `resource_id` in the
// audit log silently. `SetResourceID` moves the declaration into the handler
// — the code that already knows the id — and `GetResourceID` lets `AuditLog`
// read it back without touching the request body. The handler is the source of
// truth; the middleware is the recorder.
//
// # Why a slot and not a derived context
//
// The declaration travels UP: the handler runs inside the middleware, and the
// middleware reads the id after the handler returns. A plain
// `context.WithValue` cannot carry it, because the derived context the handler
// builds is invisible to the frame above it — the middleware still holds the
// request it handed down. So `AuditLog` installs a mutable slot on the way in
// (`WithResourceIDSlot`) and reads it on the way out; `SetResourceID` fills the
// slot the handler already has a pointer to. The slot is per-request and
// mutex-guarded, so a handler that declares from a goroutine it spawned is
// still safe.
package audit

import (
	"context"
	"net/http"
	"sync"
)

// resourceIDKey is a private context-key type. Using a package-private struct
// prevents any other package from colliding on the value's storage — the
// standard Go idiom documented in `context.WithValue`.
type resourceIDKey struct{}

// resourceIDSlot is the per-request cell the route and the handler write and
// the middleware reads: the resource id (declared by the handler) and the
// action and resource type (declared where the route is registered, see
// Declare). The mutex guards the case where the handler declares from a
// goroutine while the middleware is reading after ServeHTTP returned.
type resourceIDSlot struct {
	mu           sync.Mutex
	id           string
	action       string
	resourceType string
}

func (s *resourceIDSlot) set(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = id
}

func (s *resourceIDSlot) get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

func (s *resourceIDSlot) declare(action, resourceType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.action, s.resourceType = action, resourceType
}

func (s *resourceIDSlot) identity() (action, resourceType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.action, s.resourceType
}

// WithResourceIDSlot returns a context carrying a fresh, empty declaration slot.
// `AuditLog` calls this once per request before invoking the next handler; no
// other caller needs it.
func WithResourceIDSlot(ctx context.Context) context.Context {
	return context.WithValue(ctx, resourceIDKey{}, &resourceIDSlot{})
}

// SetResourceID declares the resource id for the audit row of the request that
// carries ctx. Handlers call it as soon as the id is known — after decoding the
// body, after resolving a name, before writing the response.
//
// It is a no-op when ctx carries no slot, which is the case for requests that
// never passed through `AuditLog` (unit tests of a handler in isolation, routes
// outside the audited group). A handler must never have to ask whether it is
// being audited.
//
// Calling twice replaces the value; declaring the empty string is the same as
// declaring nothing, and the middleware then falls back to the URL path.
func SetResourceID(ctx context.Context, resourceID string) {
	if slot, ok := ctx.Value(resourceIDKey{}).(*resourceIDSlot); ok {
		slot.set(resourceID)
	}
}

// GetResourceID returns the id a handler declared through SetResourceID.
// Returns "" when nothing was declared or when the context carries no slot —
// a legitimate state, not an error. The caller (`AuditLog`) treats it as "fall
// back to the URL path".
func GetResourceID(ctx context.Context) string {
	if slot, ok := ctx.Value(resourceIDKey{}).(*resourceIDSlot); ok {
		return slot.get()
	}
	return ""
}

// Declare returns the per-route middleware that names what a mutation IS for
// the audit trail: a stable action ("project.created") and the type of
// resource it acts on ("project"). It is mounted where the route is
// registered, so the router — which already knows each route — states the
// identity instead of AuditLog reverse-engineering it from URL segments.
// Adding a route is then additive: nothing else changes.
//
// It must run inside AuditLog, which installs the slot it writes to; outside
// an audited group it does nothing.
func Declare(action, resourceType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slot, ok := r.Context().Value(resourceIDKey{}).(*resourceIDSlot); ok {
				slot.declare(action, resourceType)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// declaredIdentity returns what Declare stored for the request of ctx.
func declaredIdentity(ctx context.Context) (action, resourceType string) {
	if slot, ok := ctx.Value(resourceIDKey{}).(*resourceIDSlot); ok {
		return slot.identity()
	}
	return "", ""
}
