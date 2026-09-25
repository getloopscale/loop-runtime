// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// seed subcommand group — Go replacements for the canonical post-Cleanroom
// shell seeds (M15 — operator-glue-go-migration). The composition root: it
// builds the KubeClient + loads creds, then dispatches to a registered seed.
//
// Seeds are registered by the per-wave files (seed_wave*.go); Phase 0 ships
// the dispatch + registry plumbing with an empty registry. `seed list` and
// `seed all` iterate the registry in canonical (insertion) order.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/seed"
)

const seedHelpText = `loopzero-ops seed — Go replacements for the canonical post-Cleanroom seeds (M15)

USAGE:
  loopzero-ops seed <name> [--verify] [--dry-run] [--json]
  loopzero-ops seed all    [--verify] [--dry-run] [--json]
  loopzero-ops seed list

SEMANTICS:
  Run (default) applies the seed idempotently (read → guard → CreateOrUpdate).
  --verify   probe the post-condition instead of applying (parity with bash --verify).
  --dry-run  print intended actions without mutating the cluster.
  --json     emit one machine-readable line per seed: {"seed":..,"verdict":..,"reason":..}.

EXIT CODES:
  0  all seeds PASS or SKIP
  1  usage error (unknown seed, bad flags), or at least one seed FAIL
`

// seedRegistry is the process-wide registry the wave files populate via init().
var seedRegistry = seed.NewRegistry()

func runSeed(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, seedHelpText)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(seedHelpText)
		return 0
	case "list":
		for _, n := range seedRegistry.Names() {
			fmt.Println(n)
		}
		return 0
	}

	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	verify := fs.Bool("verify", false, "probe post-condition instead of applying")
	dryRun := fs.Bool("dry-run", false, "print intended actions without mutating")
	asJSON := fs.Bool("json", false, "emit one JSON line per seed")
	name := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	var targets []seed.Seed
	if name == "all" {
		for _, n := range seedRegistry.Names() {
			s, _ := seedRegistry.Lookup(n)
			targets = append(targets, s)
		}
	} else {
		s, ok := seedRegistry.Lookup(name)
		if !ok {
			fmt.Fprintf(os.Stderr, "seed: unknown seed %q (run `loopzero-ops seed list`)\n", name)
			return 1
		}
		targets = []seed.Seed{s}
	}

	deps, err := buildSeedDeps(*dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		return 2
	}

	worst := 0
	for _, s := range targets {
		var r seed.Result
		if *verify {
			r = s.Verify(deps)
		} else {
			r = s.Run(deps)
		}
		emitSeedResult(s.Name(), r, *asJSON)
		if code := r.Verdict.ExitCode(); code > worst {
			worst = code // EC-2: continue-and-aggregate; worst verdict wins
		}
	}
	return worst
}

func emitSeedResult(name string, r seed.Result, asJSON bool) {
	if asJSON {
		line, _ := json.Marshal(map[string]string{
			"seed": name, "verdict": r.Verdict.String(), "reason": r.Reason,
		})
		fmt.Println(string(line))
		return
	}
	if r.Reason != "" {
		fmt.Printf("seed %s: %s — %s\n", name, r.Verdict, r.Reason)
	} else {
		fmt.Printf("seed %s: %s\n", name, r.Verdict)
	}
}

// buildSeedDeps is the composition root: resolves kubeconfig via ConfigFlags
// (kubectl-like — blueprint T1 cnpg plugin.go:86 pattern), builds a typed
// controller-runtime client, and loads dev-credentials.json.
//
// A missing creds file is NOT an error at this level — LoadCreds returns
// ErrCredsAbsent and each seed maps that to SKIP (SKIP-graceful semantics).
// Only ErrCredsBadMode / ErrCredsMalformed are surfaced upstream (operator
// error → FAIL). A missing kubeconfig IS a hard error (nothing to do).
func buildSeedDeps(dryRun bool) (seed.Deps, error) {
	// Cliente do contexto resolvido via ConfigFlags.
	//
	// A origem do contexto, em ordem: `CORE_CONTEXT` quando definida, senao o
	// `current-context` do kubeconfig (`KUBECONFIG` ou caminho padrao). NAO ha flags
	// `--kubeconfig` nem `--context` neste subcomando — `runSeed` declara apenas
	// `--verify`, `--dry-run` e `--json`, e o comentario anterior prometia afordancias
	// inexistentes (achado LOW da revisao de B-024).
	//
	// B-024: este comentario dizia "Default (mgmt) client", e nao era verdade — nada aqui
	// fixa o mgmt. Depois de `k3d:secrets:create`, que troca o contexto ativo como efeito
	// colateral, o "cliente do mgmt" passava a falar com o cluster de secrets em silencio.
	// O cliente agora carrega o nome do contexto para que a mensagem diga onde procurou.
	mgmt, mgmtRest, mgmtCtrl, err := buildKubeClientAndRest("")
	if err != nil {
		return seed.Deps{}, fmt.Errorf("kubeconfig: %w", err)
	}
	credsPath := os.Getenv("LOOPZERO_DEV_CREDENTIALS")
	if credsPath == "" {
		home, _ := os.UserHomeDir()
		credsPath = filepath.Join(home, ".loopzero", "dev-credentials.json")
	}
	creds, credsErr := seed.LoadCreds(credsPath)
	if credsErr != nil && !errors.Is(credsErr, seed.ErrCredsAbsent) {
		return seed.Deps{}, fmt.Errorf("creds: %w", credsErr)
	}
	// PortForwardCtor: memoize (ctx, rest, ctrl) triples per kubeconfig context.
	// For the mgmt context we already have them; other contexts resolve via
	// buildKubeClientAndRest lazily.
	restByCtx := map[string]*rest.Config{"": mgmtRest}
	ctrlByCtx := map[string]ctrlclient.Client{"": mgmtCtrl}
	pfCtor := func(ctx context.Context, target seed.TunnelTarget) (*seed.Tunnel, error) {
		contextName := target.Context
		r, ok := restByCtx[contextName]
		c, ok2 := ctrlByCtx[contextName]
		if !ok || !ok2 {
			_, rr, cc, err := buildKubeClientAndRest(contextName)
			if err != nil {
				return nil, err
			}
			r, c = rr, cc
			restByCtx[contextName] = rr
			ctrlByCtx[contextName] = cc
		}
		return seed.OpenTunnelForService(ctx, r, c, target)
	}
	return seed.Deps{
		Ctx:             context.Background(),
		Kube:            mgmt,
		RestConfig:      mgmtRest,
		CtrlClient:      mgmtCtrl,
		Creds:           creds,
		DryRun:          dryRun,
		ClientCtor:      buildKubeClient,
		PortForwardCtor: pfCtor,
		// A política de exigência é resolvida AQUI, na raiz de composição, e em
		// nenhum outro lugar: é o que impede que cada seed volte a ler o ambiente
		// por conta própria e a responder a mesma pergunta de um jeito diferente.
		Policy: seed.PolicyFromEnvironment(),
	}, nil
}

// buildKubeClient resolves a controller-runtime client for the given kubeconfig
// context name. The empty string uses the default context (equivalent to
// `kubectl` without --context). Multi-cluster seeds request build/workload/vault
// contexts via Deps.ClientFor which memoizes.
func buildKubeClient(ctxName string) (seed.KubeClient, error) {
	k, _, _, err := buildKubeClientAndRest(ctxName)
	return k, err
}

// buildKubeClientAndRest returns the KubeClient wrapper alongside the raw
// *rest.Config + ctrlclient.Client so the composition root can pass them to
// PortForwardCtor (port-forward needs the rest.Config; pod-listing for label
// resolution needs the ctrlclient.Client).
func buildKubeClientAndRest(ctxName string) (seed.KubeClient, *rest.Config, ctrlclient.Client, error) {
	// O contexto e resolvido UMA vez e usado para as duas coisas: construir o cliente e
	// nomear as mensagens. Resolver em dois caminhos deixaria a mensagem nomear um cluster
	// onde ninguem procurou — pior que nao nomear, porque manda o operador conferir o lugar
	// errado com confianca. (Achado 1 da revisao de B-024.)
	resolved := resolveContextName(ctxName)

	cfgFlags := genericclioptions.NewConfigFlags(true)
	if resolved != "" {
		cfgFlags.Context = &resolved
	}
	restCfg, err := cfgFlags.ToRESTConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("kubeconfig context %q: %w", ctxName, err)
	}
	cli, err := ctrlclient.New(restCfg, ctrlclient.Options{})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("kube client context %q: %w", ctxName, err)
	}
	// B-024: o cliente passa a saber COM QUEM fala. Sem isto, um recurso ausente porque o
	// contexto ativo aponta para outro cluster produzia a mesma mensagem que um recurso que
	// realmente nao existe, e as duas mandam o operador para lados opostos.
	return seed.NewKubeClientForContext(cli, resolved), restCfg, cli, nil
}

// contextUsedByClientConfig devolve o contexto que o ConfigFlags de fato usaria, pelo mesmo
// caminho que `buildKubeClientAndRest` percorre. Existe para que um teste possa provar que
// o nome anunciado e o cluster consultado nao divergem.
func contextUsedByClientConfig(explicit string) (string, error) {
	resolved := resolveContextName(explicit)
	cfgFlags := genericclioptions.NewConfigFlags(true)
	if resolved != "" {
		cfgFlags.Context = &resolved
	}
	raw, err := cfgFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return "", err
	}
	if resolved != "" {
		return resolved, nil
	}
	return raw.CurrentContext, nil
}

// resolveContextName devolve o nome do contexto que o ConfigFlags realmente usaria.
//
// Um pedido explicito vence. Sem ele, le o `current-context` do kubeconfig — que e o que o
// ConfigFlags faz, e o que `k3d:secrets:create` altera como efeito colateral.
//
// Devolve vazio quando o kubeconfig nao pode ser lido: uma mensagem com nome errado e pior
// que uma sem nome nenhum, e nomear e uma comodidade de diagnostico, nunca um pre-requisito
// para o seed rodar.
func resolveContextName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	// `CORE_CONTEXT` FIXA o cluster, em vez de herdar o ativo.
	//
	// Achado da revisao (B-024): nomear o contexto na mensagem melhora o diagnostico mas
	// nao elimina a heranca — trocava apenas QUAL cluster errado era atingido. A convencao
	// ja existia no repositorio (`seed-post-cleanroom-all.sh:335` usa
	// `${CORE_CONTEXT:-k3d-loopzero-core}` nos proprios kubectl), entao a escada parou na
	// rung 4: reusar o que esta instalado.
	//
	// Sem a variavel o comportamento NAO muda — herda o ativo e diz qual usou. Fixar um
	// padrao rigido aqui quebraria quem roda seeds fora do k3d, e o item pede que a origem
	// seja declarada ou dita, nao que seja imposta.
	if pinned := os.Getenv("CORE_CONTEXT"); pinned != "" {
		return pinned
	}
	raw, err := genericclioptions.NewConfigFlags(true).ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return ""
	}
	return raw.CurrentContext
}
