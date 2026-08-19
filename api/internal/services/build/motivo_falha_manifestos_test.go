// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"errors"
	"strings"
	"testing"
)

// A mensagem que o usuario recebia era a CONSTANTE "Failed to build manifests
// with custom domains": nomeava a etapa e nao o dominio, o app nem o motivo
// (B-152). O erro real ia so' para o log, entao quem recebia a falha nao tinha
// como saber qual app parou.
func TestMotivoDaFalhaNomeiaOAppDoErroReal(t *testing.T) {
	err := errors.New(`manifest_builder: missing image digest for app "api"`)
	msg := motivoDaFalhaDeManifestos(err)

	if !strings.Contains(msg, `"api"`) {
		t.Errorf("a mensagem nao nomeia o app do erro real:\n%s", msg)
	}
	if !strings.Contains(msg, "digest") {
		t.Errorf("a mensagem nao carrega o motivo:\n%s", msg)
	}
}

// O que torna seguro carregar o motivo: ele passa pelo mesmo higienizador que
// protege o evento de timeline. Sem isso, embutir texto de erro cru trocaria uma
// mensagem inutil por um vazamento da Regra Inquebravel 3.
func TestMotivoDaFalhaHigienizaTermoInterno(t *testing.T) {
	err := errors.New("ArgoCD sync failed while rendering manifests")
	msg := motivoDaFalhaDeManifestos(err)

	if strings.Contains(strings.ToLower(msg), "argocd") {
		t.Errorf("termo interno vazou para a mensagem do usuario:\n%s", msg)
	}
}

// Erro nulo nao pode produzir "Manifest build failed: " com sufixo vazio.
func TestMotivoDaFalhaComErroNulo(t *testing.T) {
	if msg := motivoDaFalhaDeManifestos(nil); strings.HasSuffix(msg, ": ") {
		t.Errorf("mensagem com sufixo vazio: %q", msg)
	}
}
