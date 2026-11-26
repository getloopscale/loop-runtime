// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ClusterKubeconfigsSeed writes the peer-cluster kubeconfigs loopruntime-api mounts
// from loopruntime-system/loopruntime-cluster-kubeconfigs (charts/loopruntime-api: build and cell-0;
// the core key is its own cluster, reached in-cluster, and is left alone).
//
// It is the one minter for those keys. Three writers used to exist, each with
// its own credential model:
//
//   - k3d-cross-cluster-heal.sh H2 minted kube-system/default tokens for 24h on
//     every cluster, with TLS verification off. On the build cluster that is
//     the identity Sprint 7 (#36) moved loopruntime-api away from — loopruntime-api watches
//     only the argo namespace because its Role is scoped there — and 24h is the
//     lifetime #242 measured expiring between re-seeds and breaking every
//     deploy. Every `task k3d:heal` reverted both fixes.
//   - regen_build_runtime_kubeconfigs.py minted the scoped identity for 720h
//     with the cluster CA: the model LIVE-verified on the dev-public droplet.
//   - provision-api-tokens.sh patched the cell-0 key with admin client
//     certificates.
//
// The model kept is the verified one: the scoped build identity, 720h, and
// the CA read live from the peer's kube-root-ca.crt ConfigMap rather than a
// snapshot (CLAUDE.md "Live vs snapshot credentials").
type ClusterKubeconfigsSeed struct{}

// NewClusterKubeconfigsSeed returns the seed instance for the registry.
func NewClusterKubeconfigsSeed() *ClusterKubeconfigsSeed { return &ClusterKubeconfigsSeed{} }

// Name is the CLI token: `loopruntime-ops seed cluster-kubeconfigs`.
func (s *ClusterKubeconfigsSeed) Name() string { return "cluster-kubeconfigs" }

const (
	clusterKubeconfigsNamespace = "loopruntime-system"
	clusterKubeconfigsName      = "loopruntime-cluster-kubeconfigs"
	// clusterKubeconfigTokenTTL is long enough to outlive the interval between
	// re-seeds on an always-on environment (#242: 24h did not).
	clusterKubeconfigTokenTTL = 720 * time.Hour
	k3dNetwork                = "k3d-loopruntime"
	inClusterPlaceholder      = "kubernetes.default.svc"
)

// peerKubeconfig is one peer cluster loopruntime-api reaches through the Secret.
type peerKubeconfig struct {
	key         string // data key in loopruntime-cluster-kubeconfigs
	context     string // kubeconfig context of the peer cluster
	container   string // k3d server container; its k3d-loopruntime IP is what pods reach
	saNamespace string // the identity loopruntime-api acts as on the peer
	saName      string
}

var peerKubeconfigs = []peerKubeconfig{
	{key: "build.kubeconfig", context: "k3d-loopruntime-build", container: "k3d-loopruntime-build-server-0",
		saNamespace: "argo", saName: "loopruntime-workflow-submitter"},
	{key: "cell-0.kubeconfig", context: "k3d-loopruntime-cell-0", container: "k3d-loopruntime-cell-0-server-0",
		saNamespace: "kube-system", saName: "default"},
}

func (s *ClusterKubeconfigsSeed) Run(d Deps) Result {
	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would mint %s-token kubeconfigs for %s into %s/%s",
			clusterKubeconfigTokenTTL, peerNames(), clusterKubeconfigsNamespace, clusterKubeconfigsName)}
	}
	// Every peer is rendered before anything is written, so a peer that
	// cannot be reached leaves the Secret as it was rather than half-rotated.
	data := map[string][]byte{}
	for _, p := range peerKubeconfigs {
		kc, err := s.render(&d, p)
		if err != nil {
			return Result{Verdict: FAIL, Reason: err.Error()}
		}
		data[p.key] = kc
	}
	if err := d.Kube.EnsureNamespace(d.Ctx, clusterKubeconfigsNamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("ensure namespace %s: %v", clusterKubeconfigsNamespace, err)}
	}
	if err := d.Kube.MergeSecretData(d.Ctx, clusterKubeconfigsNamespace, clusterKubeconfigsName, data); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("write %s/%s: %v", clusterKubeconfigsNamespace, clusterKubeconfigsName, err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("wrote %s-token kubeconfigs for %s", clusterKubeconfigTokenTTL, peerNames())}
}

// render mints the peer's token and builds its kubeconfig.
func (s *ClusterKubeconfigsSeed) render(d *Deps, p peerKubeconfig) ([]byte, error) {
	ip, err := d.Host.dockerNetworkIP(k3dNetwork, p.container)
	if err != nil {
		return nil, fmt.Errorf("%s: address of %s on %s: %w", p.key, p.container, k3dNetwork, err)
	}
	peer, err := d.ClientFor(p.context)
	if err != nil {
		return nil, fmt.Errorf("%s: client for %s: %w", p.key, p.context, err)
	}
	ca, err := peer.GetConfigMap(d.Ctx, "default", "kube-root-ca.crt")
	if err != nil {
		return nil, fmt.Errorf("%s: read the live CA of %s: %w", p.key, p.context, err)
	}
	caPEM := ca.Data["ca.crt"]
	if caPEM == "" {
		return nil, fmt.Errorf("%s: %s default/kube-root-ca.crt has no ca.crt", p.key, p.context)
	}
	token, err := peer.MintServiceAccountToken(d.Ctx, p.saNamespace, p.saName, clusterKubeconfigTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.key, err)
	}
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["in-cluster"] = &clientcmdapi.Cluster{
		Server:                   "https://" + ip + ":6443",
		CertificateAuthorityData: []byte(caPEM),
	}
	cfg.AuthInfos["in-cluster"] = &clientcmdapi.AuthInfo{Token: token}
	cfg.Contexts["in-cluster"] = &clientcmdapi.Context{Cluster: "in-cluster", AuthInfo: "in-cluster"}
	cfg.CurrentContext = "in-cluster"
	out, err := clientcmd.Write(*cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: serialise kubeconfig: %w", p.key, err)
	}
	return out, nil
}

// Verify checks that each peer key holds a token kubeconfig aimed at a real
// peer — not the in-cluster placeholder generate-secrets.sh installs on a fresh
// bootstrap, which silently points loopruntime-api at the core cluster.
func (s *ClusterKubeconfigsSeed) Verify(d Deps) Result {
	sec, err := d.Kube.GetSecret(d.Ctx, clusterKubeconfigsNamespace, clusterKubeconfigsName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v", clusterKubeconfigsNamespace, clusterKubeconfigsName, err)}
	}
	for _, p := range peerKubeconfigs {
		cfg, err := clientcmd.Load(sec.Data[p.key])
		if err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s is not a kubeconfig: %v", p.key, err)}
		}
		kctx := cfg.Contexts[cfg.CurrentContext]
		if kctx == nil || cfg.Clusters[kctx.Cluster] == nil || cfg.AuthInfos[kctx.AuthInfo] == nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s has no usable current context", p.key)}
		}
		cluster, user := cfg.Clusters[kctx.Cluster], cfg.AuthInfos[kctx.AuthInfo]
		if strings.Contains(cluster.Server, inClusterPlaceholder) {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s still points at %s — run `loopruntime-ops seed %s`", p.key, cluster.Server, s.Name())}
		}
		if user.Token == "" || len(cluster.CertificateAuthorityData) == 0 {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s lacks a token or the cluster CA", p.key)}
		}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s carry token kubeconfigs for their peer clusters", peerNames())}
}

func peerNames() string {
	keys := make([]string, 0, len(peerKubeconfigs))
	for _, p := range peerKubeconfigs {
		keys = append(keys, p.key)
	}
	return strings.Join(keys, ", ")
}
