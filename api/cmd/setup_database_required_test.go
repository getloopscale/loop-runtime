// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/config"
)

// A ausência de `DATABASE_URL` é erro de CONFIGURAÇÃO, não degradação — decidido
// em MEET-ASYNC-AMENDMENT-2026-08-17-no-database-is-a-config-error.
//
// Três medições sustentam a decisão: a degradação nunca executou (ela entrava em
// pânico três funções adiante, `B-104`); o alcance é 27 das 36 superfícies do
// roteador; e o único caso registrado dela em quatro meses foi um bootstrap
// quebrado em silêncio (B35, 2026-04-28, comentado no próprio deployment).

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSetupDatabase_MissingURLIsATypedBootError(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	deps, err := setupDatabase(config.Config{}, quietLogger())

	if err == nil {
		t.Fatal("DATABASE_URL ausente deve falhar o arranque: nenhuma espera futura produz o valor, e 27 das 36 superfícies dependem dele")
	}
	if !errors.Is(err, ErrDatabaseURLNotSet) {
		t.Fatalf("o erro precisa ser reconhecível por quem trata o arranque, não uma string; got %T: %v", err, err)
	}
	if deps != nil {
		t.Fatalf("nada foi construído; devolver deps não-nil junto de erro convida o chamador a usá-los: %+v", deps)
	}
}

// A mensagem é o único diagnóstico que o operador recebe no primeiro
// `kubectl logs`, e o contraditor do MEET aceitou o `CrashLoopBackOff`
// justamente porque ela nomeia a variável.
func TestSetupDatabase_MissingURLErrorNamesTheVariable(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	_, err := setupDatabase(config.Config{}, quietLogger())
	if err == nil {
		t.Fatal("esperava erro")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("a mensagem precisa nomear a variável que falta; got %q", err.Error())
	}
}

// A fronteira oposta: com a variável presente, `setupDatabase` NÃO pode recusar
// por esta razão. Sem esta asserção, endurecer o arranque viraria uma regra que
// nenhum ambiente satisfaz — e o valor aqui é deliberadamente inválido, para que
// o teste não precise de um Postgres para provar o que precisa provar.
func TestSetupDatabase_PresentURLIsNotRejectedAsMissing(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")

	_, err := setupDatabase(config.Config{}, quietLogger())
	if errors.Is(err, ErrDatabaseURLNotSet) {
		t.Fatalf("uma URL presente nunca é 'ausente'; got %v", err)
	}
}
