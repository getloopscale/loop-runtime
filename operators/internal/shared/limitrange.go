// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shared

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/loopzero/loopzero/operators/internal/constants"
)

// ApplyTenantLimitRange creates or updates the canonical tenant-limits
// LimitRange (Container scope, Default + DefaultRequest CPU/Memory from
// platform constants) in the given namespace. Phase 3 cross-package helper:
// previously inlined 17-line CreateOrUpdate block in 2 sites:
//   - internal/preview/preview_resources.go (preview namespace)
//   - internal/tenant/environment_quota.go (runtime tenant namespace)
//
// Note: tenant_build.go has a STRUCTURALLY-similar LimitRange block but uses
// different constants (LimitRangeBuild + BuildLimit*) — that one stays inline
// because folding into this helper would require either parameterizing every
// constant (over-abstracted, YAGNI) or losing the build vs runtime distinction.
//
// errCtx is the human-readable error context (e.g., "preview limitrange",
// "limitrange") so each call site preserves its existing error-wrap message.
func ApplyTenantLimitRange(ctx context.Context, c client.Client, nsName, errCtx string) error {
	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.LimitRangeTenant,
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, lr, func() error {
		lr.Spec.Limits = []corev1.LimitRangeItem{
			{
				Type: corev1.LimitTypeContainer,
				Default: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(constants.TenantLimitDefaultCPU),
					corev1.ResourceMemory: resource.MustParse(constants.TenantLimitDefaultMemory),
				},
				DefaultRequest: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(constants.TenantLimitDefaultRequestCPU),
					corev1.ResourceMemory: resource.MustParse(constants.TenantLimitDefaultRequestMemory),
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s/%s: %w", errCtx, nsName, constants.LimitRangeTenant, err)
	}
	return nil
}
