// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// A not-found error must name the cluster that was searched.
//
// B-024, measured 2026-08-10: `loop-runtime-ops seed patch-kratos` failed with
// `get configmap loop-runtime-auth/ory-kratos-config: configmaps "ory-kratos-config" not found`
// — and the ConfigMap existed. The seed was talking to another cluster, because
// `k3d:secrets:create` switches the active context as a side effect and seeds
// inherit whatever is active. Naming the context is what separates "the resource
// does not exist" from "you are looking in the wrong place".
func TestGetConfigMap_NotFoundNamesTheContext(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClientForContext(c, "k3d-loop-runtime-secrets")

	_, err := k.GetConfigMap(context.Background(), "loop-runtime-auth", "ory-kratos-config")
	if err == nil {
		t.Fatal("want a not-found error")
	}
	if !strings.Contains(err.Error(), "k3d-loop-runtime-secrets") {
		t.Errorf("the message does not name the context:\n  %v", err)
	}
}

func TestGetSecret_NotFoundNamesTheContext(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClientForContext(c, "k3d-loop-runtime-secrets")

	_, err := k.GetSecret(context.Background(), "loop-runtime-system", "absent")
	if err == nil {
		t.Fatal("want a not-found error")
	}
	if !strings.Contains(err.Error(), "k3d-loop-runtime-secrets") {
		t.Errorf("the message does not name the context:\n  %v", err)
	}
}

// With no declared context the message reads as it always did: the test sites
// using the old constructor need not change, and a message with an empty
// context dangling from it would be worse than the original.
func TestNotFound_WithoutAContextTheMessageIsUnchanged(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClient(c)

	_, err := k.GetConfigMap(context.Background(), "loop-runtime-auth", "ory-kratos-config")
	if err == nil {
		t.Fatal("want a not-found error")
	}
	if strings.Contains(err.Error(), "(context ") {
		t.Errorf("with no declared context the message must not invent one:\n  %v", err)
	}
	if !strings.Contains(err.Error(), "loop-runtime-auth/ory-kratos-config") {
		t.Errorf("the message lost the resource it always named:\n  %v", err)
	}
}

// The coverage has to reach EVERY operation, not only the two from the
// original report. Review finding 4: 5 of 11 sites had no context — including
// RolloutRestartDeployment, called by the patch-kratos seed that B-024 was
// about.
func TestEveryOperationNamesTheContext(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClientForContext(c, "k3d-loop-runtime-secrets")
	ctx := context.Background()

	cases := map[string]error{}
	_, errCM := k.GetConfigMap(ctx, "ns", "absent")
	cases["GetConfigMap"] = errCM
	_, errS := k.GetSecret(ctx, "ns", "absent")
	cases["GetSecret"] = errS
	cases["RolloutRestartDeployment"] = k.RolloutRestartDeployment(ctx, "ns", "absent")
	_, errSvc := k.GetService(ctx, "ns", "absent")
	cases["GetService"] = errSvc

	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: want an error about the absent resource", name)
			continue
		}
		if !strings.Contains(err.Error(), "k3d-loop-runtime-secrets") {
			t.Errorf("%s does not name the context:\n  %v", name, err)
		}
	}
}

// The resource identifier must NOT vanish when the context is added. Review
// finding 5: a mutant deleting `namespace/name` from contextual messages passed
// the whole suite, because the tests only guarded the new suffix.
func TestContextIsAddedWithoutLosingTheResource(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClientForContext(c, "k3d-loop-runtime-secrets")

	_, err := k.GetConfigMap(context.Background(), "loop-runtime-auth", "ory-kratos-config")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "loop-runtime-auth/ory-kratos-config") {
		t.Errorf("the resource vanished from the message when the context was added:\n  %v", err)
	}
}
