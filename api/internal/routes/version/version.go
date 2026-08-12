// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package version

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// F-07 (CHG-2026-04-29-DOGFOOD T5.2) — /api/v1/version é endpoint público
// (sem auth) que retorna metadata do binário. Usado pelo skill de dogfood +
// smoke checks externos. Os 4 build flags são injetados via ldflags em
// `cmd/main.go`; quando ausentes (ex: `go run`), retornam strings vazias /
// "dev" — comportamento documentado pra que o /version sempre responda 200
// independente do build environment.

// Version metadata — populados via ldflags em build (ver api/Dockerfile).
//
//	go build -ldflags "\
//	  -X github.com/getloopscale/loopzero/api/internal/routes.versionTag=$(git describe --tags) \
//	  -X github.com/getloopscale/loopzero/api/internal/routes.versionCommit=$(git rev-parse HEAD) \
//	  -X github.com/getloopscale/loopzero/api/internal/routes.versionBuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	versionTag       = "dev"
	versionCommit    = "unknown"
	versionBuildDate = "unknown"
)

// VersionResponse is the schema returned by GET /api/v1/version.
type VersionResponse struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// fleetVersionResponse e' o schema que o painel de status do dashboard consome.
//
// Os nomes dos campos NAO sao escolha nossa: sao o contrato que
// loopzero-cloud/internal/serviceversions/probe.go ja' fala com cinco servicos
// (`version` + `built_at`). O engine ficava fora do painel por divergir dele em
// path E em nome de campo.
type fleetVersionResponse struct {
	Version string `json:"version"`
	BuiltAt string `json:"built_at"`
}

// FleetVersionHandler serve GET /v1/version — o contrato da frota de status.
//
// Coexiste com VersionHandler de proposito. `/api/v1/version` e' contrato publico
// estavel do CLI (Rule 5): renomear `build_date` para `built_at` ali quebraria
// consumidores sem bump maior. Os dois leem os MESMOS globais, entao servem a
// mesma verdade — um teste trava essa igualdade, porque duas fontes que PODEM
// divergir acabam divergindo.
//
// Sem autenticacao, como o /api/v1/version: metadado de build nao e' segredo, e o
// coletor sonda o alvo sem qualquer sessao.
func FleetVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Fallback explicito: celula vazia no painel sobre um servico que respondeu
		// `ok` sugere ausencia de dado, quando o que houve foi build sem ldflags.
		_ = json.NewEncoder(w).Encode(fleetVersionResponse{
			Version: orUnknown(versionTag),
			BuiltAt: orUnknown(versionBuildDate),
		})
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// VersionHandler returns a chi.HandlerFunc for /api/v1/version.
// No authentication required. Stable schema (per Rule 5 — JSON contract).
func VersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := VersionResponse{
			Version:   versionTag,
			Commit:    versionCommit,
			BuildDate: versionBuildDate,
			GoVersion: runtime.Version(),
			Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
