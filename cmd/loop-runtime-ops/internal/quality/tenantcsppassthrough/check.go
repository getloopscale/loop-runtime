// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package tenantcsppassthrough locks the invariant behind issue #59: a Traefik
// middleware that hardcodes a Content-Security-Policy must never be attached to
// tenant IngressRoutes.
//
// LoopZeroKit serves per-request CSP nonces — every SSR response carries a fresh
// `Content-Security-Policy: … 'nonce-<X>'` header AND a matching
// `<script nonce="<X>">` in the body. The two must agree or the browser blocks
// the script silently. A proxy-level middleware that sets a STATIC CSP replaces
// the header while the body keeps the nonce, so the two can no longer agree:
// every inline script in every LoopZeroKit app stops running, with no server-side
// error to notice.
//
// THIS GUARD IS GREEN TODAY, and that is the point. `renderer_ingress.go`
// renders tenant routes without a `Middlewares` field, so the static-CSP
// middleware in infra/helm/traefik/middlewares.yaml is inert. But it is inert
// BY ACCIDENT: that file's own header comment claims
//
//	"Default Traefik middlewares applied to all tenant IngressRoutes."
//
// which is false, and names two middlewares the renderer does not reference.
// Issue #59's concrete risk is an operator reading that comment and wiring the
// middleware to "finally apply the security defaults" — a change that looks
// like hardening and silently breaks every tenant's inline script.
//
// A regression lock is the right shape here precisely because there is nothing
// to fix in the code: the defect is that a correct state is undefended and
// documented as its opposite.
//
// SCOPE: asserts the renderer does not reference a CSP-bearing middleware. It
// does NOT prove the nonce survives the full Pod → Traefik → CF Tunnel → CF
// Edge path — that is #59's Part A, an E2E journey requiring a live cluster,
// and no static check can stand in for it.
package tenantcsppassthrough

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// Config points the check at the two files that together define the invariant.
type Config struct {
	RepoRoot        string
	MiddlewaresPath string // default: infra/helm/traefik/middlewares.yaml
	RendererPath    string // default: api/internal/manifests/renderer_ingress.go
}

const (
	defaultMiddlewares = "infra/helm/traefik/middlewares.yaml"
	defaultRenderer    = "api/internal/manifests/renderer_ingress.go"
)

// Outcome carries the verdict plus the middleware names involved, so a failure
// names the specific object to detach rather than describing a category.
type Outcome struct {
	Pass          bool
	Skipped       bool
	Reason        string
	CSPMiddleware []string // middleware names that define a static CSP
	Attached      []string // of those, the ones the renderer references
}

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

var (
	// A `name: <x>` line inside the metadata of a Middleware document.
	nameRe = regexp.MustCompile(`(?m)^\s*name:\s*([a-zA-Z0-9._-]+)`)
	// The directive that makes a middleware dangerous for LoopZeroKit.
	cspRe = regexp.MustCompile(`(?i)contentSecurityPolicy\s*:`)
)

// cspBearingMiddlewares returns the names of middleware documents that set a
// static CSP. Documents are split on `---`, so a CSP in one does not taint the
// name of another.
func cspBearingMiddlewares(manifest string) []string {
	var out []string
	for _, doc := range strings.Split(manifest, "\n---") {
		if !cspRe.MatchString(doc) {
			continue
		}
		if m := nameRe.FindStringSubmatch(doc); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// Check runs the guard.
func Check(cfg Config) (*Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	mwRel := cfg.MiddlewaresPath
	if mwRel == "" {
		mwRel = defaultMiddlewares
	}
	rdRel := cfg.RendererPath
	if rdRel == "" {
		rdRel = defaultRenderer
	}

	mwBody, err := os.ReadFile(filepath.Join(cfg.RepoRoot, mwRel)) //nolint:gosec // operator-controlled repo path
	if err != nil {
		return &Outcome{Skipped: true, Reason: fmt.Sprintf("middlewares manifest not readable at %s (%v) — invariant NOT verified", mwRel, err)}, nil
	}
	rdBody, err := os.ReadFile(filepath.Join(cfg.RepoRoot, rdRel)) //nolint:gosec // operator-controlled repo path
	if err != nil {
		return &Outcome{Skipped: true, Reason: fmt.Sprintf("ingress renderer not readable at %s (%v) — invariant NOT verified", rdRel, err)}, nil
	}

	names := cspBearingMiddlewares(string(mwBody))
	out := &Outcome{Pass: true, CSPMiddleware: names}
	if len(names) == 0 {
		out.Reason = fmt.Sprintf("%s declares no middleware with a static contentSecurityPolicy", mwRel)
		return out, nil
	}

	renderer := string(rdBody)
	for _, n := range names {
		if strings.Contains(renderer, n) {
			out.Pass = false
			out.Attached = append(out.Attached, n)
		}
	}
	if out.Pass {
		out.Reason = fmt.Sprintf("%s references none of the %d static-CSP middleware(s) in %s — LoopZeroKit per-request nonces pass through intact",
			rdRel, len(names), mwRel)
	}
	return out, nil
}
