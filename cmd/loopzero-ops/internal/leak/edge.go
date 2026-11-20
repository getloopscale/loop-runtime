// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package leak proves that the public edge CLOSES without a credential.
//
// WHY IT EXISTS. On 2026-08-10, probing every public surface of app-dev with no
// credential at all, all ten answered 401 (or 302 into the login flow). That was
// the right answer — and nothing guaranteed it. No test asserted the closure and
// no gate checked it: it was true by luck, not by contract. The first route
// mounted outside the authenticated group would have gone unnoticed until a red
// team found it.
//
// The hermetic `proxy-exposure` gate proves every edge route was DECLARED by a
// person; it says in its own header that proving the response needs a live
// probe, `task leak:live`. This package is that probe.
//
// WHERE IT LIVES. Here and not in a `.sh`: new shell is forbidden by default, and
// an HTTP probe is exactly the case Go needs no help with. Nor is it a `quality`
// gate: every gate is hermetic by contract, and this one needs network access to
// a live target. It is its own subcommand, next to `chaos` and `drill`, which
// are the groups that touch an environment.
//
// WHAT IT DOES NOT PROVE: that an authenticated response exposes only what it
// should. That depends on the body, varies per route and does not fit a generic
// probe — `proxy-exposure` records the shape of each one in the rationale
// column, reviewed by a person.
package leak

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/credshape"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/exposure"
)

// Verdict is the outcome of probing one path.
type Verdict struct {
	Host    string
	Path    string
	Status  int
	Passed  bool
	Reason  string
	Skipped bool
}

// Target is one concrete path to probe.
//
// PublicBySpec inverts the assertion: most routes must DENY without a
// credential, but OIDC discovery and health must ANSWER — clients read them
// before holding any token, and a 401 there breaks login instead of guarding it.
type Target struct {
	Host         string
	Path         string
	PublicBySpec bool
	// Why says WHO depends on this route answering. The reason changes the fix:
	// OIDC discovery is read by a program before any token exists; the root is
	// reached by a person who typed the address.
	Why string
}

// closed reports whether the status denies access to the data.
//
// 401 and 403 deny; 302/303 send the browser to identity. 404 does NOT count:
// hiding is not denying, and accepting 404 disguises a broken route as
// protection — that is how a declared identity route answered 404 for months
// while the inventory declared it live.
func closed(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden,
		http.StatusFound, http.StatusSeeOther:
		return true
	}
	return false
}

// Probe requests one target with NO credential and returns the verdict.
func Probe(ctx context.Context, client *http.Client, t Target) Verdict {
	v := Verdict{Host: t.Host, Path: t.Path}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+t.Host+t.Path, nil)
	if err != nil {
		v.Reason = fmt.Sprintf("invalid request: %v", err)
		return v
	}
	resp, err := client.Do(req)
	if err != nil {
		v.Skipped = true
		v.Reason = fmt.Sprintf("unreachable: %v", err)
		return v
	}
	defer func() { _ = resp.Body.Close() }()
	v.Status = resp.StatusCode

	if t.PublicBySpec {
		// 200 serves the content; 302/303 leads to it. Both meet "this route
		// leads somewhere"; what fails is 404 or 5xx. Demanding 200 would fail
		// the root, which deliberately redirects to the login.
		if resp.StatusCode == http.StatusOK ||
			resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
			v.Passed = true
			v.Reason = fmt.Sprintf("answered %d", resp.StatusCode)
		} else {
			why := t.Why
			if why == "" {
				why = "it is public by the OIDC specification and clients read it before holding any token"
			}
			v.Reason = fmt.Sprintf("returned %d and must answer — %s", resp.StatusCode, why)
		}
		return v
	}

	if closed(resp.StatusCode) {
		v.Passed = true
		return v
	}
	v.Reason = fmt.Sprintf("returned %d WITHOUT a credential; the edge must deny (401/403) or send to login (302)",
		resp.StatusCode)
	return v
}

// representatives maps an inventory family to ONE concrete path.
//
// A pattern like `/v1/*` is not requestable; the probe needs a real path behind
// it. Families without a representative are NOT silenced — Targets returns them
// separately so the caller reports a coverage hole instead of counting a PASS
// that measured nothing.
var representatives = map[string]string{
	"/engine/*":                 "/engine/api/v1/projects",
	"/v1/*":                     "/v1/dashboard/status/versions",
	"/v1/integrations/github/*": "/v1/integrations/github/install",
	"/v1/memory/*":              "/v1/memory/health",
	"/v1/rag/*":                 "/v1/rag/health",
	"/v1/lens/*":                "/v1/lens/health",
	"/sessions/whoami":          "/sessions/whoami",
}

// exemptions are families this probe does not measure, each with its reason.
//
// Exempting is a decision, not an omission: without this table a new family
// without a representative would be confused with an exempt one and drop out of
// coverage.
//
// The site-level path `-` is deliberately NOT exempt. It declares the WIDEST
// possible exposure, and exempting it made whole hosts vanish from the probe —
// measured 2026-08-18: cloud.getloopscale.com and auth.getloopscale.com produced zero
// targets and zero complaints while serving live services. It falls into the
// uncovered list instead; no `/` representative is invented, because the
// responses measured at the root (404, 200) prove no closure.
var exemptions = map[string]string{
	"/self-service/*":         "browser flow; 200 is the correct answer and the body is what Kratos must deliver",
	"@auth_bff":               "named Caddy matcher, no fixed path to request",
	"/api/v1/webhooks/github": "authenticated by HMAC, not by a user credential",
}

// isExempt reports whether a family is exempt, honouring the glob the
// declaration uses (`/self-service/*` covers `/self-service/registration`).
func isExempt(family string) bool {
	if _, ok := exemptions[family]; ok {
		return true
	}
	for pattern := range exemptions {
		if prefix, ok := strings.CutSuffix(pattern, "*"); ok && strings.HasPrefix(family, prefix) {
			return true
		}
	}
	return false
}

// UncoveredAfterIdentity removes from the coverage holes the families the
// identity targets already probe, so a run never reports `/userinfo` as both
// "ok 401" and "not measured".
func UncoveredAfterIdentity(holes []string, targets []Target) []string {
	covered := map[string]bool{}
	for _, t := range targets {
		covered[t.Path] = true
		// A probed `/x/y` covers the declared family `/x/*`.
		if i := strings.LastIndex(t.Path, "/"); i > 0 {
			covered[t.Path[:i]+"/*"] = true
		}
	}
	var rest []string
	for _, h := range holes {
		fields := strings.Fields(h)
		if covered[fields[len(fields)-1]] {
			continue
		}
		rest = append(rest, h)
	}
	return rest
}

// Targets derives the probe targets for one host from the declared inventory.
//
// The targets come from infra/quality/proxy-exposure.txt and NOT from a list of
// their own: a second list would age alone, and the route somebody added to the
// Caddyfile and declared (because the gate forces it) would be exactly the one
// not probed.
//
// It returns the targets and the declared families WITHOUT a representative,
// which are coverage holes, not silence.
func Targets(inventory string, host string) (targets []Target, uncovered []string, err error) {
	decls, err := loadInventory(inventory)
	if err != nil {
		return nil, nil, err
	}

	seen := map[string]bool{}
	for _, d := range decls {
		if d.Site != host {
			continue
		}
		// An `internal` route returns no user data, so there is no credential
		// for it to deny: demanding a 401 from it would be a category error.
		if d.Class == exposure.ClassInternal {
			continue
		}
		family := d.Path
		if seen[family] {
			continue
		}
		seen[family] = true

		if isExempt(family) {
			continue
		}
		if p, ok := representatives[family]; ok {
			targets = append(targets, Target{Host: host, Path: p})
			continue
		}
		// The hole carries its host, so a report with several holes says which
		// host is uncovered.
		uncovered = append(uncovered, host+" "+family)
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].Path < targets[j].Path })
	sort.Strings(uncovered)
	return targets, uncovered, nil
}

// IdentityTargets are the identity host's routes, with the assertion inverted
// on the ones the OIDC specification requires to be public.
//
// The root is PublicBySpec because whoever reaches it is a PERSON who typed the
// address and needs a way to the login, not a dead end. Measured 2026-08-10:
// `https://auth-dev.getloopscale.com/` returned a plain-text `404 page not found`
// from Kratos itself. No product flow leads there, but a public address that
// leads nowhere is a product defect even when nothing links to it.
func IdentityTargets(host string) []Target {
	return []Target{
		{Host: host, Path: "/.well-known/openid-configuration", PublicBySpec: true,
			Why: "it is OIDC discovery, read by every client before any token exists"},
		{Host: host, Path: "/health/alive", PublicBySpec: true,
			Why: "it is the identity service health endpoint"},
		{Host: host, Path: "/", PublicBySpec: true,
			Why: "whoever types the address is a person and needs a way to the login instead of a dead end"},
		{Host: host, Path: "/userinfo"},
	}
}

// DefaultClient is the probe client: a short timeout and NO redirect following,
// because the 302 of the login flow IS the evidence — following it would
// replace the proof with its destination.
func DefaultClient() *http.Client {
	return &http.Client{
		Timeout: 12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// DiscoveryLeak reports the OIDC discovery fields that advertise a host
// unreachable from outside the cluster, or plain text.
//
// WHY A PROBE AND NOT A GATE. The Hydra chart already handles this (it uses
// `publicAuthUrl` when present); the configuration is right, and what ran was
// not. Measured 2026-08-10 on app-dev: NINE fields — `issuer` and `jwks_uri`
// among them — advertised
// `http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444`, because the sync
// ran without the variable. No hermetic gate sees that: the defect is not in the
// repository. And `issuer` is the value that goes into the token and that every
// verifier compares, so advertising it wrong breaks validation downstream.
func DiscoveryLeak(doc map[string]any) []string {
	var leaks []string
	for field, value := range doc {
		s, ok := value.(string)
		if !ok {
			continue
		}
		switch {
		case strings.Contains(s, ".svc.cluster.local"):
			leaks = append(leaks, fmt.Sprintf(
				"%s advertises the internal host %q — unreachable outside the cluster", field, s))
		case strings.HasPrefix(s, "http://"):
			leaks = append(leaks, fmt.Sprintf(
				"%s advertises plain text %q — the edge serves HTTPS", field, s))
		}
	}
	sort.Strings(leaks)
	return leaks
}

// PlaceholderLeak reports configuration values left as a substitution marker
// instead of a real credential.
//
// WHY IT EXISTS. On 2026-08-10 Google login stopped with `Error 401:
// invalid_client`. The Kratos chart reads LOOPZERO_GOOGLE_CLIENT_ID from the
// environment with `| default "REPLACE_VIA_PULUMI"`; a `helmfile sync` without
// the variable exported wrote the MARKER over the good credential, and the sync
// succeeded. The check is cheap and the failure is expensive: a marker breaks
// login entirely and only shows when someone tries to sign in.
func PlaceholderLeak(config string) []string {
	seen := map[string]bool{}
	var findings []string
	for _, line := range strings.Split(config, "\n") {
		marker, ok := credshape.PlaceholderMarker(line)
		if !ok {
			continue
		}
		field := strings.TrimSpace(line)
		if i := strings.Index(field, ":"); i > 0 {
			field = strings.TrimSpace(strings.TrimPrefix(field[:i], "- "))
		}
		if seen[field] {
			continue
		}
		seen[field] = true
		findings = append(findings, fmt.Sprintf(
			"%s still holds the marker %q instead of a credential — the sync ran without the variable exported",
			field, marker))
	}
	sort.Strings(findings)
	return findings
}
