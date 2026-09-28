// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The rollout settle deadlines, named. They DIFFER on purpose, and each
// difference has a reason that only survives if written down:
//
//   - loopzeroAPISettle — loopzero-api starts heavy (migrations plus connections to
//     several clusters), so it is the longest.
//   - corednsSettle — the shortest: a light, stateless system Deployment that
//     restarts fast.
//
// They used to be seven literals across six files, and an eighth caller would
// have written its own — the shape spreads by being read. None is the
// EFFECTIVE deadline: Deps.settleTimeout resolves that, so a hermetic
// environment can inject its own without changing the production value.
const (
	loopzeroAPISettle = 120 * time.Second
	corednsSettle = 60 * time.Second
)

// waitForRolloutSettled polls the Deployment until the controller has observed
// the latest spec and ready/updated replicas equal the desired count, or the
// timeout elapses. A timeout is returned as an error: a seed that restarted a
// workload has not done its job until the workload is ready again.
func waitForRolloutSettled(ctx context.Context, cli ctrlclient.Client,
	namespace, name string, timeout time.Duration,
) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			var d appsv1.Deployment
			if err := cli.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: name}, &d); err != nil {
				return false, err
			}
			want := int32(1)
			if d.Spec.Replicas != nil {
				want = *d.Spec.Replicas
			}
			return d.Status.ObservedGeneration >= d.Generation &&
				d.Status.ReadyReplicas == want && d.Status.UpdatedReplicas == want, nil
		})
}
