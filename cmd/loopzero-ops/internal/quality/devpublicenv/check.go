// Package devpublicenv is a hermetic structural test for
// infra/helmfile/environments/dev-public.yaml.
//
// Validates the file has the canonical shape: dev URLs (-dev.loopzero.example),
// shared cookie domain (loopzero.example), CORS + allowedReturnUrls cover both
// -dev hosts, no localhost URLs, no legacy non-dev URLs, D5 OAuth reuse
// pattern (no _DEV env var override).
//
// Migrated from infra/tests/helm/dev-public-env_test.sh (M-Migrate2
// continuation 2026-07-07). Hermetic — pure text walk on YAML.
package devpublicenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const envPath = "infra/helmfile/environments/dev-public.yaml"

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Failures []string
}

// ErrViolations means at least one assertion failed.
var ErrViolations = errors.New("dev-public.yaml canonical shape violated")

var (
	localhostRE    = regexp.MustCompile(`localhost:4455|http://localhost|127\.0\.0\.1`)
	legacyURLRE    = regexp.MustCompile(`https://(auth|app|cloud)\.loopzero\.dev`)
	devSuffixEnvRE = regexp.MustCompile(`LOOPZERO_(GITHUB|GOOGLE)_CLIENT_(ID|SECRET)_DEV`)
)

// Check runs the 14 assertions from the bash guard.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, envPath)
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("dev-public.yaml missing at %s: %v", envPath, err)}}, ErrViolations
	}
	text := string(body)

	var failures []string
	assertContains := func(desc, needle string) {
		if !strings.Contains(text, needle) {
			failures = append(failures, fmt.Sprintf("MISSING: %s (needle `%s`)", desc, needle))
		}
	}

	// Core public URLs.
	//
	// Um painel, um nome. `app-dev` e a superficie canonica desde 2026-06-22, mas o
	// ambiente seguia declarando `cloud-dev` como dashboardOrigin e publicCloudUrl —
	// dois nomes para a MESMA coisa, que so funcionava porque o `cloud-dev` respondia
	// 301 para o `app-dev`. E o mesmo defeito que motivou a renomeacao dos clusters e a
	// unificacao das cells, agora na camada de identidade: CORS e URL de retorno
	// autorizando um host que existe apenas para redirecionar.
	//
	// O host `cloud-dev` foi removido em 2026-08-10. Antes de remove-lo foi verificado
	// que as duas metades dele estavam cobertas: o webhook do GitHub ja e registrado
	// contra `app-dev` (cmd/loopzero-ops/internal/seed/github_webhook_fixture.go:44) e o
	// `app-dev` ja tem o mesmo desvio direto para o loopzero-api, entao nada dependia dele.
	assertContains("publicAuthUrl points to auth-dev", `publicAuthUrl: "https://auth-dev.loopzero.example"`)
	assertContains("publicCloudUrl points to app-dev", `publicCloudUrl: "https://app-dev.loopzero.example"`)
	assertContains("uiBaseUrl points to app-dev", `uiBaseUrl: "https://app-dev.loopzero.example"`)
	assertContains("dashboardOrigin points to app-dev", `dashboardOrigin: "https://app-dev.loopzero.example"`)
	assertContains("cookieDomain is eTLD+1", `cookieDomain: "loopzero.example"`)

	// CORS + allowedReturnUrls
	assertContains("corsAllowedOrigins includes app-dev", `  - "https://app-dev.loopzero.example"`)
	assertContains("allowedReturnUrls includes app-dev trailing slash", `  - "https://app-dev.loopzero.example/"`)

	// E o inverso, que e o que impede o host de voltar por inercia: autorizar em CORS ou
	// em URL de retorno um host que nao existe mais e superficie de auth concedida a
	// ninguem.
	if strings.Contains(text, "cloud-dev.loopzero.example") {
		failures = append(failures, "PRESENT: cloud-dev.loopzero.example — o host foi removido em 2026-08-10; "+
			"mante-lo em CORS/allowedReturnUrls autoriza uma origem que nao responde")
	}

	// No localhost leak
	if localhostRE.MatchString(text) {
		failures = append(failures, "FORBIDDEN: dev-public.yaml contains localhost URL(s) (regression risk)")
	}

	// No legacy non-dev URL leak
	if legacyURLRE.MatchString(text) {
		failures = append(failures, "FORBIDDEN: dev-public.yaml contains legacy *.loopzero.example URL(s) — expected -dev suffix everywhere")
	}

	// D5: no _DEV env var override
	if devSuffixEnvRE.MatchString(text) {
		failures = append(failures, "FORBIDDEN: dev-public.yaml references _DEV-suffix env vars — per D5 (2026-06-04), OAuth apps are reused")
	}

	// Stripe webhook per-env split IS documented (D5 caveat).
	assertContains("Stripe webhook split per-env documented", "loopzero-stripe-webhook-dev")

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}
