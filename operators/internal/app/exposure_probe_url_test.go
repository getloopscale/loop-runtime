// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package app

import (
	"testing"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

func routeWithHostname(hostname string, tls bool) *loopruntimev1.ExposedRoute {
	r := &loopruntimev1.ExposedRoute{}
	r.Spec.Hostname = hostname
	r.Spec.TLS.Enabled = tls
	return r
}

// Where a route's hostname is the public name (production), the probe asks for it
// directly — unchanged by this configuration.
func TestProbeURLIsTheRouteHostnameWhenNoPublicDomainIsSet(t *testing.T) {
	r := &ExposureReconciler{}
	if got := r.resolveProbeURL(routeWithHostname("tn-a-web-app-production.example.com", true)); got != "https://tn-a-web-app-production.example.com" {
		t.Errorf("probe URL = %q", got)
	}
}

// On dev-public the hostname is an internal SNI (`*.loop-runtime.local`) that the edge
// rewrites the public host to, and it resolves nowhere: probing it failed for every
// app and none reached Ready (#447). With a public domain configured the probe asks
// for the URL users reach — the hostname's first label under that domain, over TLS,
// through the edge.
func TestProbeURLIsThePublicURLWhenAPublicDomainIsSet(t *testing.T) {
	r := &ExposureReconciler{ProbePublicDomain: "prod-dev.getloopscale.app"}
	got := r.resolveProbeURL(routeWithHostname("tn-admin-sample-app-production.loop-runtime.local", true))
	if want := "https://tn-admin-sample-app-production.prod-dev.getloopscale.app"; got != want {
		t.Errorf("probe URL = %q, want %q", got, want)
	}
}

// The public edge serves TLS whatever the route's own setting.
func TestProbeURLThroughThePublicDomainIsAlwaysHTTPS(t *testing.T) {
	r := &ExposureReconciler{ProbePublicDomain: "prod-dev.getloopscale.app"}
	if got := r.resolveProbeURL(routeWithHostname("x-y.loop-runtime.local", false)); got != "https://x-y.prod-dev.getloopscale.app" {
		t.Errorf("probe URL = %q", got)
	}
}

// The test-only override still wins when it is allowed, so envtest keeps its seam.
func TestProbeURLOverrideStillWinsOverThePublicDomain(t *testing.T) {
	r := &ExposureReconciler{ProbePublicDomain: "prod-dev.getloopscale.app", AllowProbeURLOverride: true}
	route := routeWithHostname("x-y.loop-runtime.local", true)
	route.Annotations = map[string]string{probeURLOverrideAnnotation: "http://127.0.0.1:9/"}
	if got := r.resolveProbeURL(route); got != "http://127.0.0.1:9/" {
		t.Errorf("probe URL = %q", got)
	}
}
