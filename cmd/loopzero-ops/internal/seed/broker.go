// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BrokerSeed is the Go port of infra/scripts/local/seed-broker-canonical.sh.
//
// It seeds the secret material of the ESO secret-fetch broker:
//
//  1. Generate/reuse the bearer token; apply mgmt loopzero-system/loopzero-broker-token
//     Secret with keys {token, LOOPZERO_BROKER_TOKEN}.
//  2. Mirror it to the workload cluster's external-secrets ns with the
//     `external-secrets.io/type=webhook` canonical label.
//  3. Rollout restart mgmt loopzero-api so envFrom picks up LOOPZERO_BROKER_TOKEN.
//
// The broker's ClusterSecretStore is the cell helmfile's release `loopzero-broker`
// (infra/helmfile/cell.helmfile.yaml.gotmpl). This seed used to install it with its
// own `helm upgrade --install`, out of Helmfile's sight and with the core server-0
// container IP as the API address (principle_violations#67); the release now reads
// its address from the environment and this seed writes only the token.
type BrokerSeed struct{}

func NewBrokerSeed() *BrokerSeed   { return &BrokerSeed{} }
func (s *BrokerSeed) Name() string { return "broker" }

const (
	brokerContextWorkload = "k3d-loopzero-cell-0"
	brokerESONamespace    = "external-secrets"
	brokerSecretName      = "loopzero-broker-token"
	brokerTokenKey        = "token"
	brokerCanonicalEnvKey = "LOOPZERO_BROKER_TOKEN"
)

func (s *BrokerSeed) Run(d Deps) Result {
	// Layer 1: get-or-generate canonical token.
	token, reused, err := brokerAcquireToken(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 token: %v", err)}
	}
	tokenB := []byte(token)

	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would %s token + apply Secrets + restart loopzero-api", tokenAction(reused))}
	}

	// Layer 1 apply — mgmt Secret with both `token` (legacy) + canonical env key.
	mgmtSec := brokerMgmtSecret(tokenB)
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx, mgmtSec); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 apply mgmt Secret: %v", err)}
	}

	// Layer 2: workload cluster mirror with canonical webhook label.
	workloadCli, err := d.ClientFor(brokerContextWorkload)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 resolve workload: %v", err)}
	}
	if err := workloadCli.EnsureNamespace(d.Ctx, brokerESONamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 ensure ns: %v", err)}
	}
	workloadSec := brokerWorkloadSecret(tokenB)
	if _, err := workloadCli.CreateOrUpdateSecret(d.Ctx, workloadSec); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 apply workload Secret: %v", err)}
	}

	// Layer 3: rollout mgmt loopzero-api.
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, "loopzero-system", "loopzero-api"); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 rollout loopzero-api: %v", err)}
	}
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, "loopzero-system", "loopzero-api", d.settleTimeout(loopzeroAPISettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 loopzero-api did not settle: %v", err)}
	}

	return Result{Verdict: PASS, Reason: fmt.Sprintf("broker token seeded on core and cell (%s)", tokenAction(reused))}
}

// Verify probes: (a) mgmt Secret has canonical env key non-empty,
// (b) workload Secret has canonical label + token, (c) CSS Ready optional.
func (s *BrokerSeed) Verify(d Deps) Result {
	sec, err := d.Kube.GetSecret(d.Ctx, "loopzero-system", brokerSecretName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get mgmt Secret: %v", err)}
	}
	if len(sec.Data[brokerCanonicalEnvKey]) == 0 {
		return Result{Verdict: FAIL, Reason: "mgmt Secret missing LOOPZERO_BROKER_TOKEN"}
	}
	workloadCli, err := d.ClientFor(brokerContextWorkload)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	wsec, err := workloadCli.GetSecret(d.Ctx, brokerESONamespace, brokerSecretName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get workload Secret: %v", err)}
	}
	if wsec.Labels["external-secrets.io/type"] != "webhook" {
		return Result{Verdict: FAIL, Reason: "workload Secret missing canonical `external-secrets.io/type=webhook` label"}
	}
	if len(wsec.Data[brokerTokenKey]) == 0 {
		return Result{Verdict: FAIL, Reason: "workload Secret token key empty"}
	}
	return Result{Verdict: PASS, Reason: "mgmt + workload broker Secrets canonical"}
}

// brokerAcquireToken returns the canonical token — reusing the existing one
// unless the Secret is absent or carries no token. Any other read error is
// returned: regenerating on it would silently rotate the broker token on a
// transient API failure.
func brokerAcquireToken(d Deps) (token string, reused bool, err error) {
	sec, err := d.Kube.GetSecret(d.Ctx, "loopzero-system", brokerSecretName)
	switch {
	case err == nil:
		if v := string(sec.Data[brokerTokenKey]); v != "" {
			return v, true, nil
		}
	case !apierrors.IsNotFound(err):
		return "", false, fmt.Errorf("read broker token: %w", err)
	}
	// Generate new (32 bytes hex = 64 chars — matches `openssl rand -hex 32`).
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false, err
	}
	return hex.EncodeToString(buf), false, nil
}

func tokenAction(reused bool) string {
	if reused {
		return "reused"
	}
	return "generated"
}

// brokerMgmtSecret builds the mgmt/loopzero-system/loopzero-broker-token Secret with
// both `token` (legacy alias) and `LOOPZERO_BROKER_TOKEN` (canonical envFrom key)
// keys. Labels record the seed as manager (managed-by renamed to Go form).
func brokerMgmtSecret(token []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "loopzero-system",
			Name:      brokerSecretName,
			Labels: map[string]string{
				"app.kubernetes.io/name":       brokerSecretName,
				"app.kubernetes.io/managed-by": "loopzero-ops-seed-broker",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			brokerTokenKey:        token,
			brokerCanonicalEnvKey: token,
		},
	}
}

// brokerWorkloadSecret builds the workload/external-secrets/loopzero-broker-token
// Secret mirror with the canonical webhook label.
func brokerWorkloadSecret(token []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: brokerESONamespace,
			Name:      brokerSecretName,
			Labels: map[string]string{
				"external-secrets.io/type":     "webhook",
				"app.kubernetes.io/name":       brokerSecretName,
				"app.kubernetes.io/managed-by": "loopzero-ops-seed-broker",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{brokerTokenKey: token},
	}
}
