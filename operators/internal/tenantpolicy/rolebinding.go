// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenantpolicy

import (
	"context"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ApplySingleSARoleBinding is the canonical RoleBinding create-or-update that
// binds one ServiceAccount to one Role in a namespace. Shared by the tenant
// build-namespace RBAC and the environment runtime RBAC reconcilers.
func ApplySingleSARoleBinding(ctx context.Context, c client.Client, namespace, bindingName, roleName, saName string) error {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, rb, func() error {
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		}
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: namespace,
			},
		}
		return nil
	})
	return err
}
