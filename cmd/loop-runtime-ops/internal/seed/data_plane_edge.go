// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DataPlaneEdgeSeed proves that the public edge authorises /v1/{memory,rag,lens}/*
// through loop-console's forward-auth before any call reaches a service (#470). It writes
// nothing: Run and Verify are the same probe, so the seed chain can carry it as a step.
//
// An anonymous call must come back 401 with loop-console's forward-auth denial. Each
// alternative means something different and is reported as such: the service's own
// 401 means the edge let the call through unauthenticated, a 404 means loop-console has
// no forward-auth route for the service (its base URL is unset), and a 502 means the
// edge could not reach the authoriser at all.
//
// What it does NOT prove: that a valid key is accepted and a tenant key minted. That
// needs a user credential, which the seed chain does not hold.
type DataPlaneEdgeSeed struct{}

func NewDataPlaneEdgeSeed() *DataPlaneEdgeSeed { return &DataPlaneEdgeSeed{} }
func (s *DataPlaneEdgeSeed) Name() string      { return "data-plane-edge" }

const (
	dataPlaneEdgeURLEnv     = "LOOP_RUNTIME_EDGE_URL"
	dataPlaneEdgeDefaultURL = "https://app-dev.getloopscale.com"
	// forwardAuthDenialMessage is loop-console's ForwardAuthHandler answer to a call with
	// no valid key (loop-console internal/routes/forward_auth.go). The services word their
	// own 401 differently, which is what tells the two apart.
	forwardAuthDenialMessage = "Invalid or missing API key."
)

func (s *DataPlaneEdgeSeed) Run(d Deps) Result { return s.Verify(d) }

func (s *DataPlaneEdgeSeed) Verify(d Deps) Result {
	enabled, err := deployedDataPlane(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if len(enabled) == 0 {
		return Result{Verdict: SKIP, Reason: "no data-plane service is deployed in " + dataPlaneNamespace}
	}
	edge := strings.TrimSuffix(d.Host.envOr(dataPlaneEdgeURLEnv, dataPlaneEdgeDefaultURL), "/")
	for _, spec := range enabled {
		if err := probeEdgeDeniesAnonymous(d, edge, spec.edgePath); err != nil {
			return Result{Verdict: FAIL, Reason: err.Error()}
		}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s authorises %s through loop-console forward-auth", edge, serviceNames(enabled))}
}

func probeEdgeDeniesAnonymous(d Deps, edge, path string) error {
	req, err := http.NewRequestWithContext(d.Ctx, http.MethodGet, edge+path, nil)
	if err != nil {
		return fmt.Errorf("build probe %s: %w", path, err)
	}
	resp, err := d.Host.devHTTP().Do(req)
	if err != nil {
		return fmt.Errorf("probe %s%s: %w", edge, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("anonymous GET %s%s answered HTTP %d, want 401 from loop-console forward-auth: %s",
			edge, path, resp.StatusCode, snippet(raw))
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Message != forwardAuthDenialMessage {
		return fmt.Errorf("anonymous GET %s%s was refused by something other than loop-console forward-auth "+
			"(the service itself, so the edge let it through): %s", edge, path, snippet(raw))
	}
	return nil
}
