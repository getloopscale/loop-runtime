// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdminHydraClientSeed is the Go port of
// infra/scripts/local/seed-admin-hydra-client-canonical.sh.
//
// It registers/refreshes the admin-cli Hydra OAuth2 client whose client_id ==
// admin@getloopscale.com user_id (a UUID from users table). This makes the
// `client_credentials` grant emit a JWT with `sub == client_id ==
// admin_user_id` — engine auth resolves tenant via tenant_members DB lookup
// on `user_id = sub`.
//
// Parity contract:
//   - PG connection fails / admin user missing / tenant row missing → FAIL
//   - Hydra admin API unreachable / non-200 response                → FAIL
//   - filesystem write of secret fails                              → FAIL
//   - success (create OR PUT-refresh)                               → PASS
//
// Idempotent: probe by client_id, PUT-refresh when present. Persists the
// client_secret at $HOME/.loop-runtime/admin-cli-client-secret.txt mode 0600.
type AdminHydraClientSeed struct{}

func NewAdminHydraClientSeed() *AdminHydraClientSeed { return &AdminHydraClientSeed{} }
func (s *AdminHydraClientSeed) Name() string         { return "admin-hydra-client" }

const (
	pgDatabaseURLEnv          = "LOOP_RUNTIME_ADMIN_HYDRA_PG_URL"
	pgDatabaseURLDefault      = "postgres://loop_runtime:loop-runtime-dev-password@127.0.0.1:15432/loop_runtime?sslmode=disable"
	adminEmailCanonical       = "admin@getloopscale.com"
	hydraAdminNamespaceCanon  = "loop-runtime-auth"
	hydraAdminServiceName     = "ory-hydra-admin"
	hydraAdminRemotePort      = 4445
	adminCLIClientName        = "loop-runtime-admin-cli"
	adminCLIClientScope       = "openid offline_access"
	adminCLIClientAudience    = "loop-runtime-engine"
	adminCLIClientSecretFile  = ".loop-runtime/admin-cli-client-secret.txt"
	adminCLIClientSecretBytes = 32
)

func (s *AdminHydraClientSeed) Run(d Deps) Result {
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would resolve admin user_id + register/PUT Hydra client + persist secret"}
	}
	// Step 1 — PG lookup.
	pgURL := d.Host.envOr(pgDatabaseURLEnv, pgDatabaseURLDefault)
	userID, err := adminHydraLookupUserID(d.Ctx, d.Host, pgURL, adminEmailCanonical)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("PG lookup: %v (run seed admin-loop-runtime-dev first)", err)}
	}
	if userID == "" {
		return Result{Verdict: FAIL, Reason: "admin@getloopscale.com not in users table (run seed admin-loop-runtime-dev first)"}
	}

	// Step 2 — port-forward Hydra admin API.
	tunnel, err := openHydraAdminTunnel(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	defer tunnel.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", tunnel.LocalPort)
	secretPath, err := adminCLIClientSecretPath()
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("resolve secret path: %v", err)}
	}
	secret, err := adminHydraRegisterOrRefresh(d.Ctx, base, userID, secretPath)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if err := writeSecretFile(secretPath, secret); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("persist secret file: %v", err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("admin-cli Hydra client canonical (client_id=%s, secret persisted %s)", userID, secretPath)}
}

// Verify probes: (a) PG has admin user, (b) Hydra has ≥ 3 clients live.
// Skips the token-mint probe (bash also flagged it informational).
func (s *AdminHydraClientSeed) Verify(d Deps) Result {
	pgURL := d.Host.envOr(pgDatabaseURLEnv, pgDatabaseURLDefault)
	userID, err := adminHydraLookupUserID(d.Ctx, d.Host, pgURL, adminEmailCanonical)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("PG lookup: %v", err)}
	}
	if userID == "" {
		return Result{Verdict: FAIL, Reason: "admin@getloopscale.com not in users"}
	}
	tunnel, err := openHydraAdminTunnel(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	defer tunnel.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", tunnel.LocalPort)
	got, err := adminHydraGetClient(d.Ctx, base, userID)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get client: %v", err)}
	}
	if !got {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("client %s not registered in Hydra", userID)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("admin-cli client %s live in Hydra", userID)}
}

// adminHydraLookupUserID connects to PG and looks up the admin user id.
// Small connection: single query, no pool needed.
// Uses Host.PGConnect (shared with tenant_fixture + admin_loop_runtime_dev) so
// tests can substitute a stubPgConn without a live PG.
func adminHydraLookupUserID(ctx context.Context, h Host, url, email string) (string, error) {
	c, err := h.pgConnect(ctx, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close(ctx) }()
	// Timeout the query so a stuck PG doesn't hang the halt-loop.
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var id string
	err = c.QueryRow(qctx, "SELECT id FROM users WHERE email = $1", email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// adminHydraRegisterOrRefresh probes GET /admin/clients/{id} and either
// PUT-refreshes with the existing/newly-minted secret OR POSTs a new client.
func adminHydraRegisterOrRefresh(ctx context.Context, base, clientID, secretPath string) (string, error) {
	// Probe first — 200 means already registered.
	getURL := fmt.Sprintf("%s/admin/clients/%s", base, clientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("GET client: %w", err)
	}
	existing := resp.StatusCode == http.StatusOK
	_ = resp.Body.Close()

	var secret string
	if existing {
		// Only an absent or empty file justifies minting a new secret; any
		// other read failure would silently rotate a registered client.
		b, ferr := os.ReadFile(secretPath)
		switch {
		case ferr == nil && len(b) > 0:
			secret = string(b)
		case ferr == nil || errors.Is(ferr, fs.ErrNotExist):
			secret = mustHexSecret(adminCLIClientSecretBytes)
		default:
			return "", fmt.Errorf("read client secret %s: %w", secretPath, ferr)
		}
	} else {
		secret = mustHexSecret(adminCLIClientSecretBytes)
	}

	payload := map[string]interface{}{
		"client_id":                  clientID,
		"client_name":                adminCLIClientName,
		"client_secret":              secret,
		"grant_types":                []string{"client_credentials"},
		"response_types":             []string{},
		"scope":                      adminCLIClientScope,
		"audience":                   []string{adminCLIClientAudience},
		"token_endpoint_auth_method": "client_secret_post",
	}
	body, _ := json.Marshal(payload)

	var (
		method   = http.MethodPost
		target   = base + "/admin/clients"
		wantCode = http.StatusCreated
	)
	if existing {
		method = http.MethodPut
		target = getURL
		wantCode = http.StatusOK
	}
	req2, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := (&http.Client{Timeout: 10 * time.Second}).Do(req2)
	if err != nil {
		return "", fmt.Errorf("%s admin/clients: %w", method, err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != wantCode {
		b, _ := io.ReadAll(resp2.Body)
		return "", fmt.Errorf("%s admin/clients HTTP %d: %s", method, resp2.StatusCode, snippet(b))
	}
	return secret, nil
}

// adminHydraGetClient probes GET /admin/clients/{id} and returns true when 200.
func adminHydraGetClient(ctx context.Context, base, clientID string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/admin/clients/%s", base, clientID), nil)
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// adminCLIClientSecretPath returns the mode-0600 file path for the client secret.
func adminCLIClientSecretPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, adminCLIClientSecretFile), nil
}

// writeSecretFile writes the secret with mode 0600, creating parent dirs.
func writeSecretFile(path, secret string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(secret), 0o600)
}

// mustHexSecret returns hex-encoded random bytes (equivalent to
// `openssl rand -hex nBytes`). Panics on rand.Reader failure — which is
// unrecoverable.
func mustHexSecret(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// openHydraAdminTunnel opens a tunnel to the Hydra admin API. The pod is
// labelled by its service name on some installs and by plain "hydra" on
// others, so both are tried in that order.
func openHydraAdminTunnel(d Deps) (*Tunnel, error) {
	if d.PortForwardCtor == nil {
		return nil, fmt.Errorf("PortForwardCtor not wired")
	}
	target := TunnelTarget{
		Namespace:  hydraAdminNamespaceCanon,
		PodLabels:  PodMetaLabels(hydraAdminServiceName),
		RemotePort: hydraAdminRemotePort,
		ProbePath:  "/health/ready",
	}
	tunnel, err := d.PortForwardCtor(d.Ctx, target)
	if err == nil {
		return tunnel, nil
	}
	target.PodLabels = PodMetaLabels("hydra")
	tunnel, fallbackErr := d.PortForwardCtor(d.Ctx, target)
	if fallbackErr != nil {
		return nil, fmt.Errorf("port-forward %s: %w (fallback label hydra: %w)", hydraAdminServiceName, err, fallbackErr)
	}
	return tunnel, nil
}
