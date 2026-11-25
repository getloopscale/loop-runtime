// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// BrokerSeed is the Go port of infra/scripts/local/seed-broker-canonical.sh.
//
// It seeds the secret material of the ESO secret-fetch broker, one token per cell
// (#456): the broker binds a request to the cell whose token it presented, so each
// cell's External Secrets must hold its own.
//
//  1. Read the cells from the core's ClusterCredentials; reuse the tokens already
//     issued and generate the rest. Apply the core loopzero-system/loopzero-broker-token
//     Secret with LOOPZERO_BROKER_TOKENS (every cell=token pair) and LOOPZERO_BROKER_TOKEN /
//     `token` (the default cell's, the single-cell form).
//  2. Write each cell's external-secrets/loopzero-broker-token Secret with that cell's
//     token and the `external-secrets.io/type=webhook` label.
//  3. Rollout restart core loopzero-api so envFrom picks up the tokens.
//
// The broker's ClusterSecretStore is the cell helmfile's release `loopzero-broker`
// (infra/helmfile/cell.helmfile.yaml.gotmpl), which reads its address from the
// environment; this seed writes only the tokens (principle_violations#67).
type BrokerSeed struct{}

func NewBrokerSeed() *BrokerSeed   { return &BrokerSeed{} }
func (s *BrokerSeed) Name() string { return "broker" }

const (
	brokerESONamespace     = "external-secrets"
	brokerSecretName       = "loopzero-broker-token"
	brokerTokenKey         = "token"
	brokerCanonicalEnvKey  = "LOOPZERO_BROKER_TOKEN"
	brokerCellTokensEnvKey = "LOOPZERO_BROKER_TOKENS"
	brokerDefaultCell      = "cell-0" // domain.DefaultCellID; loopzero-ops does not import pkg/domain
)

// clusterCredentialGVK is the core's record of a cell (operators/api/v1). Read as
// unstructured: loopzero-ops does not import the operators module.
var clusterCredentialGVK = schema.GroupVersionKind{Group: "loopzero.io", Version: "v1", Kind: "ClusterCredential"}

// brokerCellContext is the kube context of a cell's k3d cluster: infra/k3d names the
// cluster loopzero-<cellID>, and k3d prefixes its contexts with k3d-. It is the default
// of Deps.CellContextPrefix.
func brokerCellContext(cellID string) string { return "k3d-loopzero-" + cellID }

func (s *BrokerSeed) Run(d Deps) Result {
	cells, err := brokerCells(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	tokens, generated, err := brokerAcquireCellTokens(d, cells)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 tokens: %v", err)}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would seed broker tokens for %d cell(s) (%d new) and restart loopzero-api", len(cells), generated)}
	}
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx, brokerMgmtSecret(tokens, cells)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 apply core Secret: %v", err)}
	}
	for _, cell := range cells {
		cellCli, err := d.ClientFor(d.cellContext(cell))
		if err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 resolve %s: %v", cell, err)}
		}
		if err := cellCli.EnsureNamespace(d.Ctx, brokerESONamespace); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 %s ensure ns: %v", cell, err)}
		}
		if _, err := cellCli.CreateOrUpdateSecret(d.Ctx, brokerWorkloadSecret([]byte(tokens[cell]))); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 %s apply Secret: %v", cell, err)}
		}
	}
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, "loopzero-system", "loopzero-api"); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 rollout loopzero-api: %v", err)}
	}
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, "loopzero-system", "loopzero-api", d.settleTimeout(loopzeroAPISettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 loopzero-api did not settle: %v", err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("broker tokens seeded on core and %d cell(s): %s (%d new)", len(cells), strings.Join(cells, ", "), generated)}
}

// Verify checks that loopzero-api holds a token for every cell and that each cell's
// External Secrets holds exactly that token.
func (s *BrokerSeed) Verify(d Deps) Result {
	sec, err := d.Kube.GetSecret(d.Ctx, "loopzero-system", brokerSecretName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get core Secret: %v", err)}
	}
	if len(sec.Data[brokerCanonicalEnvKey]) == 0 && len(sec.Data[brokerCellTokensEnvKey]) == 0 {
		return Result{Verdict: FAIL, Reason: "core Secret carries neither LOOPZERO_BROKER_TOKEN nor LOOPZERO_BROKER_TOKENS"}
	}
	cells, err := brokerCells(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	tokens := parseBrokerCellTokens(string(sec.Data[brokerCellTokensEnvKey]))
	for _, cell := range cells {
		want := tokens[cell]
		if want == "" {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("core Secret has no broker token for %s", cell)}
		}
		cellCli, err := d.ClientFor(d.cellContext(cell))
		if err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("resolve %s: %v", cell, err)}
		}
		wsec, err := cellCli.GetSecret(d.Ctx, brokerESONamespace, brokerSecretName)
		if err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s Secret: %v", cell, err)}
		}
		if wsec.Labels["external-secrets.io/type"] != "webhook" {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s Secret missing canonical `external-secrets.io/type=webhook` label", cell)}
		}
		if subtle.ConstantTimeCompare(wsec.Data[brokerTokenKey], []byte(want)) != 1 {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s holds a broker token loopzero-api does not issue to it", cell)}
		}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("core and %d cell broker Secrets agree", len(cells))}
}

// brokerCells lists the cells the core knows, from its ClusterCredentials (spec.cellID),
// sorted and without duplicates — two credentials may name one cell. None is an error: a
// token written nowhere reports success for a broker no cell can reach.
func brokerCells(d Deps) ([]string, error) {
	if d.CtrlClient == nil {
		return nil, fmt.Errorf("no core client to list ClusterCredentials")
	}
	list := &unstructured.UnstructuredList{}
	listGVK := clusterCredentialGVK
	listGVK.Kind += "List"
	list.SetGroupVersionKind(listGVK)
	if err := d.CtrlClient.List(d.Ctx, list); err != nil {
		return nil, fmt.Errorf("list ClusterCredentials: %w", err)
	}
	seen := map[string]bool{}
	var cells []string
	for _, item := range list.Items {
		cell, _, _ := unstructured.NestedString(item.Object, "spec", "cellID")
		if cell != "" && !seen[cell] {
			seen[cell] = true
			cells = append(cells, cell)
		}
	}
	if len(cells) == 0 {
		return nil, fmt.Errorf("no ClusterCredential names a cell; the broker would serve none")
	}
	sort.Strings(cells)
	return cells, nil
}

// brokerAcquireCellTokens returns a token per cell, reusing every token already issued:
// LOOPZERO_BROKER_TOKENS, and the single token the seed wrote before tokens were per cell,
// which stays the default cell's. Only a missing Secret or a missing entry generates; any
// other read error is returned, because regenerating on it would silently rotate a token
// on a transient API failure.
func brokerAcquireCellTokens(d Deps, cells []string) (map[string]string, int, error) {
	existing := map[string]string{}
	sec, err := d.Kube.GetSecret(d.Ctx, "loopzero-system", brokerSecretName)
	switch {
	case err == nil:
		existing = parseBrokerCellTokens(string(sec.Data[brokerCellTokensEnvKey]))
		if legacy := string(sec.Data[brokerTokenKey]); legacy != "" && existing[brokerDefaultCell] == "" {
			existing[brokerDefaultCell] = legacy
		}
	case !apierrors.IsNotFound(err):
		return nil, 0, fmt.Errorf("read broker tokens: %w", err)
	}
	tokens := map[string]string{}
	generated := 0
	for _, cell := range cells {
		if tok := existing[cell]; tok != "" {
			tokens[cell] = tok
			continue
		}
		buf := make([]byte, 32) // 64 hex chars, as `openssl rand -hex 32`
		if _, err := rand.Read(buf); err != nil {
			return nil, 0, err
		}
		tokens[cell] = hex.EncodeToString(buf)
		generated++
	}
	return tokens, generated, nil
}

// parseBrokerCellTokens reads cell=token pairs; loopzero-api validates the same format.
func parseBrokerCellTokens(raw string) map[string]string {
	out := map[string]string{}
	for _, entry := range strings.Split(raw, ",") {
		if cell, token, ok := strings.Cut(strings.TrimSpace(entry), "="); ok && cell != "" && token != "" {
			out[cell] = token
		}
	}
	return out
}

// brokerMgmtSecret builds the core loopzero-system/loopzero-broker-token Secret loopzero-api reads
// through envFrom: LOOPZERO_BROKER_TOKENS lists every cell=token pair, and LOOPZERO_BROKER_TOKEN
// (with its legacy `token` alias) is the default cell's, the single-cell form.
func brokerMgmtSecret(tokens map[string]string, cells []string) *corev1.Secret {
	pairs := make([]string, 0, len(cells))
	for _, cell := range cells {
		pairs = append(pairs, cell+"="+tokens[cell])
	}
	data := map[string][]byte{brokerCellTokensEnvKey: []byte(strings.Join(pairs, ","))}
	if tok := tokens[brokerDefaultCell]; tok != "" {
		data[brokerTokenKey] = []byte(tok)
		data[brokerCanonicalEnvKey] = []byte(tok)
	}
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
		Data: data,
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
