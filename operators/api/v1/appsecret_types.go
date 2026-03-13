package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppSecret is the Schema for the appsecrets API.
// It represents a secret that the AppSecretOperator synchronizes from the
// management cluster to a workload cluster namespace. See spec section 7.3.2.
type AppSecret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppSecretSpec   `json:"spec,omitempty"`
	Status AppSecretStatus `json:"status,omitempty"`
}

// AppSecretSpec defines the desired state of an AppSecret.
type AppSecretSpec struct {
	// TenantID is the owning tenant identifier.
	TenantID string `json:"tenantId"`

	// ProjectID is the project this secret belongs to.
	ProjectID string `json:"projectId"`

	// Environment is the target environment (e.g., "production", "staging").
	Environment string `json:"environment"`

	// App is the app within the project that uses this secret.
	App string `json:"app"`

	// SecretRef references the source Kubernetes Secret in the management cluster.
	SecretRef SecretReference `json:"secretRef"`

	// Rotation configures secret rotation policy.
	Rotation RotationConfig `json:"rotation,omitempty"`
}

// SecretReference identifies a Kubernetes Secret by name and namespace.
type SecretReference struct {
	// Name is the name of the source Secret.
	Name string `json:"name"`

	// Namespace is the namespace of the source Secret.
	Namespace string `json:"namespace"`
}

// RotationConfig defines the rotation policy for a secret.
type RotationConfig struct {
	// Enabled indicates whether automatic rotation tracking is enabled.
	Enabled bool `json:"enabled,omitempty"`

	// MaxAgeHours is the maximum age of the secret before rotation is due.
	MaxAgeHours int `json:"maxAgeHours,omitempty"`

	// NotifyBeforeHours is how many hours before expiry to start alerting.
	NotifyBeforeHours int `json:"notifyBeforeHours,omitempty"`
}

// AppSecretStatus defines the observed state of an AppSecret.
type AppSecretStatus struct {
	// SyncedAt is the last time the secret was successfully synchronized.
	SyncedAt *metav1.Time `json:"syncedAt,omitempty"`

	// TargetNamespace is the workload namespace where the secret is synced.
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// Hash is the SHA256 hash of the source secret data.
	Hash string `json:"hash,omitempty"`

	// WorkloadHash is the SHA256 hash of the target secret in the workload namespace.
	WorkloadHash string `json:"workloadHash,omitempty"`

	// HashMatch indicates whether the source and target hashes match.
	HashMatch bool `json:"hashMatch"`

	// RotationDueAt is when the next rotation is due (if rotation is enabled).
	RotationDueAt *metav1.Time `json:"rotationDueAt,omitempty"`

	// Conditions represent the latest available observations.
	// Condition types: Synced, RotationCurrent.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AppSecretList contains a list of AppSecret resources.
type AppSecretList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppSecret `json:"items"`
}
