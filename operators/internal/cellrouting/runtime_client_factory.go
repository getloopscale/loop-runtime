// Package cellrouting resolves how a controller REACHES the cluster of a cell.
//
// Split out of `reconcilehelpers` in B-061: routing to a cell and reconciling a
// resource change for different reasons, and they shared a package only because
// its name could not refuse either of them.
package cellrouting

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

// kubeconfigKey is the Secret data key holding a cell's kubeconfig.
const kubeconfigKey = "kubeconfig"

// RuntimeClientFactory abstracts cell-aware runtime client lookup.
// Implementations resolve a tenant's cellID to the right cluster client.
type RuntimeClientFactory interface {
	// For returns a controller-runtime client connected to the runtime
	// cluster identified by cellID. Only cellID "" returns the legacy
	// client; every named cell resolves its own ClusterCredential.
	For(ctx context.Context, cellID string) (client.Client, error)
}

// CellAwareRuntimeClientFactory implements RuntimeClientFactory by reading
// ClusterCredential CRs from the mgmt cluster and building runtime clients
// from their kubeconfig Secrets.
//
// A built client is cached per cell and keyed by the identity and
// resourceVersion of the kubeconfig Secret it was built from, so a rotated
// credential produces a new client on the next lookup instead of failing every
// reconcile until the operator restarts. The ClusterCredential and Secret reads
// go through MgmtClient (the manager's cached client in production) and happen
// outside the lock; only the map access is serialised.
type CellAwareRuntimeClientFactory struct {
	// MgmtClient reads ClusterCredential CRs + their kubeconfig Secrets.
	MgmtClient client.Client
	// DefaultClient is returned only for cellID="" (legacy path).
	DefaultClient client.Client
	// MgmtNamespace is where ClusterCredential CRs live (default: loopzero-system).
	MgmtNamespace string

	mu      sync.RWMutex
	clients map[string]cachedCellClient
}

// cachedCellClient is a built cell client plus the version of the kubeconfig
// Secret it was built from.
type cachedCellClient struct {
	client        client.Client
	secretVersion string
}

// NewCellAwareRuntimeClientFactory builds a factory wired to the mgmt
// cluster client + a default runtime client (legacy single-cell path).
// MgmtNamespace defaults to "loopzero-system" if empty.
func NewCellAwareRuntimeClientFactory(mgmtClient, defaultClient client.Client, mgmtNamespace string) *CellAwareRuntimeClientFactory {
	if mgmtNamespace == "" {
		mgmtNamespace = "loopzero-system"
	}
	return &CellAwareRuntimeClientFactory{
		MgmtClient:    mgmtClient,
		DefaultClient: defaultClient,
		MgmtNamespace: mgmtNamespace,
		clients:       make(map[string]cachedCellClient),
	}
}

// For implements RuntimeClientFactory.
func (f *CellAwareRuntimeClientFactory) For(ctx context.Context, cellID string) (client.Client, error) {
	if cellID == "" {
		if f.DefaultClient == nil {
			return nil, fmt.Errorf("runtime-client-factory: default client not configured for cellID=%q", cellID)
		}
		return f.DefaultClient, nil
	}

	secret, err := f.kubeconfigSecret(ctx, cellID)
	if err != nil {
		return nil, err
	}
	version := string(secret.UID) + "/" + secret.ResourceVersion

	f.mu.RLock()
	cached, ok := f.clients[cellID]
	f.mu.RUnlock()
	if ok && cached.secretVersion == version {
		return cached.client, nil
	}

	c, err := f.buildClient(cellID, secret)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.clients[cellID] = cachedCellClient{client: c, secretVersion: version}
	f.mu.Unlock()
	return c, nil
}

// kubeconfigSecret resolves the kubeconfig Secret of the ClusterCredential
// whose spec.cellID matches. Lookup is by spec.cellID, NOT metadata.name
// (MEET-053 B9) — the same rule as the API's K8sCellStore.
func (f *CellAwareRuntimeClientFactory) kubeconfigSecret(ctx context.Context, cellID string) (*corev1.Secret, error) {
	var ccList loopzerov1.ClusterCredentialList
	if err := f.MgmtClient.List(ctx, &ccList, client.InNamespace(f.MgmtNamespace)); err != nil {
		return nil, fmt.Errorf("runtime-client-factory: list ClusterCredentials: %w", err)
	}
	var match *loopzerov1.ClusterCredential
	for i := range ccList.Items {
		if ccList.Items[i].Spec.CellID == cellID {
			match = &ccList.Items[i]
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("runtime-client-factory: no ClusterCredential found with spec.cellID=%q in namespace %s", cellID, f.MgmtNamespace)
	}

	secretNS := match.Spec.SecretRef.Namespace
	if secretNS == "" {
		secretNS = f.MgmtNamespace
	}
	var secret corev1.Secret
	if err := f.MgmtClient.Get(ctx, client.ObjectKey{Name: match.Spec.SecretRef.Name, Namespace: secretNS}, &secret); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("runtime-client-factory: kubeconfig Secret %s/%s not found for cell %q",
				secretNS, match.Spec.SecretRef.Name, cellID)
		}
		return nil, fmt.Errorf("runtime-client-factory: get kubeconfig Secret for cell %q: %w", cellID, err)
	}
	return &secret, nil
}

// buildClient turns the kubeconfig carried by secret into a runtime client.
func (f *CellAwareRuntimeClientFactory) buildClient(cellID string, secret *corev1.Secret) (client.Client, error) {
	raw, ok := secret.Data[kubeconfigKey]
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("runtime-client-factory: Secret %s/%s has no %q key",
			secret.Namespace, secret.Name, kubeconfigKey)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("runtime-client-factory: parse kubeconfig for cell %q: %w", cellID, err)
	}
	c, err := client.New(restConfig, client.Options{Scheme: f.MgmtClient.Scheme()})
	if err != nil {
		return nil, fmt.Errorf("runtime-client-factory: build client for cell %q: %w", cellID, err)
	}
	return c, nil
}

// staticRuntimeClientFactory is a trivial RuntimeClientFactory that always
// returns the same client regardless of cellID. Used in suite_test.go and
// any other code path that hasn't been migrated to multi-cell yet —
// preserves backward compatibility during incremental rollout.
type staticRuntimeClientFactory struct {
	c client.Client
}

// NewStaticRuntimeClientFactory wraps a single client as a factory. For
// tests + legacy single-cell deployments that don't have a mgmt-side
// ClusterCredential registry yet.
func NewStaticRuntimeClientFactory(c client.Client) RuntimeClientFactory {
	return &staticRuntimeClientFactory{c: c}
}

// For returns the static client regardless of cellID.
func (s *staticRuntimeClientFactory) For(_ context.Context, _ string) (client.Client, error) {
	if s.c == nil {
		return nil, fmt.Errorf("static-runtime-client-factory: no client configured")
	}
	return s.c, nil
}
