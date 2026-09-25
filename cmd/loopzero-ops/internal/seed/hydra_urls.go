// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"fmt"
	"regexp"
)

// HydraURLsSeed is the Go port of
// infra/scripts/local/seed-hydra-urls-canonical.sh.
//
// Patches loopzero-auth/ory-hydra ConfigMap (`hydra.yaml`) so verification/success/
// login/consent/logout/error URLs point at the canonical dev-public hosts:
//
//	urls.self.{issuer,public}      → https://<authHost>/
//	urls.device.{verification,success} → https://<appHost>/device-{verify,success}
//	urls.{login,consent}           → https://<appHost>/auth/{login,consent}
//	urls.logout                    → https://<appHost>/logout
//	urls.error                     → https://<appHost>/auth/error
//
// Idempotent: re-runs are no-op when URLs already canonical.
// Env overrides: LOOPZERO_AUTH_HOST (default auth-dev.loopzero.example),
// LOOPZERO_APP_HOST (default app-dev.loopzero.example), AUTH_NAMESPACE
// (default loopzero-auth).
type HydraURLsSeed struct{}

func NewHydraURLsSeed() *HydraURLsSeed { return &HydraURLsSeed{} }
func (s *HydraURLsSeed) Name() string  { return "hydra-urls" }

const (
	hydraAuthNamespace      = "loopzero-auth"
	hydraConfigMapName      = "ory-hydra"
	hydraConfigMapDataKey   = "hydra.yaml"
	hydraDeploymentName     = "ory-hydra"
	defaultHydraAuthHostVal = "auth-dev.loopzero.example"
	defaultHydraAppHostVal  = "app-dev.loopzero.example"
)

func (s *HydraURLsSeed) Run(d Deps) Result {
	authNS := d.Host.envOr("AUTH_NAMESPACE", hydraAuthNamespace)
	authHost := d.Host.envOr("LOOPZERO_AUTH_HOST", defaultHydraAuthHostVal)
	appHost := d.Host.envOr("LOOPZERO_APP_HOST", defaultHydraAppHostVal)

	cm, err := d.Kube.GetConfigMap(d.Ctx, authNS, hydraConfigMapName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v (run task setup first)", authNS, hydraConfigMapName, err)}
	}
	current := cm.Data[hydraConfigMapDataKey]
	if current == "" {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s data.%s empty", authNS, hydraConfigMapName, hydraConfigMapDataKey)}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would patch ory-hydra ConfigMap + restart"}
	}
	updated := rewriteHydraURLs(current, authHost, appHost)
	if updated == current {
		return Result{Verdict: PASS, Reason: "no-op: ory-hydra URLs already canonical"}
	}
	if err := d.Kube.PatchConfigMapData(d.Ctx, authNS, hydraConfigMapName,
		map[string]string{hydraConfigMapDataKey: updated}); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("patch %s/%s: %v", authNS, hydraConfigMapName, err)}
	}
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, authNS, hydraDeploymentName); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout %s/%s: %v", authNS, hydraDeploymentName, err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("ory-hydra ConfigMap patched (auth=%s app=%s) + rolled out", authHost, appHost)}
}

// Verify probes that the ConfigMap URLs are canonical. Stronger than the
// bash `--verify` (which hit an external URL) — a data probe is deterministic
// and doesn't depend on external DNS being wired.
func (s *HydraURLsSeed) Verify(d Deps) Result {
	authNS := d.Host.envOr("AUTH_NAMESPACE", hydraAuthNamespace)
	authHost := d.Host.envOr("LOOPZERO_AUTH_HOST", defaultHydraAuthHostVal)
	appHost := d.Host.envOr("LOOPZERO_APP_HOST", defaultHydraAppHostVal)
	cm, err := d.Kube.GetConfigMap(d.Ctx, authNS, hydraConfigMapName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v", authNS, hydraConfigMapName, err)}
	}
	current := cm.Data[hydraConfigMapDataKey]
	updated := rewriteHydraURLs(current, authHost, appHost)
	if current != updated {
		return Result{Verdict: FAIL, Reason: "ory-hydra URLs diverge from canonical"}
	}
	return Result{Verdict: PASS, Reason: "ory-hydra URLs canonical"}
}

// URL rewrite patterns (compiled once for perf; parallel to the bash script's
// python3 regex block).
var (
	reHydraIssuer  = regexp.MustCompile(`issuer: http://ory-hydra-public[^\s]*`)
	reHydraPublic  = regexp.MustCompile(`public: http://ory-hydra-public[^\s]*`)
	reHydraVerify  = regexp.MustCompile(`verification: http://localhost:\d+/[\w-]*device-verify`)
	reHydraSuccess = regexp.MustCompile(`success: http://localhost:\d+/[\w-]*device-success`)
	reHydraLogin   = regexp.MustCompile(`login: http://localhost:\d+/[\w/-]*login`)
	reHydraConsent = regexp.MustCompile(`consent: http://localhost:\d+/[\w/-]*consent`)
	reHydraLogout  = regexp.MustCompile(`logout: http://localhost:\d+/[\w/-]*logout`)
	reHydraError   = regexp.MustCompile(`error: http://localhost:\d+/[\w/-]*error`)
)

// rewriteHydraURLs is the pure function that mirrors the bash python3 block
// canonical rewrite. Pulled out so it is testable byte-for-byte against fixture
// yamls without a KubeClient.
func rewriteHydraURLs(in, authHost, appHost string) string {
	out := in
	out = reHydraIssuer.ReplaceAllString(out, "issuer: https://"+authHost+"/")
	out = reHydraPublic.ReplaceAllString(out, "public: https://"+authHost+"/")
	out = reHydraVerify.ReplaceAllString(out, "verification: https://"+appHost+"/device-verify")
	out = reHydraSuccess.ReplaceAllString(out, "success: https://"+appHost+"/device-success")
	out = reHydraLogin.ReplaceAllString(out, "login: https://"+appHost+"/auth/login")
	out = reHydraConsent.ReplaceAllString(out, "consent: https://"+appHost+"/auth/consent")
	out = reHydraLogout.ReplaceAllString(out, "logout: https://"+appHost+"/logout")
	out = reHydraError.ReplaceAllString(out, "error: https://"+appHost+"/auth/error")
	return out
}
