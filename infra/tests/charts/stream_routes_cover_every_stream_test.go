// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

// The route without the buffering middleware is chosen by path, so a stream
// whose path the rule does not match is buffered until it ends. The deploy
// timeline ends in /timeline: on staging the console got no byte of it, and a
// deploy that had finished showed every phase as pending.

import (
	"regexp"
	"strings"
	"testing"
)

// The Server-Sent Events endpoints the API serves, as the console calls them.
var serverSentEventPaths = []string{
	"/engine/api/v1/builds/build-1/stream",
	"/engine/api/v1/projects/app/builds/stream",
	"/engine/api/v1/builds/build-1/logs/stream",
	"/engine/api/v1/projects/app/logs/stream",
	"/engine/api/v1/projects/app/signals/stream",
	"/engine/api/v1/deploys/build-1/timeline",
}

var pathRegexpOfMatch = regexp.MustCompile("PathRegexp\\(`([^`]+)`\\)")

func unbufferedPathRule(t *testing.T, routes []renderedRoute) *regexp.Regexp {
	t.Helper()
	for _, r := range routes {
		if strings.Contains(strings.Join(middlewareNames(r), ","), "body-limit") {
			continue
		}
		if m := pathRegexpOfMatch.FindStringSubmatch(r.Match); m != nil {
			return regexp.MustCompile(m[1])
		}
	}
	t.Fatalf("no route without body-limit selects by PathRegexp: %+v", routes)
	return nil
}

func assertEveryStreamIsUnbuffered(t *testing.T, rule *regexp.Regexp) {
	t.Helper()
	for _, path := range serverSentEventPaths {
		if !rule.MatchString(path) {
			t.Errorf("%s is not matched by %s, so it goes through the buffering middleware", path, rule)
		}
	}
	if rule.MatchString("/engine/api/v1/projects/app/secrets") {
		t.Errorf("%s also matches a request that carries a body", rule)
	}
}

func TestEveryStreamOfTheConsoleSkipsTheBufferingMiddleware(t *testing.T) {
	out := renderChart(t, uniqueChart(t, "public-routes"), "public-routes", nil, writeValues(t, `
console:
  enabled: true
  host: app.example.test
`))
	for _, ir := range ingressRoutesIn(t, out) {
		if ir.Metadata.Name == "public-console" {
			assertEveryStreamIsUnbuffered(t, unbufferedPathRule(t, ir.Spec.Routes))
			return
		}
	}
	t.Fatal("public-console IngressRoute not rendered")
}

func TestEveryStreamOfTheCloudHostSkipsTheBufferingMiddleware(t *testing.T) {
	values := writeValues(t, cloudRouteValues+`  streamMiddlewares:
    - name: rate-limit
      namespace: traefik
`)
	out := renderChart(t, uniqueChart(t, "loopruntime-cloud"), "loopruntime-cloud", nil, values)
	irs := ingressRoutesIn(t, out)
	if len(irs) != 1 {
		t.Fatalf("want 1 IngressRoute, got %d", len(irs))
	}
	assertEveryStreamIsUnbuffered(t, unbufferedPathRule(t, irs[0].Spec.Routes))
}
