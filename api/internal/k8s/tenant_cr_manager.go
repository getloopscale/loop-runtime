// Package k8s provides Kubernetes client implementations.
// For Sprint 1, the TenantCRManager is a stub that logs the intent.
// Real k8s client integration comes when clusters are wired.
package k8s

import (
	"context"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// TenantCRManager manages Tenant custom resources in Kubernetes.
// It implements routes.TenantCRManager (DIP).
//
// For Sprint 1, this is a stub that logs the intent and returns nil.
// The real implementation will use a dynamic client or typed client
// when the Tenant CRD (defined in operators/api/v1/) is available.
type TenantCRManager struct {
	Logger *slog.Logger
}

// EnsureTenant creates or updates a Tenant CR in Kubernetes.
// Sprint 1 stub: logs the intent and returns nil.
func (m *TenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec routes.TenantCRSpec) error {
	if m.Logger != nil {
		m.Logger.Info("ensuring tenant CR (stub)", "tenantID", tenantID, "plan", spec.Plan)
	}
	return nil
}
