// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeDataPlaneAdmin stands in for one data-plane service's admin API: a key
// authenticates the cheap admin read when the service knows it, and the bootstrap
// token mints a new one until the service is told to refuse it.
type fakeDataPlaneAdmin struct {
	spec dataPlaneAdminKey

	mu            sync.Mutex
	valid         map[string]bool
	bootstrap     string
	refuseMint    bool
	readStatus    int // forced status for the admin read; 0 = derive from the key
	mints         int
	mintedCounter int
}

func (f *fakeDataPlaneAdmin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == f.spec.adminReadPath:
		if f.readStatus != 0 {
			w.WriteHeader(f.readStatus)
			return
		}
		if !f.valid[bearer] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	case r.Method == http.MethodPost && r.URL.Path == "/v1/admin/keys":
		f.mints++
		if f.refuseMint || bearer != f.bootstrap {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"bootstrap_already_consumed"}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] == nil || body["scopes"] == nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		f.mintedCounter++
		key := f.spec.service + "-minted-" + strconv.Itoa(f.mintedCounter)
		f.valid[key] = true
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{f.spec.mintField: key})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeDataPlaneAdmin) mintCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mints
}

// dataPlaneFixture wires a fake cluster and one fake admin API per enabled service.
type dataPlaneFixture struct {
	t        *testing.T
	client   ctrlclient.Client
	admins   map[string]*fakeDataPlaneAdmin
	servers  map[string]*httptest.Server
	tunneled map[string]TunnelTarget
}

func dataPlaneSpec(t *testing.T, service string) dataPlaneAdminKey {
	t.Helper()
	for _, s := range dataPlaneAdminKeys {
		if s.service == service {
			return s
		}
	}
	t.Fatalf("no data-plane admin key spec for %s", service)
	return dataPlaneAdminKey{}
}

func readyDeployment(namespace, name string) *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 1},
	}
}

func bootstrapSecret(service, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: dataPlaneNamespace, Name: service},
		Data:       map[string][]byte{dataPlaneBootstrapKey: []byte(token)},
	}
}

// newDataPlaneFixture deploys the given services, each with a bootstrap token, and
// loopzero-cloud. extra objects (stored admin-key Secrets) are added as given.
func newDataPlaneFixture(t *testing.T, services []string, extra ...ctrlclient.Object) *dataPlaneFixture {
	t.Helper()
	f := &dataPlaneFixture{t: t, admins: map[string]*fakeDataPlaneAdmin{}, servers: map[string]*httptest.Server{}, tunneled: map[string]TunnelTarget{}}
	objs := []ctrlclient.Object{readyDeployment("loopzero-system", "loopzero-cloud")}
	for _, svc := range services {
		spec := dataPlaneSpec(t, svc)
		admin := &fakeDataPlaneAdmin{spec: spec, valid: map[string]bool{}, bootstrap: "boot-" + svc}
		f.admins[svc] = admin
		srv := httptest.NewServer(admin)
		t.Cleanup(srv.Close)
		f.servers[svc] = srv
		objs = append(objs, readyDeployment(dataPlaneNamespace, svc), bootstrapSecret(svc, admin.bootstrap))
	}
	f.client = fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(append(objs, extra...)...).Build()
	return f
}

func (f *dataPlaneFixture) deps() Deps {
	return Deps{
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(f.client),
		CtrlClient:           f.client,
		RolloutSettleTimeout: 50 * time.Millisecond,
		PortForwardCtor: func(_ context.Context, target TunnelTarget) (*Tunnel, error) {
			svc := target.PodLabels["app.kubernetes.io/name"]
			srv, ok := f.servers[svc]
			if !ok {
				return nil, errors.New("no pod for " + svc)
			}
			f.tunneled[svc] = target
			u, _ := url.Parse(srv.URL)
			port, _ := strconv.Atoi(u.Port())
			return &Tunnel{LocalPort: port, Close: func() {}}, nil
		},
	}
}

func (f *dataPlaneFixture) storedKey(service string) string {
	f.t.Helper()
	spec := dataPlaneSpec(f.t, service)
	var s corev1.Secret
	if err := f.client.Get(context.Background(), fakeKey("loopzero-system", spec.secretName), &s); err != nil {
		return ""
	}
	return string(s.Data[spec.envKey])
}

func (f *dataPlaneFixture) cloud() *appsv1.Deployment {
	f.t.Helper()
	var d appsv1.Deployment
	if err := f.client.Get(context.Background(), fakeKey("loopzero-system", "loopzero-cloud"), &d); err != nil {
		f.t.Fatalf("get loopzero-cloud: %v", err)
	}
	return &d
}

func storedAdminKeySecret(t *testing.T, service, key string) *corev1.Secret {
	spec := dataPlaneSpec(t, service)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: spec.secretName},
		Data:       map[string][]byte{spec.envKey: []byte(key)},
	}
}

var allDataPlane = []string{"loopzero-memory", "loopzero-rag", "loopzero-lens"}

func TestDataPlaneAdminKeysMintsAKeyForEveryDeployedServiceWithoutOne(t *testing.T) {
	f := newDataPlaneFixture(t, allDataPlane)

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	for _, svc := range allDataPlane {
		key := f.storedKey(svc)
		if !f.admins[svc].valid[key] {
			t.Errorf("%s: stored key %q is not one the service minted", svc, key)
		}
	}
}

func TestDataPlaneAdminKeysReachesEachServiceAppPodOnItsPort(t *testing.T) {
	f := newDataPlaneFixture(t, allDataPlane)

	NewDataPlaneAdminKeysSeed().Run(f.deps())

	for _, svc := range allDataPlane {
		target := f.tunneled[svc]
		spec := dataPlaneSpec(t, svc)
		if target.Namespace != dataPlaneNamespace || target.RemotePort != spec.port ||
			target.PodLabels["app.kubernetes.io/component"] != "app" {
			t.Errorf("%s tunnel = %+v, want the app pod in %s on :%d", svc, target, dataPlaneNamespace, spec.port)
		}
	}
}

func TestDataPlaneAdminKeysKeepsAKeyThatStillAuthenticates(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-rag"}, storedAdminKeySecret(t, "loopzero-rag", "rag-still-good"))
	f.admins["loopzero-rag"].valid["rag-still-good"] = true

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if n := f.admins["loopzero-rag"].mintCount(); n != 0 {
		t.Errorf("minted %d key(s) although the stored one authenticates", n)
	}
	if k := f.storedKey("loopzero-rag"); k != "rag-still-good" {
		t.Errorf("stored key replaced with %q", k)
	}
}

func TestDataPlaneAdminKeysReplacesAKeyTheServiceNoLongerAccepts(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-lens"}, storedAdminKeySecret(t, "loopzero-lens", "lens-revoked"))

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if k := f.storedKey("loopzero-lens"); k == "lens-revoked" || !f.admins["loopzero-lens"].valid[k] {
		t.Errorf("stored key = %q, want a freshly minted one", k)
	}
}

func TestDataPlaneAdminKeysFailsWhenTheBootstrapIsRefusedAndNoKeyWorks(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-memory"})
	f.admins["loopzero-memory"].refuseMint = true

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "loopzero-memory") || !strings.Contains(got.Reason, "refused") {
		t.Errorf("Reason = %q, want it to name loopzero-memory and the refusal", got.Reason)
	}
	if k := f.storedKey("loopzero-memory"); k != "" {
		t.Errorf("a key was stored although none was minted: %q", k)
	}
	if a := f.cloud().Spec.Template.Annotations[dataPlaneKeysAnnotation]; a != "" {
		t.Errorf("loopzero-cloud was rolled (%s) with no key to load", a)
	}
}

func TestMintPlatformAdminKeyReturnsATypedRefusal(t *testing.T) {
	spec := dataPlaneSpec(t, "loopzero-rag")
	admin := &fakeDataPlaneAdmin{spec: spec, valid: map[string]bool{}, bootstrap: "boot", refuseMint: true}
	srv := httptest.NewServer(admin)
	defer srv.Close()

	_, err := mintPlatformAdminKey(context.Background(), http.DefaultClient, srv.URL, spec, "boot")

	var refused *BootstrapRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want *BootstrapRefusedError", err)
	}
	if refused.Service != "loopzero-rag" || refused.Status != http.StatusForbidden {
		t.Errorf("refusal = %+v, want service loopzero-rag status 403", refused)
	}
}

func TestDataPlaneAdminKeysDoesNotMintOnATransientAdminReadFailure(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-rag"}, storedAdminKeySecret(t, "loopzero-rag", "rag-maybe-good"))
	f.admins["loopzero-rag"].readStatus = http.StatusServiceUnavailable

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if n := f.admins["loopzero-rag"].mintCount(); n != 0 {
		t.Errorf("minted %d key(s) on a 503 — a single-use bootstrap would be burned on an outage", n)
	}
}

func TestDataPlaneAdminKeysFailsWhenNoBootstrapTokenIsAvailable(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-lens"})
	if err := f.client.Delete(context.Background(), bootstrapSecret("loopzero-lens", "")); err != nil {
		t.Fatal(err)
	}

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != FAIL || !strings.Contains(got.Reason, dataPlaneBootstrapKey) {
		t.Fatalf("Verdict = %s (%s), want FAIL naming %s", got.Verdict, got.Reason, dataPlaneBootstrapKey)
	}
}

func TestDataPlaneAdminKeysRestartsTheCloudWhenAKeyChanged(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-memory"})

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if f.cloud().Spec.Template.Annotations[dataPlaneKeysAnnotation] == "" {
		t.Errorf("loopzero-cloud pod template carries no %s — it was not restarted onto the new key", dataPlaneKeysAnnotation)
	}
}

func TestDataPlaneAdminKeysLeavesTheCloudAloneWhenNothingChanged(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-memory"})
	if got := NewDataPlaneAdminKeysSeed().Run(f.deps()); got.Verdict != PASS {
		t.Fatalf("first run = %s (%s)", got.Verdict, got.Reason)
	}
	before := f.cloud().ResourceVersion

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if after := f.cloud().ResourceVersion; after != before {
		t.Errorf("loopzero-cloud was updated (resourceVersion %s → %s) although no key changed", before, after)
	}
}

func TestDataPlaneAdminKeysRestartsTheCloudThatNeverLoadedAKeptKey(t *testing.T) {
	// A previous run stored the key and failed before the restart took effect: the key
	// still authenticates, yet the running loopzero-cloud never read it.
	f := newDataPlaneFixture(t, []string{"loopzero-rag"}, storedAdminKeySecret(t, "loopzero-rag", "rag-good"))
	f.admins["loopzero-rag"].valid["rag-good"] = true

	NewDataPlaneAdminKeysSeed().Run(f.deps())

	if f.cloud().Spec.Template.Annotations[dataPlaneKeysAnnotation] == "" {
		t.Error("loopzero-cloud was not restarted onto a key it had never loaded")
	}
}

func TestDataPlaneAdminKeysLeavesAServiceThatIsNotDeployedAlone(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-memory"})

	NewDataPlaneAdminKeysSeed().Run(f.deps())

	for _, svc := range []string{"loopzero-rag", "loopzero-lens"} {
		if _, tunneled := f.tunneled[svc]; tunneled {
			t.Errorf("%s is not deployed but was reached", svc)
		}
		if k := f.storedKey(svc); k != "" {
			t.Errorf("%s is not deployed but a key was stored", svc)
		}
	}
}

func TestDataPlaneAdminKeysSkipsWhenNoDataPlaneServiceIsDeployed(t *testing.T) {
	f := newDataPlaneFixture(t, nil)

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestDataPlaneAdminKeysVerifyFailsWhenTheCloudDidNotLoadTheKeys(t *testing.T) {
	f := newDataPlaneFixture(t, []string{"loopzero-rag"}, storedAdminKeySecret(t, "loopzero-rag", "rag-good"))
	f.admins["loopzero-rag"].valid["rag-good"] = true

	got := NewDataPlaneAdminKeysSeed().Verify(f.deps())

	if got.Verdict != FAIL {
		t.Fatalf("Verify = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestDataPlaneAdminKeysVerifyPassesAfterARun(t *testing.T) {
	f := newDataPlaneFixture(t, allDataPlane)
	if got := NewDataPlaneAdminKeysSeed().Run(f.deps()); got.Verdict != PASS {
		t.Fatalf("Run = %s (%s)", got.Verdict, got.Reason)
	}

	if got := NewDataPlaneAdminKeysSeed().Verify(f.deps()); got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestDataPlaneAdminKeysKeepsGoingPastAServiceThatFails(t *testing.T) {
	// Measured 2026-09-30: loopzero-rag answered 503 to every bearer while memory and lens
	// minted fine. One broken service must not leave the others without a key.
	f := newDataPlaneFixture(t, allDataPlane)
	f.admins["loopzero-rag"].readStatus = http.StatusServiceUnavailable
	f.admins["loopzero-rag"].refuseMint = true

	got := NewDataPlaneAdminKeysSeed().Run(f.deps())

	if got.Verdict != FAIL || !strings.Contains(got.Reason, "loopzero-rag") {
		t.Fatalf("Verdict = %s (%s), want FAIL naming loopzero-rag", got.Verdict, got.Reason)
	}
	for _, svc := range []string{"loopzero-memory", "loopzero-lens"} {
		if k := f.storedKey(svc); !f.admins[svc].valid[k] {
			t.Errorf("%s got no working key because loopzero-rag failed", svc)
		}
	}
	if f.cloud().Spec.Template.Annotations[dataPlaneKeysAnnotation] == "" {
		t.Error("loopzero-cloud was not rolled onto the keys that were minted")
	}
}
