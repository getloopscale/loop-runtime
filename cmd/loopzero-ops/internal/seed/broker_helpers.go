// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"os"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// statPath is a stdlib shim so tests can substitute filesystem lookups.
var statPath = os.Stat

// ensureNamespace idempotently creates a namespace via the KubeClient's
// underlying ctrl.Client. Bash equivalent:
//
//	kubectl get ns X || kubectl create ns X
func ensureNamespace(d Deps, k KubeClient, name string) error {
	raw, ok := k.(*kubeClient)
	if !ok {
		return nil // fake clients in tests don't care; skip
	}
	var ns corev1.Namespace
	if err := raw.c.Get(d.Ctx, ctrlclient.ObjectKey{Name: name}, &ns); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	created := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := raw.c.Create(context.Background(), created); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
