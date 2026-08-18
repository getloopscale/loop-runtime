// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package scheduledimage refuses a scheduled manifest whose image has no way to exist.
//
// # O defeito, medido
//
// Quatro CronWorkflows executavam `registry.digitalocean.com/loopzero-system/loopzero-ops:v0.1.0`, e
// o registro inteiro devolve `404 registry does not exist`. O item `B-019` registrou isso
// como "o registro foi deletado" — e a medição de 2026-08-18 encontrou **quatro camadas de
// ausência**, não uma:
//
//	o registro          `doctl registry get` → 404
//	o Dockerfile        não existe nenhum para loopzero-ops na árvore
//	a task de build     não existe — a ÚNICA menção a `task images:build:loopzero-ops` no
//	                    repositório é o comentário do values.yaml AFIRMANDO que ela existe
//	a matriz            release-artifacts.yml publica 5 imagens; loopzero-ops não está lá
//
// Nenhum `imagePullSecret` resolve um registro que não existe, e nenhuma credencial resolve
// uma imagem que ninguém constrói. O agendamento produzia pod vermelho a cada disparo —
// e isso é pior que automação ausente, porque treina quem olha o cluster a ignorar vermelho.
//
// # O critério, e por que ele é este
//
// O gate julga **imagem nossa usada por recurso agendado**. Terceiro não é responsabilidade
// nossa publicar: medido nesta árvore, dos **7** manifestos agendados, **6** usam imagem de
// terceiro (`curlimages/curl`, `bitnami/kubectl`, `quay.io/openbao/openbao`,
// `ghcr.io/openbao/openbao-snapshot-agent`) e **1** usa a nossa — justamente a quebrada.
//
// A verificação é **hermética**: compara contra a matriz de publicação desta árvore, nunca
// contra o registro. Sondar o registro exigiria rede e credencial num portão de push, e um
// `401` não distingue ausente de não-autorizado — lição medida no `B-016`, cujo token devolve
// exatamente isso. Um gate que reprova por acidente de rede é desligado.
//
// # O que ele NÃO faz
//
// Não renderiza o chart. A resolução de `{{ .Values.x.y }}` é textual e cobre a forma que
// esta árvore usa; uma expressão com condicional, `include` ou `tpl` não é resolvida, e nesse
// caso o gate reporta a imagem como **não resolvida** em vez de fingir que está tudo certo.
//
// E ele mede **o que o chart declara**, não o que um ambiente liga: a sobreposição por
// `ciCronsEnabled` mora no helmfile. Um chart que embarca `enabled: true` está declarando uma
// execução, e é isso que é julgado aqui.
//
// # Por que `WorkflowTemplate` entra, apesar do nome do gate
//
// A primeira versão varria só `CronWorkflow` e `CronJob`. Medido em 2026-08-18: o mesmo
// `loopzero-ci-workflows` usa a imagem impossível em **duas** superfícies — os quatro crons e o
// `workflowtemplate-quality-gate.yaml`, disparado por PR via o sensor do Gitea. Um gate que
// cobrisse metade reportaria verde com o defeito idêntico um arquivo ao lado.
//
// O que agendamento e disparo por evento compartilham, e é o que decide o critério: a execução
// é **autônoma**. A falha aparece longe de quem a causou, então ninguém está olhando no momento
// em que ela acontece — que é exatamente a condição para apodrecer em silêncio.
package scheduledimage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrNoPublicationPath means a scheduled manifest uses one of our images that nothing builds.
var ErrNoPublicationPath = errors.New("recurso agendado usa imagem nossa sem caminho de publicação")

// ErrNothingScanned means the scan reached no scheduled manifest.
var ErrNothingScanned = errors.New("nenhuma declaração de execução autônoma varrida — não achar nada não é o mesmo que não achar nada errado")

// ErrMatrixUnreadable means the publication matrix could not be read.
//
// Ausência é FALHA: sem a matriz não há como saber o que é publicado, e tratar isso como
// "nenhuma violação" é exatamente o verde-sobre-nada que este repositório já pagou.
var ErrMatrixUnreadable = errors.New("matriz de publicação ilegível")

// matrixRelPath is where the published image list lives.
const matrixRelPath = ".github/workflows/release-artifacts.yml"

// ourRegistries marks an image as ours to publish.
//
// Lista fechada de propósito: um prefixo novo entra aqui conscientemente. Uma heurística
// ("qualquer coisa com loopzero") classificaria `loopzero-` de terceiro como nossa.
var ourRegistries = []string{
	"registry.digitalocean.com/loopzero-system/",
	"ghcr.io/getloopscale/",
	"ghcr.io/getloopscale/",
}

var (
	scheduledKindRE = regexp.MustCompile(`(?m)^kind:\s*(CronWorkflow|CronJob|WorkflowTemplate)\s*$`)
	imageLineRE     = regexp.MustCompile(`(?m)^\s*-?\s*image:\s*(.+)$`)
	valuesRefRE     = regexp.MustCompile(`\{\{-?\s*\.Values\.([A-Za-z0-9_.]+)\s*-?\}\}`)
	// A forma que a árvore real usa: a imagem é montada numa VARIÁVEL e só depois usada.
	// A primeira versão deste gate era cega a ela, e a varredura da população passou pelo
	// motivo errado — mesma armadilha do B-090.
	varAssignRE   = regexp.MustCompile(`\{\{-?\s*(\$[A-Za-z0-9_]+)\s*:=\s*(.+?)\s*-?\}\}`)
	varUseRE      = regexp.MustCompile(`\{\{-?\s*(\$[A-Za-z0-9_]+)(?:\s*\|[^}]*)?\s*-?\}\}`)
	printfRE      = regexp.MustCompile(`^printf\s+"([^"]*)"\s+(.+)$`)
	bareValueRE   = regexp.MustCompile(`\.Values\.([A-Za-z0-9_.]+)`)
	matrixImageRE = regexp.MustCompile(`"image"\s*:\s*"([^"]+)"`)
)

// Exemption is an image knowingly without a publication path, with a reason and a sunset.
//
// Mesma forma do `publishhygiene` (B-130): sem razão E sem prazo a entrada não declara nada,
// só cala. O prazo é a função forçante — a dívida não pode envelhecer em silêncio.
type Exemption struct {
	Image  string
	Reason string
	Sunset string // YYYY-MM-DD
}

// DefaultExemptions carrega a dívida que este gate não pode resolver sozinho.
var DefaultExemptions = []Exemption{
	{
		Image:  "loopzero-ops",
		Sunset: "2026-11-18",
		Reason: "DUAS superfícies do `loopzero-ci-workflows` usam esta imagem — os quatro CronWorkflows " +
			"e o `workflowtemplate-quality-gate.yaml`, disparado por PR via o sensor do Gitea. " +
			"Aposentar só os crons deixaria o defeito idêntico no caminho por evento, então o que " +
			"está bloqueado é o SUBSISTEMA, não uma bala. E eles NÃO executam o binário loopzero-ops: " +
			"rodam `task drill:watchdog`, `task quality:coverage:report` e um script bash, sem clone " +
			"do repositório e sem volume (cronworkflows.yaml:46-55). A imagem nunca foi um binário — " +
			"era um runner de CI com a árvore e o toolchain dentro. Publicar um Dockerfile de " +
			"cmd/loopzero-ops entregaria uma imagem que PUXA e falha em seguida, em `task: not found`, " +
			"trocando um vermelho honesto por um mais caro de diagnosticar. O futuro dos quatro — " +
			"runner de CI, clone+toolchain, ou aposentar o CI in-cluster por completo — é decisão de " +
			"superfície de publicação, com custo medido no MEET de 2026-08-18 e aberta em B-019. As três " +
			"imagens de `infra/images/` também não estão publicadas, então não há imagem alcançável para " +
			"hospedar o trabalho.",
	},
}

// Config drives the check.
type Config struct {
	RepoRoot string
	// Exemptions defaults to DefaultExemptions when nil.
	Exemptions []Exemption
}

func (c Config) exemptions() []Exemption {
	if c.Exemptions == nil {
		return DefaultExemptions
	}
	return c.Exemptions
}

// Finding is one scheduled manifest whose image cannot exist.
type Finding struct {
	Chart string
	File  string
	Image string
	Why   string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned  int
	Exempt   int
	Findings []Finding
	Reason   string
}

// Check scans every chart that ships a scheduled resource.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	published, err := publishedImages(filepath.Join(root, matrixRelPath))
	if err != nil {
		return out, err
	}

	isento := map[string]bool{}
	for _, e := range cfg.exemptions() {
		if strings.TrimSpace(e.Reason) == "" || strings.TrimSpace(e.Sunset) == "" {
			return out, fmt.Errorf("%w: isenção de %q sem razão ou sem prazo — sem os dois ela não declara nada, só cala",
				ErrNoPublicationPath, e.Image)
		}
		isento[e.Image] = true
	}

	manifests, err := scheduledManifests(root)
	if err != nil {
		return out, err
	}
	out.Scanned = len(manifests)
	if out.Scanned == 0 {
		return out, ErrNothingScanned
	}

	for _, m := range manifests {
		vals := loadValues(filepath.Join(filepath.Dir(filepath.Dir(m)), "values.yaml"))
		chart := filepath.Base(filepath.Dir(filepath.Dir(m)))
		rel, _ := filepath.Rel(root, m)

		for _, img := range imagesIn(m, vals) {
			if !isOurs(img) {
				continue // terceiro: não é nosso para publicar
			}
			name := imageName(img)
			if published[name] {
				continue
			}
			if isento[name] {
				out.Exempt++
				continue
			}
			out.Findings = append(out.Findings, Finding{
				Chart: chart, File: rel, Image: img,
				Why: fmt.Sprintf("%q está sob registro nosso e não aparece em %s — nada nesta árvore a constrói, "+
					"então nenhum imagePullSecret a resolve. Publique-a, ou aponte o agendamento para uma imagem que exista",
					name, matrixRelPath),
			})
		}
	}

	sort.Slice(out.Findings, func(i, j int) bool { return out.Findings[i].File < out.Findings[j].File })
	if len(out.Findings) > 0 {
		return out, fmt.Errorf("%w (%d): %s", ErrNoPublicationPath, len(out.Findings), out.Findings[0].Why)
	}
	out.Reason = fmt.Sprintf("%d declaração(ões) de execução autônoma varrida(s); %d imagem(ns) isenta(s) com razão e prazo", out.Scanned, out.Exempt)
	return out, nil
}

// publishedImages reads the image names the publication matrix declares.
func publishedImages(path string) (map[string]bool, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrMatrixUnreadable, path, err)
	}
	out := map[string]bool{}
	for _, m := range matrixImageRE.FindAllStringSubmatch(string(body), -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %s não declara imagem nenhuma", ErrMatrixUnreadable, path)
	}
	return out, nil
}

// scheduledManifests finds every chart template declaring a scheduled resource.
func scheduledManifests(root string) ([]string, error) {
	var out []string
	for _, base := range []string{"infra/helm", "charts"} {
		dir := filepath.Join(root, base)
		err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil //nolint:nilerr // subárvore ausente não é violação
			}
			if !strings.HasSuffix(p, ".yaml") && !strings.HasSuffix(p, ".yml") {
				return nil
			}
			if !strings.Contains(p, string(os.PathSeparator)+"templates"+string(os.PathSeparator)) {
				return nil
			}
			// #nosec G122 -- `p` vem do próprio Walk sobre uma raiz derivada de constante
			// (`infra/helm`, `charts`) e é apenas LIDO; não há escrita nem execução, e o
			// pior caso de um symlink é ler um YAML que não é chart — o gate reporta a
			// imagem, não a segue.
			body, rerr := os.ReadFile(filepath.Clean(p))
			if rerr != nil {
				return nil
			}
			if scheduledKindRE.Match(body) {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("varrendo %s: %w", base, err)
		}
	}
	sort.Strings(out)
	return out, nil
}

// imagesIn extracts every image the manifest references, resolving `.Values` textually.
func imagesIn(path string, vals map[string]any) []string {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil
	}
	vars := templateVars(string(body), vals)
	var out []string
	for _, m := range imageLineRE.FindAllStringSubmatch(string(body), -1) {
		raw := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		raw = resolveVars(raw, vars)
		out = append(out, resolve(raw, vals))
	}
	return out
}

// templateVars resolves `{{- $name := ... }}` assignments in the same file.
//
// Cobre as duas formas medidas nesta árvore: atribuição direta de `.Values.x` e
// `printf "fmt" .Values.a .Values.b`. Qualquer outra fica sem resolver — e sem resolver
// significa que a imagem aparece como o texto original, visível, em vez de virar uma
// imagem plausível-porém-falsa que o gate aprovaria.
func templateVars(body string, vals map[string]any) map[string]string {
	out := map[string]string{}
	for _, m := range varAssignRE.FindAllStringSubmatch(body, -1) {
		nome, expr := m[1], strings.TrimSpace(m[2])
		if p := printfRE.FindStringSubmatch(expr); p != nil {
			formato := p[1]
			var args []any
			for _, a := range bareValueRE.FindAllStringSubmatch(p[2], -1) {
				v, ok := lookup(vals, strings.Split(a[1], "."))
				if !ok {
					args = nil
					break
				}
				args = append(args, v)
			}
			if args != nil && strings.Count(formato, "%") == len(args) {
				out[nome] = fmt.Sprintf(formato, args...)
			}
			continue
		}
		if a := bareValueRE.FindStringSubmatch(expr); a != nil && expr == "."+"Values."+a[1] {
			if v, ok := lookup(vals, strings.Split(a[1], ".")); ok {
				out[nome] = fmt.Sprintf("%v", v)
			}
		}
	}
	return out
}

// resolveVars replaces `{{ $name | quote }}` with the resolved assignment.
func resolveVars(raw string, vars map[string]string) string {
	return varUseRE.ReplaceAllStringFunc(raw, func(ref string) string {
		m := varUseRE.FindStringSubmatch(ref)
		if m == nil {
			return ref
		}
		if v, ok := vars[m[1]]; ok {
			return v
		}
		return ref
	})
}

// resolve replaces `{{ .Values.a.b }}` with the value, leaving unresolved forms visible.
//
// Deixar o texto original quando não resolve é deliberado: uma expressão com condicional ou
// `include` não vira imagem plausível-porém-falsa, e o operador vê o que o gate viu.
func resolve(raw string, vals map[string]any) string {
	return valuesRefRE.ReplaceAllStringFunc(raw, func(ref string) string {
		m := valuesRefRE.FindStringSubmatch(ref)
		if m == nil {
			return ref
		}
		if v, ok := lookup(vals, strings.Split(m[1], ".")); ok {
			return fmt.Sprintf("%v", v)
		}
		return ref
	})
}

func lookup(vals map[string]any, path []string) (any, bool) {
	var cur any = vals
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func loadValues(path string) map[string]any {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil
	}
	var out map[string]any
	if yaml.Unmarshal(body, &out) != nil {
		return nil
	}
	return out
}

// isOurs reports whether we are the ones who must publish this image.
func isOurs(img string) bool {
	for _, p := range ourRegistries {
		if strings.HasPrefix(img, p) {
			return true
		}
	}
	return false
}

// imageName strips registry and tag, leaving the name the matrix declares.
func imageName(img string) string {
	if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
		img = img[:i]
	}
	if i := strings.LastIndex(img, "/"); i >= 0 {
		img = img[i+1:]
	}
	return img
}
