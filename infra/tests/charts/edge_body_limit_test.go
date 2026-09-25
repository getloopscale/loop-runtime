// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// SD-14.1 / SD-04.6 (system-design audit 2026-09-07) and the ADR
// docs/decisions/2026-09-07-source-upload-stays-on-the-api.md.
//
// The decision keeps the API as the data path for source bundles and makes the
// edge bound real. That bound has one way to go wrong, and it is the reason this
// test exists rather than a comment: if the edge cap drops below what the
// application accepts, legitimate deploys are rejected at the edge with a 413
// the API never sees and cannot explain.
//
// The middleware shipped at 10 MiB while SOURCE_MAX_BUNDLE_SIZE is 500 MB, so
// attaching it as it stood would have broken every deploy over 10 MiB. The two
// numbers must move together.
func TestEdgeBodyLimit_IsNotBelowWhatTheAppAccepts(t *testing.T) {
	mw, err := os.ReadFile("../../helm/traefik/middlewares.yaml")
	if err != nil {
		t.Fatalf("read middlewares: %v", err)
	}
	cfg, err := os.ReadFile("../../../api/internal/config/config.go")
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	edge := regexp.MustCompile(`maxRequestBodyBytes:\s*(\d+)`).FindStringSubmatch(string(mw))
	if edge == nil {
		t.Fatal("no maxRequestBodyBytes in the traefik middlewares")
	}
	edgeBytes, _ := strconv.Atoi(edge[1])

	// SOURCE_MAX_BUNDLE_SIZE default, written as an arithmetic expression.
	app := regexp.MustCompile(`"SOURCE_MAX_BUNDLE_SIZE",\s*(\d+)\s*\*\s*(\d+)\s*\*\s*(\d+)`).
		FindStringSubmatch(string(cfg))
	if app == nil {
		t.Fatal("could not read the SOURCE_MAX_BUNDLE_SIZE default from config.go")
	}
	a, _ := strconv.Atoi(app[1])
	b, _ := strconv.Atoi(app[2])
	c, _ := strconv.Atoi(app[3])
	appBytes := a * b * c

	if edgeBytes < appBytes {
		t.Errorf("edge body limit is %d bytes but the app accepts %d (SOURCE_MAX_BUNDLE_SIZE); "+
			"every deploy between the two is rejected at the edge with a 413 the API never sees",
			edgeBytes, appBytes)
	}
}

// The bound is worth nothing unattached — which is exactly what the audit found
// for both middlewares. rate-limit was attached first; body-limit waited for the
// cap to be correct, because attaching it at 10 MiB would have been worse than
// leaving it off.
func TestEdgeBodyLimit_IsAttachedToTheAPIRoute(t *testing.T) {
	// The route is rendered by the loopzero-common ingressRoute helper from the
	// middlewares the bootstrap install declares, so that declaration is what
	// attaches them (charts/loopzero-api/values-bootstrap.yaml).
	route, err := os.ReadFile("../../../charts/loopzero-api/values-bootstrap.yaml")
	if err != nil {
		t.Fatalf("read the bootstrap values: %v", err)
	}
	content := string(route)

	if !strings.Contains(content, "name: rate-limit") {
		t.Error("the rate-limit middleware is not attached to the api route")
	}
	if !strings.Contains(content, "name: body-limit") {
		t.Error("the body-limit middleware is not attached to the api route; a declared " +
			"middleware that nothing references is the finding, not the fix")
	}
}
