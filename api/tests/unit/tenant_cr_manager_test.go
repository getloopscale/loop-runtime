package unit

import (
	"testing"

	"github.com/loopzero/loopzero/api/internal/k8s"
	"github.com/loopzero/loopzero/api/internal/routes"
)

func TestKubeTenantCRManagerImplementsInterface(t *testing.T) {
	// Compile-time check: TenantCRManager implements routes.TenantCRManager.
	var _ routes.TenantCRManager = (*k8s.TenantCRManager)(nil)
}
