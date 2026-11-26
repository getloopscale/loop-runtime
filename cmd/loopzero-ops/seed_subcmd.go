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

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/seed"
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
	// The client of the context resolved through ConfigFlags.
	//
	// Where the context comes from, in order: CORE_CONTEXT when set, else the
	// kubeconfig's current-context (KUBECONFIG or the default path). This
	// subcommand has NO --kubeconfig or --context flags — runSeed declares only
	// --verify, --dry-run and --json; an earlier comment promised affordances
	// that did not exist (a LOW finding of the B-024 review).
	//
	// B-024: this comment used to say "Default (mgmt) client", and it was not
	// true — nothing here pins mgmt. After `k3d:secrets:create`, which switches
	// the active context as a side effect, the "mgmt client" silently talked to
	// the secrets cluster. The client now carries the context name, so the
	// message says where it looked.
	mgmt, mgmtRest, mgmtCtrl, err := buildKubeClientAndRest("")
	if err != nil {
		return seed.Deps{}, fmt.Errorf("kubeconfig: %w", err)
	}
	credsPath := os.Getenv("LOOPZERO_DEV_CREDENTIALS")
	if credsPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return seed.Deps{}, fmt.Errorf("locate dev-credentials.json: set LOOPZERO_DEV_CREDENTIALS: %w", err)
		}
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
		// The requirement policy is resolved HERE, at the composition root, and
		// nowhere else: that is what stops each seed from reading the environment
		// on its own and answering the same question a different way.
		Policy: seed.PolicyFromEnvironment(),
		// LOOPZERO_CELL_CONTEXT_PREFIX names the cells' kube contexts, set even when
		// empty: staging's kubeconfig names each one after the cell id. Unset keeps
		// the k3d names.
		CellContextPrefix: cellContextPrefixFromEnvironment(),
	}, nil
}

// cellContextPrefixFromEnvironment is LOOPZERO_CELL_CONTEXT_PREFIX when it is set, empty
// included, and nil when it is not.
func cellContextPrefixFromEnvironment() *string {
	if v, ok := os.LookupEnv("LOOPZERO_CELL_CONTEXT_PREFIX"); ok {
		return &v
	}
	return nil
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
	// The context is resolved ONCE and used for both things: building the client
	// and naming the messages. Resolving it on two paths would let a message name
	// a cluster nobody searched — worse than naming none, because it sends the
	// operator to check the wrong place with confidence. (B-024 review, finding 1.)
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
	// B-024: the client knows WHO it talks to. Without this, a resource missing
	// because the active context points at another cluster produced the same
	// message as a resource that really does not exist, and the two send the
	// operator in opposite directions.
	return seed.NewKubeClientForContext(cli, resolved), restCfg, cli, nil
}

// contextUsedByClientConfig returns the context ConfigFlags would really use,
// along the same path buildKubeClientAndRest takes. It exists so a test can
// prove the announced name and the queried cluster do not diverge.
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

// resolveContextName returns the context name ConfigFlags would really use.
//
// An explicit request wins. Without one it reads the kubeconfig's
// current-context — which is what ConfigFlags does, and what
// `k3d:secrets:create` changes as a side effect.
//
// It returns empty when the kubeconfig cannot be read: a wrongly named message
// is worse than an unnamed one, and naming is a diagnostic convenience, never a
// prerequisite for the seed to run.
func resolveContextName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	// CORE_CONTEXT PINS the cluster instead of inheriting the active one.
	//
	// B-024 review: naming the context in the message improves the diagnosis
	// but does not end the inheritance — it only changed WHICH wrong cluster was
	// hit. The convention already existed in the repository
	// (seed-post-cleanroom-all.sh uses `${CORE_CONTEXT:-k3d-loopzero-core}` in its
	// own kubectl calls), so the parsimony ladder stopped at rung 4: reuse what is
	// installed.
	//
	// Without the variable the behaviour does NOT change — it inherits the
	// active context and says which it used. Pinning a hard default here would
	// break anyone running seeds outside k3d.
	if pinned := os.Getenv("CORE_CONTEXT"); pinned != "" {
		return pinned
	}
	raw, err := genericclioptions.NewConfigFlags(true).ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return ""
	}
	return raw.CurrentContext
}
