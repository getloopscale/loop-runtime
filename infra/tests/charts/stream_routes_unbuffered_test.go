// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

// The body-limit middleware is a Traefik buffering middleware, and buffering holds
// the whole response before it sends a byte. A Server-Sent Events stream behind it
// never reaches the client while it is open: on staging the CLI's deploy stream
// waited for the build to end and was cut with 504 at 60 s. Traefik v3.0 has no
// switch to buffer only the request, so every `.../stream` endpoint (a GET, with
// no body to limit) gets a route of its own without that middleware.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const streamMatch = "PathRegexp(`/(stream|timeline)$`)"

type renderedRoute struct {
	Match       string `json:"match"`
	Priority    int    `json:"priority"`
	Middlewares []struct {
		Name string `json:"name"`
	} `json:"middlewares"`
	Services []struct {
		Name string `json:"name"`
	} `json:"services"`
}

type renderedIngressRoute struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Routes []renderedRoute `json:"routes"`
	} `json:"spec"`
}

func ingressRoutesIn(t *testing.T, out string) []renderedIngressRoute {
	t.Helper()
	var routes []renderedIngressRoute
	for _, part := range strings.Split(out, "\n---") {
		var ir renderedIngressRoute
		if err := yaml.Unmarshal([]byte(part), &ir); err != nil {
			t.Fatalf("parse rendered doc: %v\n%s", err, part)
		}
		if ir.Kind == "IngressRoute" {
			routes = append(routes, ir)
		}
	}
	return routes
}

func middlewareNames(r renderedRoute) []string {
	names := make([]string, 0, len(r.Middlewares))
	for _, m := range r.Middlewares {
		names = append(names, m.Name)
	}
	return names
}

func writeValues(t *testing.T, values string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(f, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

const cloudRouteValues = `
ingressRoute:
  enabled: true
  host: cloud.example.test
  middlewares:
    - name: rate-limit
      namespace: traefik
    - name: body-limit
      namespace: traefik
`

func TestStreamEndpointsOfTheCloudHostSkipTheBufferingMiddleware(t *testing.T) {
	values := writeValues(t, cloudRouteValues+`  streamMiddlewares:
    - name: rate-limit
      namespace: traefik
`)
	out := renderChart(t, uniqueChart(t, "loopzero-cloud"), "loopzero-cloud", nil, values)

	irs := ingressRoutesIn(t, out)
	if len(irs) != 1 {
		t.Fatalf("want 1 IngressRoute, got %d", len(irs))
	}
	var stream, rest *renderedRoute
	for i, r := range irs[0].Spec.Routes {
		if strings.Contains(r.Match, streamMatch) {
			stream = &irs[0].Spec.Routes[i]
		} else {
			rest = &irs[0].Spec.Routes[i]
		}
	}
	if stream == nil {
		t.Fatalf("no route matches %s; routes: %+v", streamMatch, irs[0].Spec.Routes)
	}
	if !strings.Contains(stream.Match, "Host(`cloud.example.test`)") {
		t.Errorf("the stream route is not bound to the host: %q", stream.Match)
	}
	if got := middlewareNames(*stream); len(got) != 1 || got[0] != "rate-limit" {
		t.Errorf("stream route middlewares = %v, want [rate-limit]", got)
	}
	if rest == nil || !strings.Contains(strings.Join(middlewareNames(*rest), ","), "body-limit") {
		t.Errorf("the other requests lost the body limit: %+v", rest)
	}
}

func TestACloudHostWithoutStreamMiddlewaresKeepsOneRoute(t *testing.T) {
	out := renderChart(t, uniqueChart(t, "loopzero-cloud"), "loopzero-cloud", nil, writeValues(t, cloudRouteValues))

	irs := ingressRoutesIn(t, out)
	if len(irs) != 1 || len(irs[0].Spec.Routes) != 1 {
		t.Fatalf("want one IngressRoute with one route, got %+v", irs)
	}
}

func TestStreamEndpointsOfTheConsoleSkipTheBufferingMiddleware(t *testing.T) {
	values := writeValues(t, `
console:
  enabled: true
  host: app.example.test
`)
	out := renderChart(t, uniqueChart(t, "public-routes"), "public-routes", nil, values)

	var console *renderedIngressRoute
	irs := ingressRoutesIn(t, out)
	for i := range irs {
		if irs[i].Metadata.Name == "public-console" {
			console = &irs[i]
		}
	}
	if console == nil {
		t.Fatal("public-console IngressRoute not rendered")
	}
	cloudPriority := 0
	var stream *renderedRoute
	for i, r := range console.Spec.Routes {
		if strings.Contains(r.Match, "PathPrefix(`/engine/`)") && !strings.Contains(r.Match, streamMatch) {
			cloudPriority = r.Priority
		}
		if strings.Contains(r.Match, streamMatch) {
			stream = &console.Spec.Routes[i]
		}
	}
	if stream == nil {
		t.Fatalf("no console route matches %s", streamMatch)
	}
	if stream.Priority <= cloudPriority {
		t.Errorf("stream route priority %d does not win over the cloud route's %d", stream.Priority, cloudPriority)
	}
	if len(stream.Services) != 1 || stream.Services[0].Name != "loopzero-cloud" {
		t.Errorf("stream route goes to %+v, want loopzero-cloud", stream.Services)
	}
	for _, name := range middlewareNames(*stream) {
		if name == "body-limit" {
			t.Errorf("the console stream route carries body-limit: %v", middlewareNames(*stream))
		}
	}
	if !strings.Contains(strings.Join(middlewareNames(*stream), ","), "rate-limit") {
		t.Errorf("the console stream route lost rate-limit: %v", middlewareNames(*stream))
	}
}

// The installs that attach body-limit must also declare the unbuffered stream route.
func TestInstallsWithTheBodyLimitDeclareStreamMiddlewares(t *testing.T) {
	block := regexp.MustCompile(`(?m)^  streamMiddlewares:\n((?:    .*\n)+)`)
	for _, path := range []string{
		"../../../charts/loopzero-api/values-bootstrap.yaml",
		"../../helmfile/values/staging/loopzero-cloud.yaml.gotmpl",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		m := block.FindSubmatch(content)
		if m == nil {
			t.Errorf("%s attaches body-limit but declares no streamMiddlewares", path)
			continue
		}
		if strings.Contains(string(m[1]), "body-limit") {
			t.Errorf("%s puts body-limit on the stream route", path)
		}
	}
}
