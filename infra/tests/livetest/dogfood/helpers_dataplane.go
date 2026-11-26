// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// DP9 — data-plane helpers.
//
// Bridges the dogfood LIVE suite with the loop-runtime-data-plane products
// (loop-runtime-memory, loop-runtime-rag, loop-runtime-lens) provisioned per the DP series and pinned
// at README-QA-REDTEAM.md. Reads the platform-admin keys from
// ~/.loop-runtime/dev-credentials.json § data_plane.{product}.platform_admin_key.
//
// Related:
//   - Blueprint: knowledge-base/discoveries/blueprints/dp9-data-plane-dogfood-journeys-blueprint.md
//   - Red Team doc: loop-runtime-workspace/README-QA-REDTEAM.md
//   - Anchor spec:  loop-runtime/.claude/rules/dogfood-golden-rule.md § 1 (rag-edge-live)

package dogfood

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// shortContext is a 30s deadline for the direct-host admin-mint call. The
// bootstrap path is a single HTTP round-trip; anything above 30s means the
// host process is unhealthy (Rule 8 — fail-fast, not wait-forever).
func shortContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// envKey trims + returns the named env-var. Fallback path when the
// single-use bootstrap admin table on a data-plane product has already been
// consumed and the operator has cached a pre-minted per-tenant key.
func envKey(name string) string {
	return os.Getenv(name)
}

// DataPlaneEndpoint captures how to reach one data-plane product across both
// the direct-host and edge surfaces. host_port + edge_path come verbatim from
// dev-credentials.json § data_plane so drift in the operator seed is a HARD
// error at helper time (fail-fast, Rule 8).
type DataPlaneEndpoint struct {
	Product          string // "memory" | "rag" | "lens"
	PlatformAdminKey string // control-plane key held by the loop-console broker; NOT the customer bearer
	HostPort         int    // e.g. 18081 (memory), 18082 (rag), 18084 (lens)
	EdgePath         string // e.g. "/v1/memory/" — Traefik prefix owned by the ForwardAuth chain
}

// devCredsPath is the operator-owned canonical secret file (mode 0600) shared
// with every dogfood helper. Kept in a const so a test that needs to reason
// about the seed location does not silently accept a stale env override.
const devCredsPath = ".loop-runtime/dev-credentials.json"

// LoadDataPlaneEndpoint returns the full { key, host_port, edge_path } tuple
// for one product. Used by DP-memory/DP-rag/DP-lens journeys to (a) probe the
// edge fail-closed contract on env.AppBaseURL + EdgePath and (b) exercise the
// direct-host process on 127.0.0.1:HostPort with a per-tenant customer key
// minted separately.
func LoadDataPlaneEndpoint(env Env, product string) (DataPlaneEndpoint, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: HOME not set")
	}
	raw, err := os.ReadFile(filepath.Join(home, devCredsPath))
	if err != nil {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: read %s: %w", devCredsPath, err)
	}
	// Heterogeneous § data_plane map — decode into RawMessage first, then
	// resolve only the product slot (skips sibling scalar keys like `note`,
	// `rotated_at`, `admin_bootstrap_token` that DP8 added).
	var top struct {
		DataPlane map[string]json.RawMessage `json:"data_plane"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: parse %s: %w", devCredsPath, err)
	}
	prodRaw, ok := top.DataPlane[product]
	if !ok {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: § data_plane.%s not found", product)
	}
	if len(prodRaw) == 0 || prodRaw[0] != '{' {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: § data_plane.%s is not an object", product)
	}
	var prod struct {
		PlatformAdminKey string `json:"platform_admin_key"`
		HostPort         int    `json:"host_port"`
		EdgePath         string `json:"edge_path"`
	}
	if err := json.Unmarshal(prodRaw, &prod); err != nil {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: parse § data_plane.%s: %w", product, err)
	}
	if prod.PlatformAdminKey == "" {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: § data_plane.%s.platform_admin_key empty", product)
	}
	if prod.HostPort == 0 {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: § data_plane.%s.host_port unset", product)
	}
	if prod.EdgePath == "" {
		return DataPlaneEndpoint{}, fmt.Errorf("data-plane: § data_plane.%s.edge_path unset", product)
	}
	return DataPlaneEndpoint{
		Product:          product,
		PlatformAdminKey: prod.PlatformAdminKey,
		HostPort:         prod.HostPort,
		EdgePath:         prod.EdgePath,
	}, nil
}

// DataPlaneHostURL returns the direct-process base URL for product on
// 127.0.0.1:HostPort. Direct-host paths exist so DP9 journeys can prove the
// products WORK end-to-end even while the edge Model B mint chain is under
// investigation (README-QA-REDTEAM.md § "Known edge behavior"). NOT a
// permanent shortcut — the edge assertion above still holds.
func DataPlaneHostURL(ep DataPlaneEndpoint) string {
	return fmt.Sprintf("http://127.0.0.1:%d", ep.HostPort)
}

// MintDataPlaneCustomerKey provisions a per-tenant customer key on the
// direct-host process via the product's admin bootstrap token. Returns the
// raw secret (loop_runtime_live_*/rag_live_*) suitable for use as a Bearer on the
// customer surface (/v1/*, NOT /v1/admin/*).
//
// bootstrapToken defaults to the canonical dev value declared in
// loop-runtime-workspace README-QA-REDTEAM.md ("dev-bootstrap-token-change-me-min-32-chars")
// when empty. The bootstrap admin table on rag is single-use — if the mint
// returns "unauthorized", callers SHOULD fall back to the already-persisted
// platform_admin_key (which carries the full scope set per dev/rag-up.sh:61)
// rather than hitting the pgvector reset path from a test.
func MintDataPlaneCustomerKey(env Env, ep DataPlaneEndpoint, bootstrapToken, name, workspaceID string, scopes []string) (string, error) {
	if bootstrapToken == "" {
		bootstrapToken = "dev-bootstrap-token-change-me-min-32-chars"
	}
	body, err := json.Marshal(map[string]any{
		"name":         name,
		"workspace_id": workspaceID,
		"scopes":       scopes,
	})
	if err != nil {
		return "", fmt.Errorf("data-plane mint: marshal: %w", err)
	}
	// The rag admin path is /v1/admin/keys (M15 D3 verbatim per dev/rag-up.sh:59);
	// memory + lens follow the same convention (README-QA-REDTEAM.md § working
	// direct-host paths).
	adminURL := DataPlaneHostURL(ep) + "/v1/admin/keys"
	ctx, cancel := shortContext()
	defer cancel()
	status, out, err := CurlJSON(ctx, env, "POST", adminURL, bootstrapToken, body)
	if err != nil {
		return "", fmt.Errorf("data-plane mint %s: %w", ep.Product, err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("data-plane mint %s: HTTP %d (bootstrap likely consumed — see rag-up.sh reset)", ep.Product, status)
	}
	// The mint response ships the key as either `.secret` (memory + lens) OR
	// `.raw_key` (rag, per M15 D3). Prefer `.secret`; fall back to `.raw_key`.
	if s, ok := out["secret"].(string); ok && s != "" {
		return s, nil
	}
	if s, ok := out["raw_key"].(string); ok && s != "" {
		return s, nil
	}
	return "", fmt.Errorf("data-plane mint %s: response missing .secret / .raw_key (body: %v)", ep.Product, out)
}
