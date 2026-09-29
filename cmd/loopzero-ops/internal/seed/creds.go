// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Creds is the sponsor-provided credential file (~/.loopzero/dev-credentials.json),
// the SoT for dev-public secrets. Mirrors the schema documented in the bash
// seeds (secrets-seed-dev.sh header). Absent fields are the SKIP-graceful path.
type Creds struct {
	GitHub struct {
		AppID         string `json:"app_id"`
		AppPrivateKey string `json:"app_private_key"`
		WebhookSecret string `json:"webhook_secret"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
	} `json:"github"`
	Google struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	} `json:"google"`
	Stripe struct {
		APIKey        string `json:"api_key"`
		WebhookSecret string `json:"webhook_secret"`
		PriceStarter  string `json:"price_starter"`
		PricePro      string `json:"price_pro"`
		PriceTeam     string `json:"price_team"`
	} `json:"stripe"`
	Resend struct {
		APIKey string `json:"api_key"`
	} `json:"resend"`
	OpenBao struct {
		VaultRoleID string `json:"vault_role_id"`
	} `json:"openbao"`
	Cloud struct {
		ServiceAccountKey string `json:"service_account_key"`
	} `json:"cloud"`
	// Cloudflare is the operator's DNS token: the dev-public edge issues wildcard
	// certificates by DNS-01 with it, and dev-public:dns verifies the zone.
	Cloudflare struct {
		APIToken string `json:"api_token"`
	} `json:"cloudflare"`
	// OpenAI is the model provider key the data-plane services embed and judge with (#446).
	OpenAI struct {
		APIKey string `json:"api_key"`
	} `json:"openai"`
	// DataPlane.AdminBootstrapToken lets an operator mint the first admin key of each
	// data-plane service.
	DataPlane struct {
		AdminBootstrapToken string `json:"admin_bootstrap_token"`
	} `json:"data_plane"`
}

// ErrCredsAbsent signals the creds file does not exist. Seeds map this to SKIP
// (SKIP-graceful: no sponsor input → no-op success, not an error). Callers use
// errors.Is to distinguish it from ErrCredsMalformed / ErrCredsBadMode.
var ErrCredsAbsent = errors.New("dev-credentials.json absent")

// ErrCredsBadMode signals the file exists but is not mode 0600. This is an
// operator error (a credential file world-readable) → FAIL, never SKIP.
var ErrCredsBadMode = errors.New("dev-credentials.json must be mode 0600")

// ErrCredsMalformed signals the file exists, is 0600, but is not valid JSON.
// Present-and-corrupt is an operator error (EC-4) → FAIL, never SKIP.
var ErrCredsMalformed = errors.New("dev-credentials.json is malformed JSON")

// LoadCreds reads and validates the credential file at path.
//
//   - absent            → (nil, ErrCredsAbsent)   [seed → SKIP]
//   - mode != 0600      → (nil, ErrCredsBadMode)  [seed → FAIL]
//   - malformed JSON    → (nil, ErrCredsMalformed) [seed → FAIL]
//   - valid             → (*Creds, nil)
func LoadCreds(path string) (*Creds, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrCredsAbsent
		}
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	// 0600 enforcement — the low 9 permission bits must be exactly rw-------.
	if perm := info.Mode().Perm(); perm != 0o600 {
		return nil, fmt.Errorf("%w: got %04o", ErrCredsBadMode, perm)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- operator-supplied creds path
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Creds
	// Unknown fields are tolerated (the file may carry keys other seeds use).
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCredsMalformed, err)
	}
	return &c, nil
}
