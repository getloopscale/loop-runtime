// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Contrato da FROTA de status — o que o painel /status do dashboard consome.
//
// POR QUE UM SEGUNDO ENDPOINT, e nao um ajuste no /api/v1/version:
//
// O coletor do loopzero-cloud (internal/serviceversions/probe.go) chama
// `GET {base}/v1/version` e le' `{version, built_at}`. Cinco servicos ja' falam
// esse contrato. O engine falava outro — `/api/v1/version` com `build_date` —
// entao ficava de fora do painel por divergencia de path E de campo.
//
// Renomear `build_date` para `built_at` no endpoint existente quebraria a Rule 5
// (o schema do --json e' contrato estavel e nao muda sem bump maior). Os dois
// endpoints coexistem servindo a MESMA verdade em dois formatos: um e' o contrato
// publico do CLI, o outro e' o contrato da frota.
func TestFleetVersionHandler_SpeaksTheFleetContract(t *testing.T) {
	rr := httptest.NewRecorder()
	FleetVersionHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	// Decodifica no formato exato que o coletor espera. Um campo com outro nome
	// chega como zero-value aqui, que e' precisamente a falha a travar.
	var got struct {
		Version string `json:"version"`
		BuiltAt string `json:"built_at"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version == "" {
		t.Error("version vazio — o painel mostraria uma celula em branco para um servico `ok`")
	}
	if got.BuiltAt == "" {
		t.Error("built_at vazio — a coluna `Built` do painel ficaria em branco")
	}
}

// A mesma verdade nos dois endpoints. Se divergirem, um dos dois esta' mentindo, e
// o painel e o CLI passariam a discordar sobre qual binario esta' rodando.
func TestFleetVersionHandler_AgreesWithTheCLIContract(t *testing.T) {
	fleetRec := httptest.NewRecorder()
	FleetVersionHandler().ServeHTTP(fleetRec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
	cliRec := httptest.NewRecorder()
	VersionHandler().ServeHTTP(cliRec, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))

	var fleet struct {
		Version string `json:"version"`
		BuiltAt string `json:"built_at"`
	}
	var cli VersionResponse
	if err := json.NewDecoder(fleetRec.Body).Decode(&fleet); err != nil {
		t.Fatalf("decode fleet: %v", err)
	}
	if err := json.NewDecoder(cliRec.Body).Decode(&cli); err != nil {
		t.Fatalf("decode cli: %v", err)
	}
	if fleet.Version != cli.Version {
		t.Errorf("version diverge: frota=%q cli=%q", fleet.Version, cli.Version)
	}
	if fleet.BuiltAt != cli.BuildDate {
		t.Errorf("data de build diverge: frota=%q cli=%q", fleet.BuiltAt, cli.BuildDate)
	}
}

// Sem ldflags os globais caem nos defaults declarados. O painel precisa de um
// valor legivel — nunca de uma celula vazia sobre um servico que respondeu `ok`.
func TestFleetVersionHandler_NeverEmptyWithoutLdflags(t *testing.T) {
	origTag, origBuild := versionTag, versionBuildDate
	versionTag, versionBuildDate = "", ""
	defer func() { versionTag, versionBuildDate = origTag, origBuild }()

	rr := httptest.NewRecorder()
	FleetVersionHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	var got struct {
		Version string `json:"version"`
		BuiltAt string `json:"built_at"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version == "" || got.BuiltAt == "" {
		t.Errorf("campos vazios sem ldflags: version=%q built_at=%q", got.Version, got.BuiltAt)
	}
}
