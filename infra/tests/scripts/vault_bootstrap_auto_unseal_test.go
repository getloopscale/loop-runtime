// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readPath reads any file under the loopzero repo by relative path resolved
// from infra/tests/scripts/.
func readPath(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "infra", rel))
	if err != nil {
		t.Fatalf("resolve %s: %v", rel, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s not found at %s: %v", rel, p, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// TestVaultBootstrapDev_TransitAutoUnseal locks T2.1 (hermetic side) of
// plan dev-public-cross-cluster-secrets (issue #111).
//
// The legacy bootstrap chain mints Shamir shards on disk under
// ~/.loopzero/cleanroom-unseal.json — fragile (shard custody risk per
// blueprint D2). T2.1 adds an opt-in `AUTO_UNSEAL_MODE=transit` toggle
// that (a) gates the manual init/unseal/shard-write path so it does
// NOT run when transit auto-unseal is active, (b) waits for the
// Transit backend `sys/health` 200 before assuming OpenBao is ready
// (EC-3), (c) documents EC-7 (ephemeral Transit) + EC-8 (no seal
// migration). The corresponding helmfile change adds a conditional
// `seal "transit"` stanza in the vault-local OpenBao server config
// (gated by `.Values.transitAutoUnsealEnabled` — default false to
// preserve the existing Shamir path).
//
// LIVE acceptance (bao status reports Sealed=false + Seal Type=transit,
// no shard file written) requires a joint sponsor Cleanroom run, following
// the OpenBao init ceremony in docs/runbooks.md — NOT in scope here.
//
// This test is HERMETIC: pure source-level grep on the script + the
// helmfile gotmpl. No kubectl, no helm, no `bao` invocation.
func TestVaultBootstrapDev_TransitAutoUnseal(t *testing.T) {
	body := readScript(t, "vault-bootstrap-dev.sh")

	// T1. AUTO_UNSEAL_MODE env var declared with sensible default. The
	// canonical default is "shamir" — preserves the existing manual
	// init/unseal chain for operators who have not opted in.
	autoUnsealRe := regexp.MustCompile(`(?m)^AUTO_UNSEAL_MODE="\$\{AUTO_UNSEAL_MODE:-([^}]+)\}"$`)
	autoUnsealMatch := autoUnsealRe.FindStringSubmatch(body)
	if autoUnsealMatch == nil {
		t.Fatalf("AUTO_UNSEAL_MODE env var missing — T2.1 contract requires `AUTO_UNSEAL_MODE=\"${AUTO_UNSEAL_MODE:-shamir}\"` (default shamir preserves backwards compat)")
	}
	if got, want := autoUnsealMatch[1], "shamir"; got != want {
		t.Errorf("AUTO_UNSEAL_MODE default = %q; want %q (default must preserve the existing Shamir path so operators who do NOT opt in see no behavioral change)", got, want)
	}

	// T2. Shamir init/unseal/shard-write path is guarded so it does NOT
	// run when AUTO_UNSEAL_MODE=transit. The guard MUST surround at least
	// the `bao operator init` block. Look for a conditional that compares
	// AUTO_UNSEAL_MODE != "transit" (or equivalent) before the init.
	transitGuardRe := regexp.MustCompile(`if \[\[ "\$AUTO_UNSEAL_MODE" (!=|==) "(shamir|transit)" \]\]`)
	if !transitGuardRe.MatchString(body) {
		t.Error("Shamir path missing AUTO_UNSEAL_MODE guard — when AUTO_UNSEAL_MODE=transit the script MUST skip `bao operator init` and the cleanroom-unseal.json write (blueprint D2 — no shards on disk)")
	}

	// T3. EC-3 — Transit-ready wait. The script MUST probe the Transit
	// backend `sys/health` BEFORE proceeding with main OpenBao init
	// when in transit mode. Accept a curl loop / kubectl wait against
	// the Transit endpoint.
	transitReadyRe := regexp.MustCompile(`(?i)transit[^\n]*(sys/health|kubectl wait|/v1/sys/health)`)
	if !transitReadyRe.MatchString(body) {
		t.Error("EC-3 Transit-ready wait missing — when AUTO_UNSEAL_MODE=transit the script MUST wait for the Transit backend `sys/health` 200 BEFORE main OpenBao init (else main pod CrashLoops auto-unsealing against a not-yet-ready Transit)")
	}

	// T4. EC-7 documented (ephemeral Transit). Two mentions of either
	// `ephemeral` or `seal.migration` MUST appear in the comments so a
	// future operator understands the trade-off.
	ephemeralMentions := strings.Count(strings.ToLower(body), "ephemeral") +
		strings.Count(strings.ToLower(body), "seal-migration") +
		strings.Count(strings.ToLower(body), "seal migration")
	if ephemeralMentions < 2 {
		t.Errorf("EC-7/EC-8 doc lines insufficient — got %d mention(s) of `ephemeral` / `seal-migration`; need ≥ 2 (EC-7: dev-mode Transit loses key on restart → ok because Cleanroom re-creates; EC-8: no seal-migration code — always fresh-init)", ephemeralMentions)
	}

	// T5. Traceability — T2.1 / issue #111 / Jeito A cited so a future
	// operator reading the script understands why the toggle exists.
	if !regexp.MustCompile(`T2\.1|issue #111|Jeito A|auto-unseal`).MatchString(body) {
		t.Error("T2.1 traceability missing — add a comment citing T2.1 / issue #111 / Jeito A so future operator understands why the auto-unseal toggle exists")
	}

	// T6. Helmfile secrets.helmfile.yaml.gotmpl declares a conditional
	// `seal "transit"` stanza for the vault-local env, gated by
	// `.Values.transitAutoUnsealEnabled` (default off in the env file).
	// The opt-in toggle preserves the existing manual-Shamir path for
	// the default vault-local config.
	hf := readOverlayHF(t, "helmfile/secrets.helmfile.yaml.gotmpl")
	if !regexp.MustCompile(`seal\s+"transit"`).MatchString(hf) {
		t.Error("secrets.helmfile.yaml.gotmpl missing `seal \"transit\"` stanza — T2.1 contract: add a conditional auto-unseal block in the vault-local server.standalone.config HCL")
	}
	if !strings.Contains(hf, "transitAutoUnsealEnabled") {
		t.Error("secrets.helmfile.yaml.gotmpl missing `.Values.transitAutoUnsealEnabled` gate — the seal stanza MUST be opt-in (default off) so existing Shamir installs keep working")
	}

	// T7. Helmfile cites T2.1 / issue #111.
	if !regexp.MustCompile(`T2\.1|issue #111|dev-public-cross-cluster-secrets`).MatchString(hf) {
		t.Error("secrets.helmfile.yaml.gotmpl missing T2.1 / issue #111 traceability comment")
	}
}

// readOverlayHF is the same shape as readPath but kept as a local
// helper so the test file remains self-contained.
func readOverlayHF(t *testing.T, rel string) string {
	t.Helper()
	return readPath(t, rel)
}
