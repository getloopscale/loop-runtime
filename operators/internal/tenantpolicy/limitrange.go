// Package tenantpolicy applies what the platform IMPOSES on a tenant namespace:
// resource ceilings (LimitRange), who may talk to whom (NetworkPolicy) and which
// service account may act (RoleBinding).
//
// # Why this left the shared package (B-061)
//
// `ApplyTenantLimitRange` and `ApplyTenantNetworkPolicies` decide **what a
// customer may consume and whom it may talk to**, and they lived in a package
// named `reconcilehelpers` — cohesion 2/5, the only "helpers" name in the whole
// Go tree, imported by the four operator binaries.
//
// A name that refuses nothing, with that reach, is where the next mix-up lands.
// This name refuses: a reconcile helper has no reason to be added here.
package tenantpolicy

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/getloopscale/loopzero/operators/internal/constants"
)

// LimitRangeContainerDefaults bundles the 4 strings (default + request, CPU +
// memory) needed to fully describe a Container-scoped LimitRange's
// Default/DefaultRequest fields. Quantities are parsed via resource.MustParse
// inside ApplyContainerLimitRange so the caller passes string constants
// directly (same shape as constants.TenantLimit* / constants.BuildLimit*).
type LimitRangeContainerDefaults struct {
	CPU           string // Default container CPU limit
	Memory        string // Default container memory limit
	RequestCPU    string // Default container CPU request
	RequestMemory string // Default container memory request
}

// ApplyContainerLimitRange creates or updates a Container-scoped LimitRange
// with the given name, applying the supplied Default + DefaultRequest values.
// Phase 3 v40: generalized form of ApplyTenantLimitRange that also covers
// build-namespace LimitRanges (different constants, identical structure).
//
// errCtx is the human-readable error context wrap prefix (e.g., "preview
// limitrange", "limitrange", "build limitrange") so each call site preserves
// its existing error-wrap message used by audit/log scans.
func ApplyContainerLimitRange(ctx context.Context, c client.Client, nsName, lrName, errCtx string, d LimitRangeContainerDefaults) error {
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lrName,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, lr, func() error {
		lr.Spec.Limits = []corev1.LimitRangeItem{
			{
				Type: corev1.LimitTypeContainer,
				Default: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(d.CPU),
					corev1.ResourceMemory: resource.MustParse(d.Memory),
				},
				DefaultRequest: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(d.RequestCPU),
					corev1.ResourceMemory: resource.MustParse(d.RequestMemory),
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s/%s: %w", errCtx, nsName, lrName, err)
	}
	return nil
}

// ApplyTenantLimitRange creates or updates the canonical tenant-limits
// LimitRange in the given namespace, using platform-wide tenant constants.
// Convenience wrapper over ApplyContainerLimitRange — kept for call-site
// readability where the tenant default IS the requirement.
//
// Used by:
//   - internal/preview/preview_resources.go (preview namespace)
//   - internal/tenant/environment_quota.go  (runtime tenant namespace)
func ApplyTenantLimitRange(ctx context.Context, c client.Client, nsName, errCtx string) error {
	return ApplyContainerLimitRange(ctx, c, nsName, constants.LimitRangeTenant, errCtx, LimitRangeContainerDefaults{
		CPU:           constants.TenantLimitDefaultCPU,
		Memory:        constants.TenantLimitDefaultMemory,
		RequestCPU:    constants.TenantLimitDefaultRequestCPU,
		RequestMemory: constants.TenantLimitDefaultRequestMemory,
	})
}
