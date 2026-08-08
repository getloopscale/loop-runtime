// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argoappidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDetectaConstrucaoAdHoc usa as formas reais que existiam antes da correção,
// incluindo as duas de dentro do prune — que são as perigosas, porque decidem o que
// deletar com o finalizer de cascade ativo.
func TestDetectaConstrucaoAdHoc(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/tenant/tenant_gitops.go", `package tenant
func a() {
	appName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
	_ = appName
}
`)
	write(t, root, "api/internal/services/release/container_delivery.go", `package release
func b() {
	argoAppName := fmt.Sprintf("%s-%s", build.TenantID, build.Environment)
	_ = argoAppName
}
`)

	rep, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("as duas construções ad-hoc devem ser achadas; veio %d: %+v",
			len(rep.Findings), rep.Findings)
	}
}

// TestDetectaIdentidadeDeEnvironmentCR — #273: o Environment CR tinha a mesma forma e uma
// consequência pior (o projeto perdedor ficava sem namespace nenhum). O gate cobre os dois.
func TestDetectaIdentidadeDeEnvironmentCR(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/tenant/tenant_controller.go", `package tenant
func c() {
	envCRName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
	_ = envCRName
}
`)
	rep, _ := Check(root)
	if len(rep.Findings) != 1 {
		t.Errorf("a identidade do Environment CR também é escopo do gate (#273); findings=%+v",
			rep.Findings)
	}
}

// TestDetectaConjuntoDesiredDoGC — o `desired` do GC de Environment CRs decide o que
// DELETAR, e deletar um Environment CR apaga o namespace do tenant no cluster de runtime.
func TestDetectaConjuntoDesiredDoGC(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/tenant/tenant_controller.go", `package tenant
func d() {
	desired[fmt.Sprintf("%s-%s", tenant.Name, env.Name)] = true
}
`)
	rep, _ := Check(root)
	if rep.OK() {
		t.Error("o conjunto que protege da deleção precisa vir da fonte canônica (#273)")
	}
}

// TestNaoAcusaNomeDeAppDoRelease — release_convergence_loop.go monta "{project}-{app}"
// para os apps DENTRO de um release. Não tem termo de tenant, então não casa.
func TestNaoAcusaNomeDeAppDoRelease(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/internal/services/release/release_convergence_loop.go", `package release
func d() {
	appRef := fmt.Sprintf("%s-%s", release.ProjectID, app.Name)
	_ = appRef
}
`)
	rep, _ := Check(root)
	if !rep.OK() {
		t.Errorf("nome de app dentro do release não é nome de Application; findings=%+v", rep.Findings)
	}
}

// TestComentarioNaoEAchado — um comentário citando a forma antiga é história.
func TestComentarioNaoEAchado(t *testing.T) {
	root := t.TempDir()
	write(t, root, "operators/internal/tenant/tenant_gitops.go", `package tenant
// Antes de #271 isto era: appName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
func e() {}
`)
	rep, _ := Check(root)
	if !rep.OK() {
		t.Errorf("comentário histórico não é achado; findings=%+v", rep.Findings)
	}
}

// TestRepositorioRealPassa é a asserção de que a correção de fato aterrissou.
func TestRepositorioRealPassa(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	rep, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Findings {
		t.Errorf("%s", f.Message())
	}
	if rep.Scanned == 0 {
		t.Error("nenhum arquivo varrido — o gate passaria por vacuidade")
	}
}
