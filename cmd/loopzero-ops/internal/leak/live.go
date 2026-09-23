// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Plan is what one live run probes. It is derived from the inventory alone —
// no network — so what a run covers can be tested without a live edge.
type Plan struct {
	// Targets are the concrete paths to probe, application hosts first and
	// identity hosts after.
	Targets []Target
	// Uncovered are declared families with no representative path: coverage
	// holes, reported apart from failures.
	Uncovered []string
	// Wildcards are declared hosts with no concrete name to resolve
	// (`*.preview-dev...`); they are skipped and said so.
	Wildcards []string
	// AuthHost is the identity host whose OIDC discovery document is read.
	AuthHost string
}

// BuildPlan derives the plan for the given application hosts.
//
// Identity routes are probed on authHost AND on every probed host whose name
// starts with "auth": auth.loopzero.example served a live Kratos on a production
// hostname, and the sweep knew the host existed without applying any identity
// assertion to it (measured: discovery 404, root 404, /userinfo 404).
func BuildPlan(inventory string, hosts []string, authHost string) (Plan, error) {
	plan := Plan{AuthHost: authHost}
	for _, h := range hosts {
		// A wildcard has no concrete hostname; probing `*.x` would produce a DNS
		// error that looks like a host being down. It is reported, not hidden.
		if strings.HasPrefix(h, "*.") {
			plan.Wildcards = append(plan.Wildcards, h)
			continue
		}
		targets, uncovered, err := Targets(inventory, h)
		if err != nil {
			return Plan{}, err
		}
		plan.Targets = append(plan.Targets, targets...)
		plan.Uncovered = append(plan.Uncovered, uncovered...)
	}

	identity := map[string]bool{authHost: true}
	for _, h := range hosts {
		if strings.HasPrefix(h, "auth") {
			identity[h] = true
		}
	}
	identityHosts := make([]string, 0, len(identity))
	for h := range identity {
		identityHosts = append(identityHosts, h)
	}
	sort.Strings(identityHosts)
	for _, h := range identityHosts {
		plan.Targets = append(plan.Targets, IdentityTargets(h)...)
	}
	plan.Uncovered = UncoveredAfterIdentity(plan.Uncovered, plan.Targets)
	return plan, nil
}

// Report is the outcome of a run. Its JSON field names are the `leak live
// --json` contract and never change; fields may be added.
type Report struct {
	// Upstream says WHICH surface is down when there is a gateway failure.
	// Empty on a normal day — a field that always talks stops being read.
	Upstream string `json:"upstream,omitempty"`
	Pass     int    `json:"pass"`
	Fail     int    `json:"fail"`
	Skip     int    `json:"skip"`
	// Uncovered counts what the probe does NOT measure, apart from what it
	// measured and failed: "the edge let something through" and "this surface
	// is not measured" call for different actions.
	Uncovered   int      `json:"uncovered"`
	Failures    []string `json:"failures"`
	NotMeasured []string `json:"not_measured"`
	// Wildcards names what was skipped for having no concrete hostname.
	Wildcards []string `json:"wildcards,omitempty"`

	// Verdicts are the per-target results, in probe order, for the text renderer.
	Verdicts []Verdict `json:"-"`
	// DiscoveryLeaks and PlaceholderLeaks are the configuration findings, kept
	// apart from Failures for the text renderer.
	DiscoveryLeaks   []string `json:"-"`
	PlaceholderLeaks []string `json:"-"`
	// UncoveredFamilies are the families behind NotMeasured.
	UncoveredFamilies []string `json:"-"`
}

// Leaked reports whether the edge let something through or advertised
// something it must not.
func (r Report) Leaked() bool { return r.Fail > 0 }

// Closed reports whether the run proved closure: nothing leaked and nothing
// declared went unmeasured.
func (r Report) Closed() bool { return r.Fail == 0 && r.Uncovered == 0 }

// Checks are the probes a run performs besides the per-target HTTP requests.
// Each is injected so a run can be exercised without a live edge or cluster.
type Checks struct {
	Client *http.Client
	// Discovery returns the OIDC discovery leaks for a host (nil when the
	// document cannot be read — the per-target probe already covers
	// availability).
	Discovery func(ctx context.Context, host string) []string
	// Placeholders returns the identity configuration fields still holding a
	// substitution marker (nil when the cluster is not reachable).
	Placeholders func() []string
}

// Run executes the plan and returns the report. It never prints.
func Run(ctx context.Context, plan Plan, inventory string, checks Checks) Report {
	rep := Report{Failures: []string{}, NotMeasured: []string{}}

	if len(plan.Wildcards) > 0 {
		rep.Wildcards = plan.Wildcards
		rep.Skip += len(plan.Wildcards)
	}

	// Remember both surfaces' codes to tell WHICH one is down: the engine path
	// crosses the dashboard, so a 502 on `/engine/*` alone does not say.
	var engineStatus, dashboardStatus int
	for _, target := range plan.Targets {
		v := Probe(ctx, checks.Client, target)
		rep.Verdicts = append(rep.Verdicts, v)
		switch {
		case strings.HasPrefix(target.Path, "/engine/"):
			engineStatus = v.Status
		case strings.HasPrefix(target.Path, "/v1/") && dashboardStatus == 0:
			// A dashboard product route does not cross the engine; the first
			// one is enough to know whether the dashboard answers.
			dashboardStatus = v.Status
		}
		switch {
		case v.Skipped:
			rep.Skip++
		case v.Passed:
			rep.Pass++
		default:
			rep.Fail++
			rep.Failures = append(rep.Failures, v.Host+v.Path+" "+v.Reason)
		}
	}

	if engineStatus != 0 && dashboardStatus != 0 {
		if st := ClassifyUpstream(engineStatus, dashboardStatus); st != UpstreamBothUp {
			rep.Upstream = st.Explanation()
		}
	}

	// The OIDC discovery document is read by every client BEFORE a token
	// exists, and the `issuer` it advertises is compared by every verifier.
	if checks.Discovery != nil {
		for _, leak := range checks.Discovery(ctx, plan.AuthHost) {
			rep.Fail++
			rep.DiscoveryLeaks = append(rep.DiscoveryLeaks, leak)
			rep.Failures = append(rep.Failures, plan.AuthHost+" OIDC discovery: "+leak)
		}
	}

	// A substitution marker that survived in the cluster breaks login entirely
	// and only shows when someone tries to sign in.
	if checks.Placeholders != nil {
		for _, finding := range checks.Placeholders() {
			rep.Fail++
			rep.PlaceholderLeaks = append(rep.PlaceholderLeaks, finding)
			rep.Failures = append(rep.Failures, "identity configuration: "+finding)
		}
	}

	// A declared family that is not measured is a coverage hole, and it still
	// counts towards the exit code — the route nobody probes is exactly what
	// this probe exists to prevent — but in its own field.
	for _, family := range plan.Uncovered {
		rep.Uncovered++
		rep.UncoveredFamilies = append(rep.UncoveredFamilies, family)
		rep.NotMeasured = append(rep.NotMeasured, fmt.Sprintf(
			"family %q is declared in %s and has no representative in the probe; without a concrete path it is never measured",
			family, inventory))
	}
	return rep
}
