// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environment

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/cellrouting"
)

// B-074. Apagar o Environment CR direto deixava a Application orfa: o reaper
// `pruneOrphanPreviewApps` so' apaga Application FORA de `expectedApplicationNames`, e esse
// conjunto vem de `Tenant.Spec.Environments` pela tupla (Project, Name). Com a tupla ainda no
// spec, a Application continua ESPERADA e o reaper nunca a alcanca — enquanto o Namespace para
// onde ela aponta ja foi apagado.
//
// Registro T2: knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-19-environment-delete-orphans-application.md
func tenantComDoisAmbientes() *loopzerov1.Tenant {
	return &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-b074", Namespace: "default"},
		Spec: loopzerov1.TenantSpec{
			Environments: []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "cell-0", Project: "loja"},
				{Name: "staging", Cluster: "cell-0", Project: "loja"},
			},
		},
	}
}

func ambienteSendoApagado() *loopzerov1.Environment {
	agora := metav1.NewTime(time.Now())
	return &loopzerov1.Environment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "env-b074",
			Namespace:         "default",
			DeletionTimestamp: &agora,
			Finalizers:        []string{environmentFinalizer},
		},
		Spec: loopzerov1.EnvironmentCRSpec{
			TenantRef:   loopzerov1.TenantRef{Name: "tn-b074"},
			Environment: "staging",
			Project:     "loja",
			Namespace:   loopzerov1.NamespaceSpec{Name: "loopzero-tn-b074-loja-staging"},
		},
		Status: loopzerov1.EnvironmentCRStatus{Phase: loopzerov1.EnvironmentPhaseDeleting},
	}
}

func TestApagarEnvironmentRemoveATuplaDoTenantParaOReaperAlcancarAApplication(t *testing.T) {
	s := t31TestScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(tenantComDoisAmbientes(), ambienteSendoApagado()).
		WithStatusSubresource(&loopzerov1.Environment{}, &loopzerov1.Tenant{}).
		Build()

	r := &EnvironmentReconciler{Client: cl, RuntimeFactory: cellrouting.NewStaticRuntimeClientFactory(cl), Scheme: s}
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "env-b074", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var tn loopzerov1.Tenant
	if err := cl.Get(context.Background(),
		types.NamespacedName{Name: "tn-b074", Namespace: "default"}, &tn); err != nil {
		t.Fatalf("lendo o Tenant: %v", err)
	}

	for _, e := range tn.Spec.Environments {
		if e.Project == "loja" && e.Name == "staging" {
			t.Fatalf("a tupla (loja, staging) continua em Spec.Environments depois do delete — "+
				"a Application segue ESPERADA pelo reaper e fica orfa, apontando para um Namespace "+
				"que ja nao existe. Spec.Environments = %+v", tn.Spec.Environments)
		}
	}
	// E a contrapartida: apagar um ambiente nao pode levar os outros junto.
	if len(tn.Spec.Environments) != 1 || tn.Spec.Environments[0].Name != "production" {
		t.Errorf("Spec.Environments = %+v; a entrada de production nao era para ser tocada",
			tn.Spec.Environments)
	}
}

// Caso negativo (`rules/testing.md` § 4.1): o Tenant ja' nao existe. Nao ha' tupla para remover,
// e segurar o finalizer aqui travaria a delecao do Environment PARA SEMPRE — um conserto que
// fecha um vazamento abrindo um deadlock nao vale.
func TestApagarEnvironmentSemTenantNaoTravaADelecao(t *testing.T) {
	s := t31TestScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(ambienteSendoApagado()). // sem Tenant
		WithStatusSubresource(&loopzerov1.Environment{}).
		Build()

	r := &EnvironmentReconciler{Client: cl, RuntimeFactory: cellrouting.NewStaticRuntimeClientFactory(cl), Scheme: s}
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "env-b074", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile devolveu erro sem Tenant: %v — o finalizer nunca sai e o CR fica preso", err)
	}

	var env loopzerov1.Environment
	err := cl.Get(context.Background(),
		types.NamespacedName{Name: "env-b074", Namespace: "default"}, &env)
	if err == nil && len(env.Finalizers) > 0 {
		t.Errorf("o finalizer %v continua no Environment; sem Tenant nao ha' o que podar e a "+
			"delecao tinha de concluir", env.Finalizers)
	}
}
