// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestBuildPlan_SkipsWildcardsAndSaysSo(t *testing.T) {
	inv := writeInventory(t, "*.preview.example.dev - | reviewed | tenant apps\n")
	plan, err := BuildPlan(inv, []string{"*.preview.example.dev"}, "auth.example.dev")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Wildcards) != 1 || plan.Wildcards[0] != "*.preview.example.dev" {
		t.Errorf("wildcards = %v", plan.Wildcards)
	}
}

func TestBuildPlan_ProbesIdentityOnTheAuthHostAndEveryAuthPrefixedHost(t *testing.T) {
	inv := writeInventory(t, "auth.example.dev /userinfo | reviewed | own claims\n")
	plan, err := BuildPlan(inv, []string{"auth.example.dev"}, "auth-dev.example.dev")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	hosts := map[string]bool{}
	for _, target := range plan.Targets {
		if target.Path == "/.well-known/openid-configuration" {
			hosts[target.Host] = true
		}
	}
	if !hosts["auth.example.dev"] || !hosts["auth-dev.example.dev"] {
		t.Errorf("identity probed on %v, want both auth hosts", hosts)
	}
	// /userinfo is probed by identity, so it is not also a coverage hole.
	if len(plan.Uncovered) != 0 {
		t.Errorf("uncovered = %v, want none", plan.Uncovered)
	}
}

func TestBuildPlan_PropagatesAnUnreadableInventory(t *testing.T) {
	if _, err := BuildPlan("/nonexistent/inventory.txt", []string{"a.example.dev"}, "auth.example.dev"); err == nil {
		t.Fatal("want an error")
	}
}

func TestRun_CountsEveryOutcomeAndSeparatesHolesFromLeaks(t *testing.T) {
	cli, host := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/engine/api/v1/projects":
			w.WriteHeader(http.StatusOK) // a leak
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	plan := Plan{
		Targets: []Target{
			{Host: host, Path: "/engine/api/v1/projects"},
			{Host: host, Path: "/v1/dashboard/status/versions"},
		},
		Uncovered: []string{host + " /v1/new/*"},
		Wildcards: []string{"*.preview.example.dev"},
		AuthHost:  host,
	}
	rep := Run(context.Background(), plan, "inv.txt", Checks{Client: cli})

	if rep.Pass != 1 || rep.Fail != 1 || rep.Skip != 1 || rep.Uncovered != 1 {
		t.Errorf("pass=%d fail=%d skip=%d uncovered=%d", rep.Pass, rep.Fail, rep.Skip, rep.Uncovered)
	}
	if !rep.Leaked() || rep.Closed() {
		t.Error("a 200 without a credential is a leak and the run is not closed")
	}
	if len(rep.Failures) != 1 || !strings.Contains(rep.Failures[0], "/engine/api/v1/projects") {
		t.Errorf("failures = %v", rep.Failures)
	}
	if len(rep.NotMeasured) != 1 || !strings.Contains(rep.NotMeasured[0], "/v1/new/*") {
		t.Errorf("not measured = %v", rep.NotMeasured)
	}
}

func TestRun_OnlyCoverageHolesIsNotALeak(t *testing.T) {
	rep := Run(context.Background(), Plan{Uncovered: []string{"h.example.dev /v1/new/*"}}, "inv.txt", Checks{})
	if rep.Leaked() {
		t.Error("a coverage hole is not a leak")
	}
	if rep.Closed() {
		t.Error("a run with coverage holes has not proved closure")
	}
}

func TestRun_ClassifiesWhichSurfaceIsDown(t *testing.T) {
	cli, host := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/engine/") {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	plan := Plan{Targets: []Target{
		{Host: host, Path: "/engine/api/v1/projects"},
		{Host: host, Path: "/v1/dashboard/status/versions"},
	}}
	rep := Run(context.Background(), plan, "inv.txt", Checks{Client: cli})
	if rep.Upstream != UpstreamEngineDown.Explanation() {
		t.Errorf("upstream = %q", rep.Upstream)
	}
}

func TestRun_ConfigurationFindingsAreFailures(t *testing.T) {
	rep := Run(context.Background(), Plan{AuthHost: "auth.example.dev"}, "inv.txt", Checks{
		Discovery:    func(context.Context, string) []string { return []string{"issuer advertises an internal host"} },
		Placeholders: func() []string { return []string{"client_id still holds a marker"} },
	})
	if rep.Fail != 2 || len(rep.DiscoveryLeaks) != 1 || len(rep.PlaceholderLeaks) != 1 {
		t.Fatalf("fail=%d discovery=%v placeholders=%v", rep.Fail, rep.DiscoveryLeaks, rep.PlaceholderLeaks)
	}
	if !strings.HasPrefix(rep.Failures[0], "auth.example.dev OIDC discovery: ") {
		t.Errorf("failure = %q", rep.Failures[0])
	}
}
