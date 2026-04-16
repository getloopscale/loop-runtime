// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services_test contains golden behavioral tests for LoopZero's AppSecret
// wiring with External Secrets Operator (ESO).
//
// These tests are SPECIFICATION tests, not integration tests. They document the
// behavioral contracts that LoopZero's secret sync wiring MUST satisfy. Each test
// function describes a single contract derived from ESO's behavioral patterns,
// adapted to LoopZero's multi-cluster architecture:
//
//   mgmt cluster: K8s Secret (source of truth)
//   runtime cluster: ExternalSecret CR -> ClusterSecretStore -> K8s Secret (target)
//
// Reference: ESO externalsecret_controller_test.go patterns (table-driven testCase
// struct with checkCondition + checkSecret callbacks, makeDefaultTestcase factory).
//
// LoopZero's wiring is in:
//   - api/internal/k8s/secret_k8s_manager.go  (SecretK8sManager)
//   - api/internal/routes/secrets.go           (SecretHandler + SecretK8sSync interface)
//   - operators/api/v1/appsecret_types.go      (AppSecret CRD)
package unit

import (
	"context"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Domain types — mirrors of LoopZero's actual types, kept here so the golden file
// is self-contained and readable without importing internal packages.
// ---------------------------------------------------------------------------

// SecretSyncInput represents the parameters for a secret set operation.
type SecretSyncInput struct {
	TenantID    string
	ProjectID   string
	Environment string
	App         string
	Name        string
	Value       string
}

// MgmtSecret represents the K8s Secret created on the management cluster.
type MgmtSecret struct {
	Name      string
	Namespace string
	Labels    map[string]string
	Data      map[string]string
}

// ExternalSecretCR represents the ExternalSecret CR created on the runtime cluster.
type ExternalSecretCR struct {
	Name            string
	Namespace       string
	Labels          map[string]string
	StoreRef        StoreRef
	TargetName      string
	CreationPolicy  string
	RefreshInterval string
	DataMappings    []DataMapping
}

// StoreRef identifies the ClusterSecretStore reference on the ExternalSecret.
type StoreRef struct {
	Name string
	Kind string // "ClusterSecretStore" or "SecretStore"
}

// DataMapping represents a single key mapping in the ExternalSecret spec.
type DataMapping struct {
	SecretKey  string
	RemoteKey  string
	RemoteProp string
}

// AppSecretCondition represents a status condition on the AppSecret CRD.
type AppSecretCondition struct {
	Type    string // "Synced", "RotationCurrent"
	Status  string // "True", "False", "Unknown"
	Reason  string
	Message string
}

// SyncResult represents the output of LoopZero's secret sync operation.
type SyncResult struct {
	MgmtSecret     *MgmtSecret
	ExternalSecret *ExternalSecretCR
	Conditions     []AppSecretCondition
	Err            error
}

// ---------------------------------------------------------------------------
// Fake wiring — simulates SecretK8sManager behavior for contract validation.
// In a real test, this would use envtest or fake K8s clients.
// ---------------------------------------------------------------------------

// FakeSecretSyncer simulates LoopZero's SecretK8sManager for behavioral validation.
type FakeSecretSyncer struct {
	mgmtSecrets     map[string]*MgmtSecret
	externalSecrets map[string]*ExternalSecretCR
	shouldFail      map[string]error // key -> error to return
}

func NewFakeSecretSyncer() *FakeSecretSyncer {
	return &FakeSecretSyncer{
		mgmtSecrets:     make(map[string]*MgmtSecret),
		externalSecrets: make(map[string]*ExternalSecretCR),
		shouldFail:      make(map[string]error),
	}
}

// k8sName mirrors api/internal/k8s/secret_k8s_manager.go:k8sName
func k8sName(parts ...string) string {
	name := ""
	for i, p := range parts {
		if i > 0 {
			name += "-"
		}
		name += p
	}
	// lowercase + replace underscores
	result := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			c = c + 32
		}
		if c == '_' {
			c = '-'
		}
		result[i] = c
	}
	return string(result)
}

func (f *FakeSecretSyncer) CreateSecret(_ context.Context, input SecretSyncInput) (*SyncResult, error) {
	if err, ok := f.shouldFail[input.Name]; ok {
		return nil, err
	}

	mgmtSecretName := k8sName("secret", input.TenantID, input.App, input.Name)
	targetNS := fmt.Sprintf("loopzero-%s-%s", input.TenantID, input.Environment)
	esName := k8sName("es", input.App, input.Name)
	targetSecretName := fmt.Sprintf("user-secrets-%s-%s-%s-%s",
		input.TenantID, input.ProjectID, input.Environment, input.App)

	mgmt := &MgmtSecret{
		Name:      mgmtSecretName,
		Namespace: "platform-system",
		Labels: map[string]string{
			"loopzero.io/tenant":     input.TenantID,
			"loopzero.io/project":    input.ProjectID,
			"loopzero.io/app":        input.App,
			"loopzero.io/managed-by": "loopzero-api",
		},
		Data: map[string]string{"value": input.Value},
	}

	es := &ExternalSecretCR{
		Name:      esName,
		Namespace: targetNS,
		Labels: map[string]string{
			"loopzero.io/tenant":     input.TenantID,
			"loopzero.io/project":    input.ProjectID,
			"loopzero.io/app":        input.App,
			"loopzero.io/managed-by": "loopzero-api",
		},
		StoreRef: StoreRef{
			Name: "loopzero-mgmt-store",
			Kind: "ClusterSecretStore",
		},
		TargetName:      targetSecretName,
		CreationPolicy:  "Owner",
		RefreshInterval: "30s",
		DataMappings: []DataMapping{
			{
				SecretKey:  input.Name,
				RemoteKey:  mgmtSecretName,
				RemoteProp: "value",
			},
		},
	}

	f.mgmtSecrets[mgmtSecretName] = mgmt
	f.externalSecrets[esName] = es

	return &SyncResult{
		MgmtSecret:     mgmt,
		ExternalSecret: es,
		Conditions: []AppSecretCondition{
			{Type: "Synced", Status: "True", Reason: "Ready", Message: "secret synced to workload cluster"},
		},
	}, nil
}

func (f *FakeSecretSyncer) DeleteSecret(_ context.Context, tenantID, env, app, name string) error {
	mgmtKey := k8sName("secret", tenantID, app, name)
	esKey := k8sName("es", app, name)
	delete(f.mgmtSecrets, mgmtKey)
	delete(f.externalSecrets, esKey)
	return nil
}

// ---------------------------------------------------------------------------
// CONTRACT 1: Secret sync creates K8s Secret on mgmt cluster
//
// ESO pattern: When an ExternalSecret references a valid store, a K8s Secret
// is created with the expected data.
//
// LoopZero adaptation: When `loopzero secret set` is called, the API creates a K8s
// Secret on the mgmt cluster in the platform-system namespace. This Secret
// becomes the source for ESO to sync into the runtime cluster.
// ---------------------------------------------------------------------------
func TestSecretSync_CreatesK8sSecretOnMgmtCluster(t *testing.T) {
	tests := []struct {
		name     string
		input    SecretSyncInput
		wantName string
		wantNS   string
		wantData map[string]string
	}{
		{
			name: "simple secret creates mgmt Secret with correct naming",
			input: SecretSyncInput{
				TenantID:    "acme",
				ProjectID:   "web-app",
				Environment: "production",
				App:         "api",
				Name:        "DATABASE_URL",
				Value:       "postgres://host:5432/db",
			},
			wantName: "secret-acme-api-database-url",
			wantNS:   "platform-system",
			wantData: map[string]string{"value": "postgres://host:5432/db"},
		},
		{
			name: "secret with underscores normalizes to hyphens",
			input: SecretSyncInput{
				TenantID:    "tenant-1",
				ProjectID:   "proj",
				Environment: "staging",
				App:         "worker",
				Name:        "REDIS_CACHE_URL",
				Value:       "redis://cache:6379",
			},
			wantName: "secret-tenant-1-worker-redis-cache-url",
			wantNS:   "platform-system",
			wantData: map[string]string{"value": "redis://cache:6379"},
		},
		{
			name: "different tenants produce different Secret names",
			input: SecretSyncInput{
				TenantID:    "other-tenant",
				ProjectID:   "proj",
				Environment: "production",
				App:         "api",
				Name:        "DATABASE_URL",
				Value:       "postgres://other:5432/db",
			},
			wantName: "secret-other-tenant-api-database-url",
			wantNS:   "platform-system",
			wantData: map[string]string{"value": "postgres://other:5432/db"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer := NewFakeSecretSyncer()
			result, err := syncer.CreateSecret(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.MgmtSecret == nil {
				t.Fatal("expected MgmtSecret to be created, got nil")
			}
			if result.MgmtSecret.Name != tt.wantName {
				t.Errorf("mgmt secret name: got %q, want %q", result.MgmtSecret.Name, tt.wantName)
			}
			if result.MgmtSecret.Namespace != tt.wantNS {
				t.Errorf("mgmt secret namespace: got %q, want %q", result.MgmtSecret.Namespace, tt.wantNS)
			}
			for k, v := range tt.wantData {
				if result.MgmtSecret.Data[k] != v {
					t.Errorf("mgmt secret data[%q]: got %q, want %q", k, result.MgmtSecret.Data[k], v)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 2: Secret sync creates ExternalSecret CR on runtime cluster
//
// ESO pattern: ExternalSecret references a SecretStore/ClusterSecretStore and
// maps remote keys to local secret keys.
//
// LoopZero adaptation: The API creates an ExternalSecret CR in the runtime cluster
// namespace (loopzero-{tenant}-{env}), referencing the ClusterSecretStore
// "loopzero-mgmt-store" and mapping the mgmt Secret key to the app secret name.
// ---------------------------------------------------------------------------
func TestSecretSync_CreatesExternalSecretOnRuntimeCluster(t *testing.T) {
	tests := []struct {
		name            string
		input           SecretSyncInput
		wantESName      string
		wantNS          string
		wantStoreName   string
		wantStoreKind   string
		wantTargetName  string
		wantPolicy      string
		wantRefresh     string
		wantRemoteKey   string
		wantRemoteProp  string
		wantSecretKey   string
	}{
		{
			name: "ExternalSecret references ClusterSecretStore with correct mapping",
			input: SecretSyncInput{
				TenantID:    "acme",
				ProjectID:   "web-app",
				Environment: "production",
				App:         "api",
				Name:        "DATABASE_URL",
				Value:       "postgres://host:5432/db",
			},
			wantESName:     "es-api-database-url",
			wantNS:         "loopzero-acme-production",
			wantStoreName:  "loopzero-mgmt-store",
			wantStoreKind:  "ClusterSecretStore",
			wantTargetName: "user-secrets-acme-web-app-production-api",
			wantPolicy:     "Owner",
			wantRefresh:    "30s",
			wantRemoteKey:  "secret-acme-api-database-url",
			wantRemoteProp: "value",
			wantSecretKey:  "DATABASE_URL",
		},
		{
			name: "staging environment uses correct namespace",
			input: SecretSyncInput{
				TenantID:    "acme",
				ProjectID:   "web-app",
				Environment: "staging",
				App:         "worker",
				Name:        "REDIS_URL",
				Value:       "redis://cache:6379",
			},
			wantESName:     "es-worker-redis-url",
			wantNS:         "loopzero-acme-staging",
			wantStoreName:  "loopzero-mgmt-store",
			wantStoreKind:  "ClusterSecretStore",
			wantTargetName: "user-secrets-acme-web-app-staging-worker",
			wantPolicy:     "Owner",
			wantRefresh:    "30s",
			wantRemoteKey:  "secret-acme-worker-redis-url",
			wantRemoteProp: "value",
			wantSecretKey:  "REDIS_URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer := NewFakeSecretSyncer()
			result, err := syncer.CreateSecret(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			es := result.ExternalSecret
			if es == nil {
				t.Fatal("expected ExternalSecret to be created, got nil")
			}
			if es.Name != tt.wantESName {
				t.Errorf("ExternalSecret name: got %q, want %q", es.Name, tt.wantESName)
			}
			if es.Namespace != tt.wantNS {
				t.Errorf("ExternalSecret namespace: got %q, want %q", es.Namespace, tt.wantNS)
			}
			if es.StoreRef.Name != tt.wantStoreName {
				t.Errorf("StoreRef.Name: got %q, want %q", es.StoreRef.Name, tt.wantStoreName)
			}
			if es.StoreRef.Kind != tt.wantStoreKind {
				t.Errorf("StoreRef.Kind: got %q, want %q", es.StoreRef.Kind, tt.wantStoreKind)
			}
			if es.TargetName != tt.wantTargetName {
				t.Errorf("TargetName: got %q, want %q", es.TargetName, tt.wantTargetName)
			}
			if es.CreationPolicy != tt.wantPolicy {
				t.Errorf("CreationPolicy: got %q, want %q", es.CreationPolicy, tt.wantPolicy)
			}
			if es.RefreshInterval != tt.wantRefresh {
				t.Errorf("RefreshInterval: got %q, want %q", es.RefreshInterval, tt.wantRefresh)
			}

			if len(es.DataMappings) != 1 {
				t.Fatalf("expected 1 data mapping, got %d", len(es.DataMappings))
			}
			dm := es.DataMappings[0]
			if dm.SecretKey != tt.wantSecretKey {
				t.Errorf("DataMapping.SecretKey: got %q, want %q", dm.SecretKey, tt.wantSecretKey)
			}
			if dm.RemoteKey != tt.wantRemoteKey {
				t.Errorf("DataMapping.RemoteKey: got %q, want %q", dm.RemoteKey, tt.wantRemoteKey)
			}
			if dm.RemoteProp != tt.wantRemoteProp {
				t.Errorf("DataMapping.RemoteProperty: got %q, want %q", dm.RemoteProp, tt.wantRemoteProp)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 3: Labels enforce tenant isolation (loopzero.io/tenant, loopzero.io/project)
//
// ESO pattern: ClusterSecretStore with namespace conditions enforces that
// ExternalSecrets can only access secrets belonging to their namespace/tenant.
//
// LoopZero adaptation: Both mgmt Secret and ExternalSecret CR carry loopzero.io/tenant
// and loopzero.io/project labels. The ClusterSecretStore SHOULD be configured with
// namespace conditions so that tenant A cannot reference tenant B's secrets.
// The labels are the enforcement mechanism.
// ---------------------------------------------------------------------------
func TestSecretSync_LabelsEnforceTenantIsolation(t *testing.T) {
	tests := []struct {
		name        string
		input       SecretSyncInput
		wantLabels  map[string]string
	}{
		{
			name: "mgmt Secret and ExternalSecret carry matching tenant labels",
			input: SecretSyncInput{
				TenantID:    "acme",
				ProjectID:   "web-app",
				Environment: "production",
				App:         "api",
				Name:        "API_KEY",
				Value:       "sk-secret",
			},
			wantLabels: map[string]string{
				"loopzero.io/tenant":     "acme",
				"loopzero.io/project":    "web-app",
				"loopzero.io/app":        "api",
				"loopzero.io/managed-by": "loopzero-api",
			},
		},
		{
			name: "different tenant gets different label values",
			input: SecretSyncInput{
				TenantID:    "other-co",
				ProjectID:   "backend",
				Environment: "staging",
				App:         "worker",
				Name:        "DB_PASS",
				Value:       "pass123",
			},
			wantLabels: map[string]string{
				"loopzero.io/tenant":     "other-co",
				"loopzero.io/project":    "backend",
				"loopzero.io/app":        "worker",
				"loopzero.io/managed-by": "loopzero-api",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer := NewFakeSecretSyncer()
			result, err := syncer.CreateSecret(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify mgmt Secret labels
			for k, v := range tt.wantLabels {
				got, ok := result.MgmtSecret.Labels[k]
				if !ok {
					t.Errorf("mgmt Secret missing label %q", k)
					continue
				}
				if got != v {
					t.Errorf("mgmt Secret label %q: got %q, want %q", k, got, v)
				}
			}

			// Verify ExternalSecret labels match
			for k, v := range tt.wantLabels {
				got, ok := result.ExternalSecret.Labels[k]
				if !ok {
					t.Errorf("ExternalSecret missing label %q", k)
					continue
				}
				if got != v {
					t.Errorf("ExternalSecret label %q: got %q, want %q", k, got, v)
				}
			}

			// Cross-tenant check: ExternalSecret namespace must match tenant
			wantNS := fmt.Sprintf("loopzero-%s-%s", tt.input.TenantID, tt.input.Environment)
			if result.ExternalSecret.Namespace != wantNS {
				t.Errorf("ExternalSecret namespace %q does not match tenant namespace %q",
					result.ExternalSecret.Namespace, wantNS)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 4: Deletion removes both mgmt Secret and runtime ExternalSecret
//
// ESO pattern: When ExternalSecret is deleted, K8s Secret behavior depends on
// deletionPolicy. With "Owner" policy, ESO deletes the target Secret.
//
// LoopZero adaptation: When `loopzero secret delete` is called, the API removes BOTH
// the mgmt K8s Secret (source) AND the ExternalSecret CR (which triggers ESO
// to clean up the target Secret due to Owner policy). This is a two-phase
// cleanup across clusters.
// ---------------------------------------------------------------------------
func TestSecretSync_DeletionRemovesBothResources(t *testing.T) {
	tests := []struct {
		name        string
		input       SecretSyncInput
		deleteTenant string
		deleteEnv    string
		deleteApp    string
		deleteName   string
	}{
		{
			name: "delete removes mgmt Secret and ExternalSecret CR",
			input: SecretSyncInput{
				TenantID:    "acme",
				ProjectID:   "web-app",
				Environment: "production",
				App:         "api",
				Name:        "DATABASE_URL",
				Value:       "postgres://host:5432/db",
			},
			deleteTenant: "acme",
			deleteEnv:    "production",
			deleteApp:    "api",
			deleteName:   "DATABASE_URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer := NewFakeSecretSyncer()

			// Create first
			_, err := syncer.CreateSecret(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("setup: unexpected error creating secret: %v", err)
			}

			// Verify both resources exist
			mgmtKey := k8sName("secret", tt.input.TenantID, tt.input.App, tt.input.Name)
			esKey := k8sName("es", tt.input.App, tt.input.Name)

			if _, ok := syncer.mgmtSecrets[mgmtKey]; !ok {
				t.Fatal("setup: mgmt Secret should exist before delete")
			}
			if _, ok := syncer.externalSecrets[esKey]; !ok {
				t.Fatal("setup: ExternalSecret should exist before delete")
			}

			// Delete
			err = syncer.DeleteSecret(context.Background(), tt.deleteTenant, tt.deleteEnv, tt.deleteApp, tt.deleteName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify both resources removed
			if _, ok := syncer.mgmtSecrets[mgmtKey]; ok {
				t.Error("mgmt Secret should be removed after delete")
			}
			if _, ok := syncer.externalSecrets[esKey]; ok {
				t.Error("ExternalSecret should be removed after delete")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 5: Owner creation policy ensures cleanup on ExternalSecret deletion
//
// ESO pattern: creationPolicy=Owner sets ownerReference on the target Secret,
// so K8s garbage collection removes it when the ExternalSecret is deleted.
//
// LoopZero adaptation: All ExternalSecret CRs created by LoopZero use "Owner" policy.
// This guarantees that when the ExternalSecret is deleted (by LoopZero or by
// namespace deletion), the synced Secret in the runtime namespace is also
// cleaned up. No orphaned secrets.
// ---------------------------------------------------------------------------
func TestSecretSync_OwnerCreationPolicyIsAlwaysSet(t *testing.T) {
	inputs := []SecretSyncInput{
		{TenantID: "t1", ProjectID: "p1", Environment: "production", App: "api", Name: "KEY_A", Value: "a"},
		{TenantID: "t2", ProjectID: "p2", Environment: "staging", App: "worker", Name: "KEY_B", Value: "b"},
		{TenantID: "t3", ProjectID: "p3", Environment: "preview-42", App: "web", Name: "KEY_C", Value: "c"},
	}

	for _, input := range inputs {
		t.Run(fmt.Sprintf("%s/%s/%s", input.TenantID, input.Environment, input.Name), func(t *testing.T) {
			syncer := NewFakeSecretSyncer()
			result, err := syncer.CreateSecret(context.Background(), input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ExternalSecret.CreationPolicy != "Owner" {
				t.Errorf("CreationPolicy: got %q, want %q",
					result.ExternalSecret.CreationPolicy, "Owner")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 6: Refresh interval is set for periodic re-sync
//
// ESO pattern: ExternalSecret.spec.refreshInterval controls how often ESO
// re-reads from the store and updates the target Secret.
//
// LoopZero adaptation: All ExternalSecrets use a 30s refresh interval. This means
// if a secret is updated on the mgmt cluster, the runtime copy converges
// within 30 seconds. This is LoopZero's eventual consistency window for secrets.
// ---------------------------------------------------------------------------
func TestSecretSync_RefreshIntervalIsConfigured(t *testing.T) {
	syncer := NewFakeSecretSyncer()
	result, err := syncer.CreateSecret(context.Background(), SecretSyncInput{
		TenantID:    "acme",
		ProjectID:   "proj",
		Environment: "production",
		App:         "api",
		Name:        "SECRET_KEY",
		Value:       "val",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExternalSecret.RefreshInterval != "30s" {
		t.Errorf("RefreshInterval: got %q, want %q", result.ExternalSecret.RefreshInterval, "30s")
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 7: Namespace naming convention is deterministic
//
// ESO pattern: ExternalSecrets are namespace-scoped. The namespace determines
// the security boundary.
//
// LoopZero adaptation: Runtime namespace follows the pattern loopzero-{tenant}-{env}.
// This is critical because ESO's ClusterSecretStore can be configured with
// namespace conditions (namespaceSelector) to restrict which namespaces can
// access which secrets — the tenant label on the namespace is the isolation
// mechanism.
// ---------------------------------------------------------------------------
func TestSecretSync_NamespaceNamingConvention(t *testing.T) {
	tests := []struct {
		name     string
		tenant   string
		env      string
		wantNS   string
	}{
		{
			name:   "production namespace",
			tenant: "acme",
			env:    "production",
			wantNS: "loopzero-acme-production",
		},
		{
			name:   "staging namespace",
			tenant: "acme",
			env:    "staging",
			wantNS: "loopzero-acme-staging",
		},
		{
			name:   "preview namespace with PR number",
			tenant: "acme",
			env:    "preview-42",
			wantNS: "loopzero-acme-preview-42",
		},
		{
			name:   "different tenant same env",
			tenant: "other-co",
			env:    "production",
			wantNS: "loopzero-other-co-production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer := NewFakeSecretSyncer()
			result, err := syncer.CreateSecret(context.Background(), SecretSyncInput{
				TenantID:    tt.tenant,
				ProjectID:   "proj",
				Environment: tt.env,
				App:         "api",
				Name:        "KEY",
				Value:       "val",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ExternalSecret.Namespace != tt.wantNS {
				t.Errorf("namespace: got %q, want %q", result.ExternalSecret.Namespace, tt.wantNS)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 8: ClusterSecretStore reference is always "loopzero-mgmt-store"
//
// ESO pattern: ExternalSecret.spec.secretStoreRef.kind determines whether a
// namespace-scoped SecretStore or cluster-scoped ClusterSecretStore is used.
//
// LoopZero adaptation: ALL ExternalSecrets reference "loopzero-mgmt-store" as a
// ClusterSecretStore. This is the single store that has credentials to read
// from the mgmt cluster's platform-system namespace. Using ClusterSecretStore
// (not SecretStore) means the store is shared across all tenant namespaces,
// with namespace isolation enforced by the store's conditions.
// ---------------------------------------------------------------------------
func TestSecretSync_AlwaysReferencesClusterSecretStore(t *testing.T) {
	syncer := NewFakeSecretSyncer()
	result, err := syncer.CreateSecret(context.Background(), SecretSyncInput{
		TenantID:    "any-tenant",
		ProjectID:   "any-project",
		Environment: "any-env",
		App:         "any-app",
		Name:        "ANY_KEY",
		Value:       "any-val",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ExternalSecret.StoreRef.Kind != "ClusterSecretStore" {
		t.Errorf("StoreRef.Kind: got %q, want %q",
			result.ExternalSecret.StoreRef.Kind, "ClusterSecretStore")
	}
	if result.ExternalSecret.StoreRef.Name != "loopzero-mgmt-store" {
		t.Errorf("StoreRef.Name: got %q, want %q",
			result.ExternalSecret.StoreRef.Name, "loopzero-mgmt-store")
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 9: Synced condition is set on success
//
// ESO pattern: ExternalSecret sets Ready condition to True/False with reason.
// The testCase.checkCondition callback validates this.
//
// LoopZero adaptation: The AppSecret CRD has a "Synced" condition type. When sync
// succeeds, Synced=True with reason "Ready". This allows `loopzero secret list`
// to show sync status and `loopzero status` to aggregate health.
// ---------------------------------------------------------------------------
func TestSecretSync_SyncedConditionOnSuccess(t *testing.T) {
	syncer := NewFakeSecretSyncer()
	result, err := syncer.CreateSecret(context.Background(), SecretSyncInput{
		TenantID:    "acme",
		ProjectID:   "proj",
		Environment: "production",
		App:         "api",
		Name:        "DATABASE_URL",
		Value:       "postgres://host:5432/db",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundSynced := false
	for _, c := range result.Conditions {
		if c.Type == "Synced" {
			foundSynced = true
			if c.Status != "True" {
				t.Errorf("Synced condition status: got %q, want %q", c.Status, "True")
			}
			if c.Reason != "Ready" {
				t.Errorf("Synced condition reason: got %q, want %q", c.Reason, "Ready")
			}
		}
	}
	if !foundSynced {
		t.Error("expected Synced condition to be present")
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 10: Target secret name follows deterministic convention
//
// The target K8s Secret name on the runtime cluster (created by ESO from the
// ExternalSecret) must match the envFrom secretRef in LoopZero's manifest renderer.
// This is the glue between the secret sync and the deployment — if names
// diverge, the pod cannot mount the secret.
//
// Convention: user-secrets-{tenant}-{project}-{env}-{app}
// ---------------------------------------------------------------------------
func TestSecretSync_TargetSecretNameConvention(t *testing.T) {
	tests := []struct {
		name       string
		input      SecretSyncInput
		wantTarget string
	}{
		{
			name: "standard naming",
			input: SecretSyncInput{
				TenantID:    "acme",
				ProjectID:   "web-app",
				Environment: "production",
				App:         "api",
				Name:        "ANY",
				Value:       "v",
			},
			wantTarget: "user-secrets-acme-web-app-production-api",
		},
		{
			name: "staging environment",
			input: SecretSyncInput{
				TenantID:    "acme",
				ProjectID:   "web-app",
				Environment: "staging",
				App:         "worker",
				Name:        "ANY",
				Value:       "v",
			},
			wantTarget: "user-secrets-acme-web-app-staging-worker",
		},
		{
			name: "preview environment",
			input: SecretSyncInput{
				TenantID:    "acme",
				ProjectID:   "mono",
				Environment: "preview-99",
				App:         "web",
				Name:        "ANY",
				Value:       "v",
			},
			wantTarget: "user-secrets-acme-mono-preview-99-web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer := NewFakeSecretSyncer()
			result, err := syncer.CreateSecret(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ExternalSecret.TargetName != tt.wantTarget {
				t.Errorf("target name: got %q, want %q",
					result.ExternalSecret.TargetName, tt.wantTarget)
			}
		})
	}
}
