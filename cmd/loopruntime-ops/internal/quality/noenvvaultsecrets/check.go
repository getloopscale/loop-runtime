// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package noenvvaultsecrets is the Go home for the no-env-vault-secrets gate
// (originally T2.7 of plan 2026-06-02-openbao-adoption v1.3 — defense in depth
// preventing secrets-as-env-var regression).
//
// It scans Helm templates (charts/*/templates + infra/helm/*/templates) for
// env vars carrying secret values that must live in Vault under D11 ESO
// pattern. Three rule layers:
//
//	FORBIDDEN (FAIL): exact names `GITHUB_APP_PRIVATE_KEY`,
//	  `LOOPRUNTIME_SECRET_ENCRYPTION_KEY`, `COSIGN_PASSWORD` — these are migrated
//	  per T2.2/T2.3/T2.4 to Vault paths; reintroducing as env var = regression.
//
//	PATTERN-WARN (FAIL by default, WARN with --warn-only): regex on env name
//	  ending in `_PRIVATE_KEY`, `_TOKEN`, `_SECRET`, `_PASSWORD` —
//	  heuristic flag. Allowlist exempts legitimate cases (e.g., feature flags
//	  coincidentally matching pattern).
//
//	ALLOWLIST: explicit exception strings; matched env var skips both layers.
//
// All hits include file:line + actionable suggestion ("move to Vault via
// ExternalSecret CR at <suggested path>").
//
// References:
//   - vault-INT plan D11 (ESO supersedes — ExternalSecret distribution pattern)
//   - openbao-adoption v1.3 T2.7 spec
//   - CLAUDE.md "Common Mistakes" entry "creationPolicy: Owner" (iter 20 P0)
package noenvvaultsecrets

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// Config tells Check which repo to scan + allows operator override of allowlist.
type Config struct {
	RepoRoot string
	// Allowlist of env var names exempted from both FORBIDDEN + PATTERN layers.
	// Default includes feature flags + non-secret config that match heuristic patterns.
	Allowlist []string
	// WarnOnly toggles PATTERN matches from FAIL to WARN (FORBIDDEN remain FAIL).
	WarnOnly bool
}

// Outcome is the gate result.
type Outcome struct {
	Pass        bool
	Forbidden   []Hit // exact-name matches (always FAIL)
	PatternHits []Hit // regex matches (FAIL or WARN per WarnOnly)
	Warnings    []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Hit is a single violation location.
type Hit struct {
	File       string
	Line       int
	EnvName    string
	Reason     string
	Suggestion string
}

// scanDirs are the chart trees scanned (relative to RepoRoot).
var scanDirs = []string{"charts", "infra/helm"}

// Forbidden env var names (case-sensitive). Reintroducing these as env vars
// after T2.2/T2.3/T2.4 migration = regression.
var forbiddenNames = []string{
	"GITHUB_APP_PRIVATE_KEY",
	"LOOPRUNTIME_SECRET_ENCRYPTION_KEY",
	"COSIGN_PASSWORD",
}

// Pattern matchers (compiled at package init).
var (
	envNameRegex = regexp.MustCompile(`^\s*-?\s*name:\s*([A-Z_][A-Z0-9_]+)\s*$`)
	// Heuristic: env name ends in well-known secret suffixes.
	secretSuffixRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*?(_PRIVATE_KEY|_TOKEN|_SECRET|_PASSWORD)$`)
)

// DefaultAllowlist lists legitimate env vars matching the heuristic but NOT
// secrets, OR secrets sourced via valueFrom.secretKeyRef (ESO-managed K8s Secret).
// Operator can extend via Config.Allowlist (merged at Check time).
var DefaultAllowlist = []string{
	"GITEA_TOKEN",                                    // legitimate via ESO secretKeyRef (vault-INT gitea-admin)
	"ARGOCD_TOKEN",                                   // legitimate via ESO loopruntime-api-tokens
	"CF_API_TOKEN",                                   // legitimate via ESO loopruntime-api-tokens (iter 20 follow-up)
	"GITHUB_WEBHOOK_SECRET",                          // legitimate via ESO loopruntime-api-github
	"STRIPE_SECRET_KEY",                              // legitimate via ESO loopruntime-api-stripe
	"BAO_TOKEN",                                      // openbao client config — operational token, not app secret
	"PULUMI_CONFIG_PASSPHRASE",                       // pulumi local secret encryption
	"PULUMI_ACCESS_TOKEN",                            // pulumi cloud auth
	"LOOPRUNTIME_ADMIN_API_KEY",                      // legitimate via ESO loopruntime-api-admin
	"LOOPRUNTIME_ADMIN_API_KEY_PREVIOUS",             // legitimate via ESO loopruntime-api-admin
	"LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET",          // legitimate via ESO loopruntime-api-admin
	"LOOPRUNTIME_ADMIN_SERVICE_AUTH_SECRET_PREVIOUS", // legitimate via ESO loopruntime-api-admin
	"LOOPRUNTIME_BOOTSTRAP_SECRET",                   // legitimate via ESO loopruntime-api-internal
	"LOOPRUNTIME_ARGO_CALLBACK_SECRET",               // legitimate via ESO loopruntime-api-internal
	"LOOPRUNTIME_SERVICE_AUTH_NONCE_REDIS_PASSWORD",  // legitimate via ESO loopruntime-service-auth-redis
	"AWS_SECRET_ACCESS_KEY",                          // openbao-snapshot-s3-creds Secret (D12 backup)
	"AWS_ACCESS_KEY_ID",                              // openbao-snapshot-s3-creds Secret (D12 backup)
}

// Check scans for forbidden + pattern-matching env vars and returns Outcome.
// Pass = (len(Forbidden) == 0) AND (WarnOnly OR len(PatternHits) == 0).
func Check(cfg Config) (*Outcome, error) {
	out := &Outcome{}

	allowed := buildAllowSet(cfg.Allowlist)

	for _, d := range scanDirs {
		full := filepath.Join(cfg.RepoRoot, d)
		if _, err := os.Stat(full); err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("scan dir absent: %s", d))
			continue
		}
		err := filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || isExcludedFile(path) {
				return nil
			}
			forb, pat, e := scanFile(cfg.RepoRoot, path, allowed)
			if e != nil {
				return e
			}
			out.Forbidden = append(out.Forbidden, forb...)
			out.PatternHits = append(out.PatternHits, pat...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Pass condition: zero FORBIDDEN AND (WarnOnly OR zero PatternHits).
	out.Pass = len(out.Forbidden) == 0 && (cfg.WarnOnly || len(out.PatternHits) == 0)
	return out, nil
}

func buildAllowSet(extras []string) map[string]struct{} {
	out := make(map[string]struct{}, len(DefaultAllowlist)+len(extras))
	for _, n := range DefaultAllowlist {
		out[n] = struct{}{}
	}
	for _, n := range extras {
		out[strings.TrimSpace(n)] = struct{}{}
	}
	return out
}

// isExcludedFile skips test fixtures + non-template files.
func isExcludedFile(path string) bool {
	if strings.Contains(path, "/tests/") || strings.Contains(path, "_test.") {
		return true
	}
	// Only scan template YAML files (where env: blocks live).
	if !strings.Contains(path, "/templates/") {
		return true
	}
	if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
		return true
	}
	return false
}

func scanFile(repoRoot, absPath string, allowed map[string]struct{}) (forbidden []Hit, pattern []Hit, err error) {
	// #nosec G304 -- absPath comes from filepath.Walk of fixed chart dirs.
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, err
	}
	rel, _ := filepath.Rel(repoRoot, absPath)

	forbSet := make(map[string]struct{})
	for _, n := range forbiddenNames {
		forbSet[n] = struct{}{}
	}

	// Read all lines for look-ahead (detect valueFrom.secretKeyRef pattern).
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	for i, line := range lines {
		lineNo := i + 1
		if isCommentLine(line) {
			continue
		}
		m := envNameRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		envName := m[1]
		if _, ok := allowed[envName]; ok {
			continue
		}
		// CRITICAL: look-ahead for valueFrom.secretKeyRef OR valueFrom.configMapKeyRef
		// within next 3 non-empty non-comment lines. If found, this env var is
		// sourced from K8s Secret (ESO-managed pattern) — SKIP.
		if hasSecretKeyRefAhead(lines, i) {
			continue
		}
		if _, ok := forbSet[envName]; ok {
			forbidden = append(forbidden, Hit{
				File: rel, Line: lineNo, EnvName: envName,
				Reason:     fmt.Sprintf("env var %s is FORBIDDEN (migrated to Vault per openbao-adoption T2.2/T2.3/T2.4)", envName),
				Suggestion: suggestVaultPath(envName),
			})
			continue
		}
		if secretSuffixRegex.MatchString(envName) {
			pattern = append(pattern, Hit{
				File: rel, Line: lineNo, EnvName: envName,
				Reason:     fmt.Sprintf("env var %s matches secret-suffix heuristic; verify it's not a secret OR add to allowlist", envName),
				Suggestion: suggestVaultPath(envName),
			})
		}
	}
	return forbidden, pattern, nil
}

// hasSecretKeyRefAhead returns true if the next ~5 non-empty non-comment lines
// after envIdx contain `valueFrom:` followed by `secretKeyRef:` (or
// `configMapKeyRef:`). This is the ESO-managed pattern we explicitly want.
func hasSecretKeyRefAhead(lines []string, envIdx int) bool {
	maxLookahead := 5
	checked := 0
	sawValueFrom := false
	for i := envIdx + 1; i < len(lines) && checked < maxLookahead; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		checked++
		// If we hit `- name:` (next env entry) without seeing secretKeyRef, stop.
		if strings.HasPrefix(t, "- name:") || envNameRegex.MatchString(lines[i]) {
			return false
		}
		if strings.HasPrefix(t, "valueFrom:") {
			sawValueFrom = true
			continue
		}
		if sawValueFrom && (strings.HasPrefix(t, "secretKeyRef:") || strings.HasPrefix(t, "configMapKeyRef:")) {
			return true
		}
	}
	return false
}

func isCommentLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//")
}

func suggestVaultPath(envName string) string {
	// Map to D6 path convention: secret/loopruntime/<env>/<binary>/<category>/<key>
	key := strings.ToLower(strings.ReplaceAll(envName, "_", "/"))
	return fmt.Sprintf("move to Vault path `secret/loopruntime/<env>/<binary>/secrets/%s` via ExternalSecret CR (see infra/helm/loopruntime-external-secrets/values.yaml inventory)", key)
}

// FormatReport returns a human-readable summary suitable for CLI output.
func (o *Outcome) FormatReport() string {
	var b strings.Builder
	if o.Pass {
		fmt.Fprintf(&b, "no-env-vault-secrets: PASS (forbidden=%d, pattern=%d)\n",
			len(o.Forbidden), len(o.PatternHits))
		return b.String()
	}
	fmt.Fprintf(&b, "no-env-vault-secrets: FAIL\n")
	if len(o.Forbidden) > 0 {
		fmt.Fprintf(&b, "  Forbidden env vars (FAIL):\n")
		sort.Slice(o.Forbidden, func(i, j int) bool {
			return o.Forbidden[i].File < o.Forbidden[j].File
		})
		for _, h := range o.Forbidden {
			fmt.Fprintf(&b, "    %s:%d  env=%s  reason=%s\n      → %s\n",
				h.File, h.Line, h.EnvName, h.Reason, h.Suggestion)
		}
	}
	if len(o.PatternHits) > 0 {
		fmt.Fprintf(&b, "  Pattern matches (heuristic):\n")
		sort.Slice(o.PatternHits, func(i, j int) bool {
			return o.PatternHits[i].File < o.PatternHits[j].File
		})
		for _, h := range o.PatternHits {
			fmt.Fprintf(&b, "    %s:%d  env=%s  reason=%s\n      → %s\n",
				h.File, h.Line, h.EnvName, h.Reason, h.Suggestion)
		}
	}
	if len(o.Warnings) > 0 {
		fmt.Fprintf(&b, "  Warnings:\n")
		for _, w := range o.Warnings {
			fmt.Fprintf(&b, "    %s\n", w)
		}
	}
	return b.String()
}
