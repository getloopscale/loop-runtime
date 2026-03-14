package unit

import (
	"context"
	"testing"

	"github.com/loopzero/loopzero/api/internal/k8s"
	"github.com/loopzero/loopzero/api/internal/routes"
)

func TestKubeTenantCRManagerImplementsInterface(t *testing.T) {
	// Compile-time check: TenantCRManager implements routes.TenantCRManager.
	var _ routes.TenantCRManager = (*k8s.TenantCRManager)(nil)
}

func TestKubeTenantCRManagerEnsureTenant(t *testing.T) {
	t.Run("returns nil for Sprint 1 stub", func(t *testing.T) {
		mgr := &k8s.TenantCRManager{}

		err := mgr.EnsureTenant(context.Background(), "acme", routes.TenantCRSpec{Plan: "starter"})

		if err != nil {
			t.Fatalf("expected nil error from stub, got %v", err)
		}
	})

	t.Run("works with logger set", func(t *testing.T) {
		// Use slog default — just ensure it does not panic
		mgr := &k8s.TenantCRManager{
			Logger: nil, // nil logger should also be safe
		}

		err := mgr.EnsureTenant(context.Background(), "acme", routes.TenantCRSpec{Plan: "growth"})

		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
}
