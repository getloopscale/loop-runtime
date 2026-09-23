// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/surfaces"
)

func TestPublishSurfaces_ZeroParaDesligadaUmParaLigada(t *testing.T) {
	PublishSurfaces([]surfaces.Surface{
		{Name: "github_webhook", Enabled: false, Reason: "GITHUB_WEBHOOK_SECRET ausente"},
		{Name: "billing", Enabled: true},
	})

	if got := testutil.ToFloat64(SurfaceEnabled.WithLabelValues("github_webhook")); got != 0 {
		t.Errorf("superfície desligada = %v, esperado 0", got)
	}
	if got := testutil.ToFloat64(SurfaceEnabled.WithLabelValues("billing")); got != 1 {
		t.Errorf("superfície ligada = %v, esperado 1", got)
	}
}

func TestPublishSurfaces_LigadaEmiteSerieEmVezDeAusencia(t *testing.T) {
	// Ausência de série e zero são indistinguíveis para quem alerta. Uma
	// superfície LIGADA precisa emitir 1 explicitamente, senão o painel não
	// consegue diferenciar "está no ar" de "esta versão nem conhece esta
	// superfície" — que é a ambiguidade que o B-104 ataca.
	PublishSurfaces([]surfaces.Surface{{Name: "so_ligada", Enabled: true}})

	n := testutil.CollectAndCount(SurfaceEnabled)
	if n == 0 {
		t.Fatal("nenhuma série coletada para uma superfície ligada")
	}
}

func TestSurfaceEnabled_RazaoNaoEhLabel(t *testing.T) {
	// A razão é texto livre. Texto livre em label é cardinalidade ilimitada,
	// que é como um coletor cai. Ela pertence ao resumo de arranque.
	desc := SurfaceEnabled.WithLabelValues("qualquer").Desc().String()
	for _, proibido := range []string{"reason", "razao", "razão", "detail"} {
		if strings.Contains(strings.ToLower(desc), proibido) {
			t.Errorf("o descritor da métrica carrega o label proibido %q: %s", proibido, desc)
		}
	}
	if !strings.Contains(desc, "surface") {
		t.Errorf("o descritor não declara o label `surface`: %s", desc)
	}
}

func TestSurfaceEnabled_NomeCanonico(t *testing.T) {
	// O nome é contrato de operação: painel e alerta apontam para ele.
	if !strings.Contains(SurfaceEnabled.WithLabelValues("x").Desc().String(), "loopzero_api_surface_enabled") {
		t.Error("nome da métrica não é `loopzero_api_surface_enabled`")
	}
}

func TestPublishSurfaces_ListaVaziaNaoEntraEmPanico(t *testing.T) {
	// Um binário que não registrasse superfície nenhuma é improvável, mas
	// publicar nada tem de ser um no-op silencioso, não um crash no arranque.
	PublishSurfaces(nil)
	PublishSurfaces([]surfaces.Surface{})
}

var _ prometheus.Collector = SurfaceEnabled
