// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// O erro de recurso-nao-encontrado precisa nomear o cluster em que se procurou.
//
// B-024, medido 2026-08-10: `loopzero-ops seed patch-kratos` reprovou com
// `get configmap loopzero-auth/ory-kratos-config: configmaps "ory-kratos-config" not found` —
// e o ConfigMap existia. O seed estava falando com outro cluster, porque `k3d:secrets:create`
// troca o contexto ativo como efeito colateral e os seeds herdam o que estiver ativo.
//
// A mensagem mandava procurar o recurso certo no cluster errado. Nomear o contexto e o que
// separa "o recurso nao existe" de "voce esta olhando o lugar errado".
func TestGetConfigMap_NotFoundNamesTheContext(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClientForContext(c, "k3d-loopzero-secrets")

	_, err := k.GetConfigMap(context.Background(), "loopzero-auth", "ory-kratos-config")
	if err == nil {
		t.Fatal("esperava erro de nao-encontrado")
	}
	if !strings.Contains(err.Error(), "k3d-loopzero-secrets") {
		t.Errorf("a mensagem nao nomeia o contexto:\n  %v", err)
	}
}

func TestGetSecret_NotFoundNamesTheContext(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClientForContext(c, "k3d-loopzero-secrets")

	_, err := k.GetSecret(context.Background(), "loopzero-system", "nao-existe")
	if err == nil {
		t.Fatal("esperava erro de nao-encontrado")
	}
	if !strings.Contains(err.Error(), "k3d-loopzero-secrets") {
		t.Errorf("a mensagem nao nomeia o contexto:\n  %v", err)
	}
}

// Sem contexto declarado a mensagem fica como era. Os 96 sitios de teste que usam o
// construtor antigo nao precisam mudar por causa disto, e uma mensagem com um contexto
// vazio pendurado seria pior que a original.
func TestNotFound_WithoutAContextTheMessageIsUnchanged(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClient(c)

	_, err := k.GetConfigMap(context.Background(), "loopzero-auth", "ory-kratos-config")
	if err == nil {
		t.Fatal("esperava erro de nao-encontrado")
	}
	if strings.Contains(err.Error(), "contexto") {
		t.Errorf("sem contexto declarado a mensagem nao deve inventar um:\n  %v", err)
	}
	if !strings.Contains(err.Error(), "loopzero-auth/ory-kratos-config") {
		t.Errorf("a mensagem perdeu o recurso que sempre nomeou:\n  %v", err)
	}
}

// A cobertura tem de alcancar TODAS as operacoes, nao so as duas do relato original.
//
// Achado 4 da revisao: 5 de 11 sitios ficaram sem o contexto — inclusive
// `RolloutRestartDeployment`, chamado por `patch_kratos.go:94`, que e o proprio seed do
// relato do B-024. Mesmo modo de falha, mensagem sem contexto.
func TestEveryOperationNamesTheContext(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClientForContext(c, "k3d-loopzero-secrets")
	ctx := context.Background()

	casos := map[string]error{}
	_, errCM := k.GetConfigMap(ctx, "ns", "ausente")
	casos["GetConfigMap"] = errCM
	_, errS := k.GetSecret(ctx, "ns", "ausente")
	casos["GetSecret"] = errS
	casos["RolloutRestartDeployment"] = k.RolloutRestartDeployment(ctx, "ns", "ausente")

	for nome, err := range casos {
		if err == nil {
			t.Errorf("%s: esperava erro sobre recurso ausente", nome)
			continue
		}
		if !strings.Contains(err.Error(), "k3d-loopzero-secrets") {
			t.Errorf("%s nao nomeia o contexto:\n  %v", nome, err)
		}
	}
}

// O identificador do recurso NAO pode sumir quando o contexto entra.
//
// Achado 5 da revisao: um mutante que apagasse `namespace/name` das mensagens com contexto
// passava a suite inteira — os testes protegiam so o sufixo novo. Saber o cluster sem saber
// o recurso e meia mensagem.
func TestContextIsAddedWithoutLosingTheResource(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClientForContext(c, "k3d-loopzero-secrets")

	_, err := k.GetConfigMap(context.Background(), "loopzero-auth", "ory-kratos-config")
	if err == nil {
		t.Fatal("esperava erro")
	}
	if !strings.Contains(err.Error(), "loopzero-auth/ory-kratos-config") {
		t.Errorf("o recurso sumiu da mensagem quando o contexto entrou:\n  %v", err)
	}
}
