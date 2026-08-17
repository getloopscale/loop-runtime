// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package publishhygiene

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// realHome monta um caminho de home NÃO-sintética em tempo de execução.
//
// Composto em vez de escrito literal porque este arquivo é varrido pela própria trava
// que ele testa: uma fixture com `/home/<nome>/` literal seria — corretamente — um
// achado. Isentar o arquivo resolveria por fora e deixaria a trava com um ponto cego
// no lugar onde ela é auditada. Compor mantém a asserção inteira e a varredura honesta.
func realHome(sub string) string { return "/" + "home" + "/alice/" + sub }

// gitTree makes a real git repo — the gate reads `git ls-files`, which is what will be
// published, not the working directory.
func gitTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
	return root
}

func TestRealHomePathIsALeak(t *testing.T) {
	root := gitTree(t, map[string]string{"a.md": "veja " + realHome("projeto/x") + "\n"})
	out, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}})
	if !errors.Is(err, ErrLeak) {
		t.Fatalf("home de usuário real é vazamento; err=%v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Kind != "home" {
		t.Fatalf("achado precisa ser classificado; got %+v", out.Findings)
	}
}

// A fronteira que impede a trava de virar ruído — e é ela que faz a diferença entre
// esta trava e uma que reprova o próprio teste de segurança.
func TestSyntheticHomesAndNonRoutableIPsAreNotLeaks(t *testing.T) {
	root := gitTree(t, map[string]string{
		"ci.yml":   "workdir: /home/runner/work/x\n",
		"ssrf.go":  "const metadata = \"169.254.169.254\"\n",
		"dns.go":   "const exampleCom = \"93.184.216.34\"\n",
		"local.go": "const dev = \"127.0.0.1\"\nconst lan = \"192.168.1.10\"\n",
		"fix.go":   "const home = \"/home/dev/loopzero\"\n",
	})
	out, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}})
	if err != nil {
		t.Fatalf("constantes de teste e homes de CI não são vazamento: %v (%+v)", err, out.Findings)
	}
}

// A metade de IP do loopzero-rag NÃO foi transportada, e isto é asserção do escopo — não
// um esquecimento. Contra esta árvore ela deu 104 achados quase todos falsos: strings
// de versão, o placeholder `1.2.3.4`, as faixas CIDR da Cloudflare que o código carrega
// de propósito, e o alvo vivo declarado em `rules/live-target.txt`, que é contrato.
func TestIPHalfIsDeliberatelyOutOfScope(t *testing.T) {
	root := gitTree(t, map[string]string{
		"runbook.md":  "ssh operador@165.227.121.20\n",
		"versions.md": "Tags aceitas: `13.3.2.1-1`, `15.5-10`\n",
		"cf.go":       `const cf = "173.245.48.0/20"` + "\n",
	})
	if _, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}}); err != nil {
		t.Fatalf("esta trava mede CAMINHO DE HOME; endereço é escopo do loopzero-rag: %v", err)
	}
}

// A trilha datada é isenta por DECLARAÇÃO, e a razão é outra regra do repositório:
// `docs/README.md` proíbe reescrever registro datado para corresponder ao presente.
func TestAuditTrailIsNotScanned(t *testing.T) {
	root := gitTree(t, map[string]string{
		"docs/wiki/reviews/old.md":              "rodou em " + realHome("loopzero") + "\n",
		"docs/operations/baselines/b.md":        "medido em " + realHome("loopzero") + "\n",
		"CHANGELOG.md":                          "corrigido em " + realHome("loopzero") + "\n",
		".claude/knowledge-base/decisions/x.md": "medido em " + realHome("m") + "\n",
		"vivo.md":                               "sem vazamento\n",
	})
	out, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}})
	if err != nil {
		t.Fatalf("registro datado não é varrido: %v (%+v)", err, out.Findings)
	}
	if out.Scanned != 1 {
		t.Fatalf("só o arquivo vivo devia ser varrido; scanned=%d", out.Scanned)
	}
}

// Uma isenção precisa de razão E sunset. Sem os dois ela não declara nada — só silencia.
func TestExemptionNeedsReasonAndSunset(t *testing.T) {
	root := gitTree(t, map[string]string{
		"deploy.conf": "root " + realHome("dist") + "\n",
		"limpo.md":    "sem vazamento\n", // sem um arquivo varrido, o veredito seria ErrNothingScanned
	})
	for _, e := range []Exemption{
		{Path: "deploy.conf", Sunset: "2026-11-17"},
		{Path: "deploy.conf", Reason: "caminho de implantação"},
	} {
		if _, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{e}}); err == nil {
			t.Fatalf("isenção incompleta não pode passar: %+v", e)
		}
	}
	ok := Exemption{Path: "deploy.conf", Reason: "caminho de implantação vivo", Sunset: "2026-11-17"}
	out, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{ok}})
	if err != nil {
		t.Fatalf("isenção completa deve passar: %v", err)
	}
	if out.Exempt != 1 {
		t.Fatalf("a isenção precisa ser CONTADA para que sua existência apareça; got %d", out.Exempt)
	}
}

// Ausência é FALHA, nunca skip.
func TestEmptyTreeIsAFailure(t *testing.T) {
	root := gitTree(t, map[string]string{})
	if _, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}}); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("varrer zero arquivos precisa reprovar; err=%v", err)
	}
}

// A população real (B-109). É esta asserção que dá sentido ao gate: ele nasce medido
// contra a árvore que ship, não contra fixtures.
func TestRealTreeIsClean(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot})
	if err != nil {
		for i, f := range out.Findings {
			if i == 12 {
				t.Errorf("... e mais %d", len(out.Findings)-12)
				break
			}
			t.Errorf("%s:%d [%s] %s", f.File, f.Line, f.Kind, f.Text)
		}
		t.Fatalf("%d vazamento(s) na árvore real: %v", len(out.Findings), err)
	}
	if out.Scanned < 500 {
		t.Fatalf("scanned=%d — a varredura está alcançando pouco da árvore", out.Scanned)
	}
}
