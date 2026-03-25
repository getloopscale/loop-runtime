package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// SecretK8sManager creates K8s Secrets on mgmt cluster and ExternalSecret CRs
// on the workload (runtime) cluster for ESO to sync.
type SecretK8sManager struct {
	mgmtClient    kubernetes.Interface
	runtimeDynamic dynamic.Interface
	mgmtNamespace string
}

var externalSecretGVR = schema.GroupVersionResource{
	Group:    "external-secrets.io",
	Version:  "v1beta1",
	Resource: "externalsecrets",
}

// NewSecretK8sManager creates a manager that can create K8s Secrets on mgmt
// and ExternalSecret CRs on runtime cluster.
func NewSecretK8sManager(mgmtKubeconfig, mgmtContext, runtimeKubeconfig, runtimeContext string) (*SecretK8sManager, error) {
	// Mgmt cluster — typed client for Secret creation
	mgmtConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: mgmtKubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: mgmtContext},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("secret k8s manager: mgmt config: %w", err)
	}
	mgmtClient, err := kubernetes.NewForConfig(mgmtConfig)
	if err != nil {
		return nil, fmt.Errorf("secret k8s manager: mgmt client: %w", err)
	}

	// Runtime cluster — dynamic client for ExternalSecret CRs
	runtimeConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: runtimeKubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: runtimeContext},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("secret k8s manager: runtime config: %w", err)
	}
	runtimeDynamic, err := dynamic.NewForConfig(runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("secret k8s manager: runtime dynamic: %w", err)
	}

	return &SecretK8sManager{
		mgmtClient:     mgmtClient,
		runtimeDynamic: runtimeDynamic,
		mgmtNamespace:  "platform-system",
	}, nil
}

// secretName generates a K8s-compatible name for a secret.
func secretName(tenantID, app, name string) string {
	return strings.ToLower(fmt.Sprintf("secret-%s-%s-%s", tenantID, app, name))
}

// externalSecretName generates a K8s-compatible name for an ExternalSecret CR.
func externalSecretName(app, name string) string {
	return strings.ToLower(fmt.Sprintf("es-%s-%s", app, name))
}

// CreateSecret creates a K8s Secret on the mgmt cluster and an ExternalSecret CR
// on the runtime cluster for ESO to sync the value to the workload namespace.
func (m *SecretK8sManager) CreateSecret(ctx context.Context, tenantID, projectID, env, app, name, value string) error {
	k8sName := secretName(tenantID, app, name)

	// 1. Create/update K8s Secret on mgmt cluster
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k8sName,
			Namespace: m.mgmtNamespace,
			Labels: map[string]string{
				"loopzero.io/tenant":     tenantID,
				"loopzero.io/project":    projectID,
				"loopzero.io/app":        app,
				"loopzero.io/managed-by": "loopzero-api",
			},
		},
		StringData: map[string]string{
			"value": value,
		},
	}

	_, err := m.mgmtClient.CoreV1().Secrets(m.mgmtNamespace).Get(ctx, k8sName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = m.mgmtClient.CoreV1().Secrets(m.mgmtNamespace).Create(ctx, secret, metav1.CreateOptions{})
		}
	} else {
		_, err = m.mgmtClient.CoreV1().Secrets(m.mgmtNamespace).Update(ctx, secret, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("create k8s secret on mgmt: %w", err)
	}

	// 2. Create/update ExternalSecret CR on runtime cluster
	targetNamespace := fmt.Sprintf("%s-%s", tenantID, env)
	esName := externalSecretName(app, name)

	externalSecret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "external-secrets.io/v1beta1",
			"kind":       "ExternalSecret",
			"metadata": map[string]interface{}{
				"name":      esName,
				"namespace": targetNamespace,
				"labels": map[string]interface{}{
					"loopzero.io/tenant":     tenantID,
					"loopzero.io/project":    projectID,
					"loopzero.io/app":        app,
					"loopzero.io/managed-by": "loopzero-api",
				},
			},
			"spec": map[string]interface{}{
				"refreshInterval": "30s",
				"secretStoreRef": map[string]interface{}{
					"name": "loopzero-mgmt-store",
					"kind": "ClusterSecretStore",
				},
				"target": map[string]interface{}{
					"name":           fmt.Sprintf("%s-secrets", app),
					"creationPolicy": "Owner",
				},
				"data": []interface{}{
					map[string]interface{}{
						"secretKey": name,
						"remoteRef": map[string]interface{}{
							"key":      k8sName,
							"property": "value",
						},
					},
				},
			},
		},
	}

	_, err = m.runtimeDynamic.Resource(externalSecretGVR).Namespace(targetNamespace).Get(ctx, esName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = m.runtimeDynamic.Resource(externalSecretGVR).Namespace(targetNamespace).Create(ctx, externalSecret, metav1.CreateOptions{})
		}
	} else {
		_, err = m.runtimeDynamic.Resource(externalSecretGVR).Namespace(targetNamespace).Update(ctx, externalSecret, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("create external secret on runtime: %w", err)
	}

	return nil
}

// DeleteSecret removes the K8s Secret from mgmt and ExternalSecret CR from runtime.
func (m *SecretK8sManager) DeleteSecret(ctx context.Context, tenantID, env, app, name string) error {
	k8sName := secretName(tenantID, app, name)

	// 1. Delete K8s Secret from mgmt
	err := m.mgmtClient.CoreV1().Secrets(m.mgmtNamespace).Delete(ctx, k8sName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete k8s secret from mgmt: %w", err)
	}

	// 2. Delete ExternalSecret CR from runtime
	targetNamespace := fmt.Sprintf("%s-%s", tenantID, env)
	esName := externalSecretName(app, name)
	err = m.runtimeDynamic.Resource(externalSecretGVR).Namespace(targetNamespace).Delete(ctx, esName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete external secret from runtime: %w", err)
	}

	return nil
}
