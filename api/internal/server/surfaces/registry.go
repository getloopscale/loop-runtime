// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package surfaces records which HTTP surfaces the router chose to register and
// which it chose to skip, so that a surface which is off can be SEEN.
//
// # Why it exists
//
// `router.go` decides fifty times whether to register a surface — `if cfg.X != nil`
// — and throws every one of those decisions away. When a dependency is absent the
// route simply is not there, and the only trace is one `Info` line at startup.
//
// Measured on the droplet on 2026-08-14: with `GITHUB_WEBHOOK_SECRET` absent,
// `POST /api/v1/webhooks/github` answers 404 while `POST /api/v1/health` answers
// 405. No error, no condition, no metric. In production that shape means a
// customer's `git push` triggers no build at all, and nothing says so (B-104).
//
// The fix is not to crash on absence. The same binary serves environments where
// absence is legitimate — an instance with no Stripe in development — so turning
// every absence into a crash trades a blind spot for an outage. What absence has
// to be is VISIBLE.
//
// # Why this package knows nothing about Prometheus
//
// It records; something else publishes. Keeping the recording free of the metrics
// client is what lets the decision be tested in milliseconds without standing up
// a collector, and it is the direction `rules/architecture.md § 2` requires:
// infrastructure implements what the domain declares, never the reverse.
//
// # Concurrency
//
// A Registry is populated while the router is being built — before the first
// request is served — and only read afterwards. It carries no lock, deliberately:
// a mutex here would suggest a concurrent-write contract that does not exist and
// that nothing tests. If a surface ever needs to be recorded after serving
// begins, that is a contract change, and it arrives with its own test.
package surfaces

import (
	"fmt"
	"sort"
	"strings"
)

// Canonical surface names. They are metric label values, so they are a contract
// with whatever dashboard or alert reads them: renaming one silently breaks the
// alert that watched it. Declared here rather than spelled at the call site so
// the compiler, not a reviewer, catches a typo.
const (
	// GitHubWebhook is the inbound GitHub delivery endpoint. Off means a
	// customer's `git push` triggers no build. B-104.
	GitHubWebhook = "github_webhook"
)

// Surface is one registration decision the router made.
type Surface struct {
	// Name is the metric label. Stable, lowercase, snake_case.
	Name string
	// Enabled is whether the router registered the surface.
	Enabled bool
	// Reason says why a disabled surface is disabled — which environment
	// variable or dependency is missing. Empty for an enabled surface.
	//
	// It is deliberately NOT a metric label: the text is free-form, and
	// free-form labels are unbounded cardinality, which is how a collector
	// falls over. It belongs in the startup summary.
	Reason string
}

// Registry collects the router's registration decisions.
type Registry struct {
	byName map[string]Surface
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Surface)}
}

// Record registers one decision.
//
// It returns an error rather than panicking on misuse because the caller is the
// router's construction path, which already returns errors, and because a panic
// during startup would take down a process that is otherwise able to serve every
// surface that IS wired.
//
// A disabled surface MUST carry a reason. A zero on a dashboard that nobody can
// act on is barely better than the silence it replaced.
// A nil Registry is a silent no-op. That is deliberate: a hundred-odd focused
// unit tests build a router Config without one, and "nobody is observing" is
// exactly the right meaning there. Forcing each of them to wire a registry would
// be churn with no reader, and the `silent-surface` gate is what guarantees the
// production path does wire it.
func (r *Registry) Record(name string, enabled bool, reason string) error {
	if r == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("surfaces: refusing to record a surface with an empty name — " +
			"it would produce a series with no usable label")
	}
	if _, dup := r.byName[name]; dup {
		return fmt.Errorf("surfaces: %q recorded twice — two surfaces sharing a name make "+
			"the series ambiguous, and an operator cannot tell which one is zero", name)
	}
	reason = strings.TrimSpace(reason)
	if !enabled && reason == "" {
		return fmt.Errorf("surfaces: %q is disabled with no reason — whoever reads the zero "+
			"needs to know what to provision, without opening the startup log", name)
	}
	r.byName[name] = Surface{Name: name, Enabled: enabled, Reason: reason}
	return nil
}

// All returns every recorded surface, ordered by name.
//
// Enabled surfaces are included on purpose. A publisher that emitted a series
// only for disabled surfaces would leave "absent" and "zero" indistinguishable
// to whoever alerts on it — which is the very ambiguity this package exists to
// remove.
func (r *Registry) All() []Surface {
	out := make([]Surface, 0, len(r.byName))
	for _, s := range r.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Disabled returns only the surfaces that were skipped, ordered by name.
//
// The ordering is stable so the startup summary reads the same on every boot; an
// unstable order turns a diff of two logs into noise, which is the opposite of
// what observability is meant to buy.
func (r *Registry) Disabled() []Surface {
	var out []Surface
	for _, s := range r.All() {
		if !s.Enabled {
			out = append(out, s)
		}
	}
	return out
}
