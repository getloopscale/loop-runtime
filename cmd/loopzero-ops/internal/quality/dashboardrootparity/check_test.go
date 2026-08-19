// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package dashboardrootparity_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dashboardrootparity"
)

// tree escreve um Caddyfile e um runbook mínimos.
func tree(t *testing.T, caddy, runbook string) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("infra/local/Caddyfile", caddy)
	mk("docs/operations/local-public-dashboard.md", runbook)
	return root
}

// O runbook tem de nomear o MESMO caminho que a borda serve.
//
// B-039, medido 2026-08-18: implantar um conserto no painel exigiu descobrir onde o código
// mora no droplet — e o caminho óbvio, `~/loopzero-cloud`, é um clone do **umbrella**, não do
// produto. Os dois clones que pareciam certos eram ad-hoc, com refspec restrito. O caminho
// que a borda de fato serve, `~/loopzero-cloud/loopzero-cloud/dashboard/dist`, funciona e não estava
// em runbook nenhum.
//
// Documentação que ninguém amarra apodrece: quem mudar o `root *` do Caddyfile deixaria o
// runbook apontando para um caminho que já não é servido, e a próxima pessoa pagaria a mesma
// descoberta com um documento que confirma o erro.
func TestRunbookNamesTheServedRoot(t *testing.T) {
	root := tree(t,
		"app-dev.loopzero.example {\n  root * /home/paulo/loopzero-cloud/loopzero-cloud/dashboard/dist\n}\n",
		"# runbook\n\nO painel é construído em `~/loopzero-cloud/loopzero-cloud`.\n")

	if _, err := dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: root}); err != nil {
		t.Fatalf("runbook e Caddyfile concordam; veio %v", err)
	}
}

// Divergência é o defeito: o runbook manda a pessoa para onde a borda não serve.
func TestDivergenceIsRefused(t *testing.T) {
	root := tree(t,
		"app-dev.loopzero.example {\n  root * /home/paulo/loopzero-cloud/loopzero-cloud/dashboard/dist\n}\n",
		"# runbook\n\nO painel é construído em `~/.buildmeta-cloud/loopzero-cloud`.\n")

	out, err := dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: root})
	if !errors.Is(err, dashboardrootparity.ErrDivergent) {
		t.Fatalf("esperava ErrDivergent, veio %v", err)
	}
	if out.Served == "" || out.Documented == "" {
		t.Errorf("o erro precisa nomear os dois lados: %+v", out)
	}
}

// Runbook que não menciona caminho algum é o estado ANTERIOR ao item, e não pode passar em
// silêncio — foi ele que custou a descoberta.
func TestSilentRunbookIsRefused(t *testing.T) {
	root := tree(t,
		"app-dev.loopzero.example {\n  root * /home/paulo/loopzero-cloud/loopzero-cloud/dashboard/dist\n}\n",
		"# runbook\n\nNada sobre onde o painel é construído.\n")

	if _, err := dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: root}); !errors.Is(err, dashboardrootparity.ErrUndocumented) {
		t.Fatalf("esperava ErrUndocumented, veio %v", err)
	}
}

// Caddyfile sem `root *` significa que a borda não serve arquivo — não há paridade a
// verificar, e reportar "de acordo" seria verde sobre nada.
func TestNoServedRootIsRefusedNotPassed(t *testing.T) {
	root := tree(t, "app-dev.loopzero.example {\n  reverse_proxy localhost:1\n}\n", "# runbook\n")

	if _, err := dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: root}); !errors.Is(err, dashboardrootparity.ErrNothingServed) {
		t.Fatalf("esperava ErrNothingServed, veio %v", err)
	}
}

// A população real: a árvore deste repositório tem de concordar consigo mesma.
func TestRealTreeAgrees(t *testing.T) {
	out, err := dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: filepath.Join("..", "..", "..", "..", "..")})
	if err != nil {
		t.Fatalf("árvore real diverge: %v (servido=%q documentado=%q)", err, out.Served, out.Documented)
	}
	t.Logf("população real: servido=%q documentado=%q", out.Served, out.Documented)
}
