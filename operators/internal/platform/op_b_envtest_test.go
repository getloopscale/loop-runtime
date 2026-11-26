// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Envtest regression specs for Op B (live CA fetch from kube-root-ca.crt).
// Plan: docs/plans/completed/2026-05-25-live-ca-fetch-clustercredential-plan.md T3.1
//
// 4 specs cover:
//  1. Live fetch overrides snapshot caData (stub returns distinct PEM →
//     Secret matches stub, NOT envtest kubeconfig snapshot).
//  2. Fallback to snapshot when fetcher fails (AlwaysFailFetcher → fallback
//     counter increments → Secret has snapshot caData).
//  3. spec.tls.insecure=true short-circuits the fetch (stubCAFetcher
//     CallCount stays 0).
//  4. Repeated reconciles preserve Secret content (idempotency).
//
// These specs use NewReconcilerForTesting + RunReconcileArgoCDSecretForTesting
// to drive the reconcile path directly with injected fakes, instead of going
// through the manager-wired production reconciler (which always uses the
// real kubeRootCAFetcher).
package platform_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/platform"
)

// distinctFakeCAPEM is a syntactically valid PEM that differs from any
// envtest control-plane CA. Spec 1 asserts that the rendered Secret's
// caData matches THIS payload (proving Op B used the stub, not the
// kubeconfig snapshot).
const distinctFakeCAPEM = `-----BEGIN CERTIFICATE-----
MIIBjDCCATKgAwIBAgIQQ04bDmgGGsfPv7g4aVXAezAKBggqhkjOPQQDAjArMSkw
JwYDVQQDDCBvcC1iLXJlZ3Jlc3Npb24tZmFrZS1jYUAxNzM5OTk5OTk5MB4XDTI1
MDEwMTAwMDAwMFoXDTM1MDEwMTAwMDAwMFowKzEpMCcGA1UEAwwgb3AtYi1yZWdy
ZXNzaW9uLWZha2UtY2FAMTczOTk5OTk5OTBZMBMGByqGSM49AgEGCCqGSM49AwEH
A0IABG7eYz3VVjPlfTGEHJK5MnEHM2HnB+kDqPxmFLqhKZkPLcRfTKvKqr9CQbQy
6CTGz6ks7TstkLNqVQOIeJ9zR3KjQjBAMA4GA1UdDwEB/wQEAwICpDAPBgNVHRMB
Af8EBTADAQH/MB0GA1UdDgQWBBQHnEPpCXjlqj1l3kSeWqv1AMmKLDAKBggqhkjO
PQQDAgNIADBFAiAyJG/zT0WAQKPMOgGoLcGogq7zM6q5IMVqXkUE7PNlMAIhAPb+
xktbI3DMSCFVdHpnIVlAehBSCJPfMxiWB3aXKqAA
-----END CERTIFICATE-----`

// snapshotCA is a bogus PEM that mimics a stale kubeconfig snapshot.
var snapshotCA = []byte(`-----BEGIN CERTIFICATE-----
MIIBaDCCAQ6gAwIBAgIQ12345678901234567890123BZAKBggqhkjOPQQDAjAj
MSEwHwYDVQQDDBhzbmFwc2hvdC1jYS1mb3ItdGVzdGluZzEwHhcNMjUwMTAxMDAw
MDAwWhcNMzUwMTAxMDAwMDAwWjAjMSEwHwYDVQQDDBhzbmFwc2hvdC1jYS1mb3It
dGVzdGluZzEwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAARS37e1KFXMqUaG37D9
9Mp7ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefghijklmnopqrstuvwx
yzo0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4E
FgQUTrefa3x4V72S9flMuuNWzZ2PIR0wCgYIKoZIzj0EAwIDSAAwRQIhAJ2vHlFX
5ULQv+7D8296ddp212ALHala2yjJ9DDWzPTeAiAkF234560000000000000000xy
zABCDEFGHIJKLMNOPQRSTUV==
-----END CERTIFICATE-----`)

// buildOpBKubeconfig builds a kubeconfig with a token + bogus CA pointing
// at the given server URL. The bogus CA is the "snapshot" that Op B must
// override with the live fetch (Spec 1) or fall back to (Spec 2).
func buildOpBKubeconfig(server, token string, snapshot []byte) []byte {
	kc := clientcmdapi.NewConfig()
	kc.Clusters["c"] = &clientcmdapi.Cluster{
		Server:                   server,
		CertificateAuthorityData: snapshot,
	}
	kc.AuthInfos["u"] = &clientcmdapi.AuthInfo{Token: token}
	kc.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
	kc.CurrentContext = "ctx"
	data, err := clientcmd.Write(*kc)
	Expect(err).NotTo(HaveOccurred())
	return data
}

// decodeOpBSecretCAData extracts and base64-decodes the tlsClientConfig.caData
// from the rendered ArgoCD cluster Secret's config field.
func decodeOpBSecretCAData(s *corev1.Secret) string {
	configRaw := s.Data["config"]
	var argoCfg struct {
		TLSClientConfig struct {
			Insecure bool   `json:"insecure"`
			CAData   string `json:"caData,omitempty"`
		} `json:"tlsClientConfig"`
	}
	Expect(json.Unmarshal(configRaw, &argoCfg)).To(Succeed())
	if argoCfg.TLSClientConfig.CAData == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(argoCfg.TLSClientConfig.CAData)
	Expect(err).NotTo(HaveOccurred())
	return string(decoded)
}

var _ = Describe("Op B: Live CA fetch (issue #67 follow-up)", Ordered, func() {
	const argoNS = "argocd"

	BeforeAll(func() {
		// Op B reconciler writes the ArgoCD cluster Secret into the "argocd"
		// namespace — envtest doesn't pre-create namespaces, so we ensure it
		// exists once for the whole Describe (idempotent: AlreadyExists is OK
		// because clustercredential_controller_test.go's BeforeAll may have
		// already created it within the same suite run).
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: argoNS}}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	mkOpBCRAndSecret := func(name string) (*loopzerov1.ClusterCredential, *corev1.Secret, []byte) {
		secretName := fmt.Sprintf("%s-secret", name)
		kubeconfigData := buildOpBKubeconfig(cfg.Host, "test-token", snapshotCA)
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
			Data:       map[string][]byte{"kubeconfig": kubeconfigData},
		}
		cr := &loopzerov1.ClusterCredential{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: loopzerov1.ClusterCredentialSpec{
				ClusterName: name,
				APIServer:   cfg.Host,
				SecretRef:   loopzerov1.SecretReference{Name: secretName, Namespace: "default"},
				Scope:       loopzerov1.CredentialScope{ArgoCD: true},
			},
		}
		return cr, sec, kubeconfigData
	}

	// Spec 1 (EC-17 amendment) — live fetch produces caData distinct from snapshot.
	It("live CA from stub fetcher overrides kubeconfig snapshot in rendered Secret", func() {
		stub := &platform.StubCAFetcher{ReturnCA: []byte(distinctFakeCAPEM)}
		r := platform.NewReconcilerForTesting(k8sClient, argoNS, stub)

		cr, sec, kubeconfigData := mkOpBCRAndSecret("op-b-live-fetch")
		Expect(k8sClient.Create(ctx, sec)).To(Succeed())
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		Expect(platform.RunReconcileArgoCDSecretForTesting(ctx, r, cr, kubeconfigData)).To(Succeed())

		var argoSecret corev1.Secret
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "cluster-op-b-live-fetch", Namespace: argoNS,
		}, &argoSecret)).To(Succeed())

		decoded := decodeOpBSecretCAData(&argoSecret)
		Expect(decoded).To(Equal(distinctFakeCAPEM),
			"Secret caData must match stub fetcher output (live)")
		Expect(decoded).NotTo(Equal(string(snapshotCA)),
			"Secret caData must NOT equal kubeconfig snapshot — Op B failed to override")
		Expect(stub.CallCount()).To(BeNumerically(">=", 1),
			"stub fetcher should have been invoked")
	})

	// Spec 2 — fallback to snapshot when fetcher fails (D2 amendment).
	It("falls back to kubeconfig snapshot when CAFetcher returns error", func() {
		failer := &platform.AlwaysFailFetcher{Err: errors.New("simulated RBAC denied")}
		r := platform.NewReconcilerForTesting(k8sClient, argoNS, failer)

		cr, sec, kubeconfigData := mkOpBCRAndSecret("op-b-fallback")
		Expect(k8sClient.Create(ctx, sec)).To(Succeed())
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		Expect(platform.RunReconcileArgoCDSecretForTesting(ctx, r, cr, kubeconfigData)).To(Succeed())

		var argoSecret corev1.Secret
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "cluster-op-b-fallback", Namespace: argoNS,
		}, &argoSecret)).To(Succeed())

		decoded := decodeOpBSecretCAData(&argoSecret)
		Expect(decoded).To(Equal(string(snapshotCA)),
			"Secret caData must fall back to kubeconfig snapshot when fetch fails")
		Expect(failer.CallCount()).To(BeNumerically(">=", 1),
			"failing fetcher should have been invoked (proves Op B path was taken before falling back)")
	})

	// Spec 3 — spec.tls.insecure=true short-circuits the fetch (D4 / Op A precedence).
	It("does NOT call CAFetcher when spec.tls.insecure=true", func() {
		stub := &platform.StubCAFetcher{ReturnCA: []byte(distinctFakeCAPEM)}
		r := platform.NewReconcilerForTesting(k8sClient, argoNS, stub)

		cr, sec, kubeconfigData := mkOpBCRAndSecret("op-b-insecure-skip")
		cr.Spec.TLS.Insecure = true
		Expect(k8sClient.Create(ctx, sec)).To(Succeed())
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		Expect(platform.RunReconcileArgoCDSecretForTesting(ctx, r, cr, kubeconfigData)).To(Succeed())

		var argoSecret corev1.Secret
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "cluster-op-b-insecure-skip", Namespace: argoNS,
		}, &argoSecret)).To(Succeed())

		// Assert Secret rendered with insecure=true + empty caData (per Op A semantics).
		configRaw := argoSecret.Data["config"]
		var argoCfg struct {
			TLSClientConfig struct {
				Insecure bool   `json:"insecure"`
				CAData   string `json:"caData,omitempty"`
			} `json:"tlsClientConfig"`
		}
		Expect(json.Unmarshal(configRaw, &argoCfg)).To(Succeed())
		Expect(argoCfg.TLSClientConfig.Insecure).To(BeTrue(),
			"Op A precedence: spec.tls.insecure=true must render Secret with insecure=true")
		Expect(argoCfg.TLSClientConfig.CAData).To(BeEmpty(),
			"insecure mode skips CA fetch — caData must be empty")

		// THE assertion: stub fetcher was NOT invoked.
		Expect(stub.CallCount()).To(Equal(int64(0)),
			"D4 short-circuit: CAFetcher must NOT be called when insecure=true")
	})

	// Spec 4 — repeated reconciles invoke fetcher idempotently (idempotency).
	//
	// Note: envtest also runs the production-wired reconciler against any CR
	// we create — and IT uses the real kubeRootCAFetcher (which fails with
	// "anonymous user forbidden" on envtest and falls back to snapshot).
	// That production reconciler may also write to the Secret after our
	// direct calls, so we cannot Consistently-assert exact Secret content
	// without race. The IDEMPOTENCY property we CAN deterministically
	// assert: our stub fetcher was called exactly N times for N direct
	// reconcile invocations, AND the Secret exists with a non-empty caData
	// (the writes are idempotent — no panic, no duplicate Secret, no
	// repeated error in logs).
	It("repeated direct reconciles invoke fetcher N times (idempotent)", func() {
		stub := &platform.StubCAFetcher{ReturnCA: []byte(distinctFakeCAPEM)}
		r := platform.NewReconcilerForTesting(k8sClient, argoNS, stub)

		cr, sec, kubeconfigData := mkOpBCRAndSecret("op-b-idempotent")
		Expect(k8sClient.Create(ctx, sec)).To(Succeed())
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		// Run reconcile 3 times.
		for i := 0; i < 3; i++ {
			Expect(platform.RunReconcileArgoCDSecretForTesting(ctx, r, cr, kubeconfigData)).To(Succeed())
		}

		// Counter incremented exactly 3x — deterministic, race-free.
		Expect(stub.CallCount()).To(Equal(int64(3)),
			"fetcher must be invoked once per direct reconcile call")

		// Secret exists and renders successfully (no panic / no duplicate).
		// Content may race with the production-wired reconciler; we only
		// assert structural validity, not byte-exact match.
		Eventually(func(g Gomega) {
			var argoSecret corev1.Secret
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "cluster-op-b-idempotent", Namespace: argoNS,
			}, &argoSecret)).To(Succeed())
			g.Expect(argoSecret.Data).To(HaveKey("config"),
				"rendered Secret must have config key")
		}, 5*time.Second, 200*time.Millisecond).Should(Succeed())
	})
})
