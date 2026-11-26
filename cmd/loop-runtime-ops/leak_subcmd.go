// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// leak subcommand group — the LIVE probe that proves the public edge closes.
//
// It lives outside `quality` because every quality gate is hermetic by contract
// (no network, no kubectl, no helmfile) and this probe needs a live target. It
// sits next to `chaos` and `drill`, the groups that touch an environment.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/cliout"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/leak"
)

const leakHelpText = `loop-runtime-ops leak — LIVE probe of the public surface

USAGE:
  loop-runtime-ops leak live  [flags]   probe the public edge without a credential
  loop-runtime-ops leak ports [flags]   audit the host's listening ports against the registry

WHAT IT PROVES:
  That the public edge DENIES without a credential (401/403) or leads to login
  (302), and that the routes public by the OIDC specification keep answering 200.
  Targets are derived from infra/quality/proxy-exposure.txt, so coverage follows
  the surface by construction — a family declared there with no concrete
  representative is reported as a coverage hole, not silenced.

  404 does NOT count as closed: hiding is not denying, and accepting it disguises
  a broken route as protection.

WHAT IT DOES NOT PROVE:
  That an authenticated response exposes only what it should — that depends on
  the body and is recorded, per route, in the inventory's rationale column.

FLAGS (live):
  --host        probe only this application host (default: every declared host)
  --auth-host   identity host              (default: auth-dev.getloopscale.com)
  --inventory   declared inventory         (default: infra/quality/proxy-exposure.txt)
  --json        machine-readable output

FLAGS (ports):
  --registry    declared port registry     (default: infra/quality/host-ports.txt)
  --json        machine-readable output

EXIT:
  0 = the edge closes    1 = usage error, something got through without a
  credential, or a declared family is not measured
`

// leakEnv is what a leak run touches outside the process. runLeak uses the
// real one; tests inject fakes so every path is exercised without a live edge.
// The writers keep their first write error, and runLeakWith settles the exit
// status against them once the probe has reported.
type leakEnv struct {
	stdout, stderr *cliout.Writer
	client         *http.Client
	discovery      func(ctx context.Context, cli *http.Client, host string) []string
	placeholders   func() []string
	listeningPorts func() ([]int, error)
}

func defaultLeakEnv() leakEnv {
	return leakEnv{
		stdout:         cliout.New(os.Stdout),
		stderr:         cliout.New(os.Stderr),
		client:         leak.DefaultClient(),
		discovery:      probeDiscovery,
		placeholders:   probePlaceholders,
		listeningPorts: listeningPorts,
	}
}

func runLeak(args []string) int { return runLeakWith(args, defaultLeakEnv()) }

func runLeakWith(args []string, env leakEnv) int {
	return cliout.Settle(dispatchLeak(args, env), env.stdout, env.stderr)
}

func dispatchLeak(args []string, env leakEnv) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		env.stdout.Print(leakHelpText)
		return 0
	}
	switch args[0] {
	case "live":
		return runLeakLive(args[1:], env)
	case "ports":
		return runLeakPorts(args[1:], env)
	default:
		env.stderr.Printf("unknown subcommand %q (use: live|ports)\n", args[0])
		return 1
	}
}

// liveOptions are the parsed `leak live` flags.
type liveOptions struct {
	hosts     []string // nil: every declared host
	authHost  string
	inventory string
	asJSON    bool
}

func parseLiveFlags(args []string, stderr io.Writer) (liveOptions, error) {
	fs := flag.NewFlagSet("leak live", flag.ContinueOnError)
	fs.SetOutput(stderr)
	host := fs.String("host", "", "probe only this application host")
	authHost := fs.String("auth-host", "auth-dev.getloopscale.com", "identity host")
	inventory := fs.String("inventory", "infra/quality/proxy-exposure.txt", "declared inventory")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return liveOptions{}, err
	}
	opts := liveOptions{authHost: *authHost, inventory: *inventory, asJSON: *asJSON}
	// fs.Visit only visits flags actually passed: `--host ""` must be refused,
	// not read as "no filter" — probing zero application surface and exiting 0
	// is the empty green this probe exists to refuse.
	var hostErr error
	fs.Visit(func(f *flag.Flag) {
		if f.Name != "host" {
			return
		}
		if strings.TrimSpace(*host) == "" {
			hostErr = fmt.Errorf("--host is empty: there is no surface to probe; omit the flag to sweep the inventory")
			return
		}
		opts.hosts = []string{*host}
	})
	return opts, hostErr
}

func runLeakLive(args []string, env leakEnv) int {
	opts, err := parseLiveFlags(args, env.stderr)
	if err != nil {
		env.stderr.Println(err)
		return 1
	}
	hosts := opts.hosts
	if hosts == nil {
		// Probe EVERY host the inventory declares: measured 2026-08-18, the
		// inventory declared 7 hosts and a single-host probe reached 2.
		if hosts, err = leak.Hosts(opts.inventory); err != nil {
			env.stderr.Println(err)
			return 1
		}
	}
	plan, err := leak.BuildPlan(opts.inventory, hosts, opts.authHost)
	if err != nil {
		env.stderr.Println(err)
		return 1
	}
	rep := leak.Run(context.Background(), plan, opts.inventory, leak.Checks{
		Client: env.client,
		Discovery: func(ctx context.Context, host string) []string {
			return env.discovery(ctx, env.client, host)
		},
		Placeholders: env.placeholders,
	})

	if opts.asJSON {
		enc := json.NewEncoder(env.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			env.stderr.Println(err)
			return 1
		}
	} else {
		renderLeakText(env.stdout, plan, rep)
	}
	renderLeakHeadline(env.stdout, env.stderr, rep, opts.asJSON)
	if !rep.Closed() {
		return 1
	}
	return 0
}

// renderLeakText prints the run in probe order.
func renderLeakText(w *cliout.Writer, plan leak.Plan, rep leak.Report) {
	if len(rep.Wildcards) > 0 {
		w.Printf("  SKIP  %-46s wildcard with no concrete hostname to resolve\n", strings.Join(rep.Wildcards, ", "))
	}
	for _, v := range rep.Verdicts {
		switch {
		case v.Skipped:
			w.Printf("  SKIP  %-46s %s\n", v.Host+v.Path, v.Reason)
		case v.Passed:
			w.Printf("  ok    %-46s %d\n", v.Host+v.Path, v.Status)
		default:
			w.Printf("  FAIL  %-46s %d\n", v.Host+v.Path, v.Status)
		}
	}
	if rep.Upstream != "" {
		w.Printf("  UPSTREAM  %s\n", rep.Upstream)
	}
	for _, l := range rep.DiscoveryLeaks {
		w.Printf("  FAIL  %-46s %s\n", plan.AuthHost+"/.well-known/openid-configuration", l)
	}
	for _, p := range rep.PlaceholderLeaks {
		w.Printf("  FAIL  %-46s %s\n", "ory-kratos-config", p)
	}
	for _, f := range rep.UncoveredFamilies {
		w.Printf("  NOT-MEASURED  %-38s no representative in the probe\n", f)
	}
	w.Printf("\n  PASS=%d  FAIL=%d  NOT-MEASURED=%d  SKIP=%d\n", rep.Pass, rep.Fail, rep.Uncovered, rep.Skip)
}

// renderLeakHeadline names what went wrong on stderr, keeping a leak and a
// coverage hole apart: they call for different actions, and a run with only
// coverage holes used to announce that "the edge let something through".
func renderLeakHeadline(stdout, stderr *cliout.Writer, rep leak.Report, asJSON bool) {
	if rep.Leaked() {
		stderr.Println("\nThe edge let something through without a credential:")
		for _, f := range rep.Failures {
			stderr.Printf("  - %s\n", f)
		}
	}
	if rep.Uncovered > 0 {
		stderr.Println("\nCoverage holes — declared surface the probe does not measure:")
		for _, n := range rep.NotMeasured {
			stderr.Printf("  - %s\n", n)
		}
	}
	if rep.Closed() && !asJSON {
		stdout.Println("  The edge closes on everything that requires a credential, and opens where the specification requires.")
	}
}

// probeDiscovery reads the discovery document and returns the fields that
// advertise a host unreachable from outside, or plain text. A read failure is
// NOT a leak — the closure probe already covers availability, and inventing a
// finding from a bad network would be worse than not measuring.
func probeDiscovery(ctx context.Context, cli *http.Client, host string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://"+host+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil
	}
	return leak.DiscoveryLeak(doc)
}

// probePlaceholders reads the Kratos configuration in the cluster and returns
// the fields still holding a substitution marker.
//
// Without a reachable kubectl there is no finding — deliberately: this probe
// measures the PUBLIC edge, and requiring cluster access would make it useless
// from outside. Whoever runs it with access gets the check for free.
func probePlaceholders() []string {
	out, err := exec.Command("kubectl", "--context", "k3d-loop-runtime-core",
		"-n", "loop-runtime-auth", "get", "cm", "ory-kratos-config", "-o", "yaml").Output()
	if err != nil {
		return nil
	}
	return leak.PlaceholderLeak(string(out))
}

// portsReport is the `leak ports --json` shape. The English keys are the
// contract; the Portuguese keys are the original names, still emitted with the
// same values so existing consumers keep working (--json field names are a
// stability contract: add, never rename). They are deprecated.
type portsReport struct {
	Listening  int                `json:"listening"`
	Declared   int                `json:"declared"`
	Undeclared []leak.PortFinding `json:"undeclared"`
	Stale      []leak.PortFinding `json:"stale"`
	ToClose    []leak.PortDecl    `json:"to_close"`

	// Deprecated: use listening, declared, undeclared, stale and to_close.
	LegacyListening  int                `json:"escutando"`      // english-only: deprecated --json key kept for compatibility
	LegacyDeclared   int                `json:"declaradas"`     // english-only: deprecated --json key kept for compatibility
	LegacyUndeclared []leak.PortFinding `json:"nao_declaradas"` // english-only: deprecated --json key kept for compatibility
	LegacyStale      []leak.PortFinding `json:"obsoletas"`      // english-only: deprecated --json key kept for compatibility
	LegacyToClose    []leak.PortDecl    `json:"a_fechar"`       // english-only: deprecated --json key kept for compatibility
}

// runLeakPorts audits what listens on the host against
// infra/quality/host-ports.txt.
//
// CLOSED BY DEFAULT: a port listening on 0.0.0.0 that is not in the registry
// FAILS. The previous firewall had the opposite polarity, and the 2026-08-10
// measurement found 17 internet-reachable ports on no list, ArgoCD and identity
// without TLS among them.
func runLeakPorts(args []string, env leakEnv) int {
	fs := flag.NewFlagSet("leak ports", flag.ContinueOnError)
	fs.SetOutput(env.stderr)
	registry := fs.String("registry", "infra/quality/host-ports.txt", "declared registry")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	decls, err := leak.ParsePorts(*registry)
	if err != nil {
		env.stderr.Println(err)
		return 1
	}
	listening, err := env.listeningPorts()
	if err != nil {
		env.stderr.Printf("could not read the listening ports: %v\n", err)
		return 1
	}

	undeclared, stale := leak.AuditPorts(listening, decls)
	toClose := leak.PortsToClose(listening, decls)

	if *asJSON {
		rep := portsReport{
			Listening: len(listening), Declared: len(decls),
			Undeclared: undeclared, Stale: stale, ToClose: toClose,
			LegacyListening: len(listening), LegacyDeclared: len(decls),
			LegacyUndeclared: undeclared, LegacyStale: stale, LegacyToClose: toClose,
		}
		enc := json.NewEncoder(env.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			env.stderr.Println(err)
			return 1
		}
	} else {
		env.stdout.Printf("  listening on 0.0.0.0: %d   declared: %d\n", len(listening), len(decls))
		for _, f := range undeclared {
			env.stdout.Printf("  FAIL  port %-6d %s\n", f.Port, f.Why)
		}
		for _, f := range stale {
			env.stdout.Printf("  WARN  port %-6d %s\n", f.Port, f.Why)
		}
		env.stdout.Printf("\n  to close (class does not admit external reach): %d\n", len(toClose))
		for _, d := range toClose {
			env.stdout.Printf("    %-6d %-8s %s\n", d.Port, d.Class, d.Reason)
		}
	}

	if len(undeclared) > 0 {
		env.stderr.Printf("\n%d port(s) listen without being in the registry. The registry is closed by "+
			"default: declare each one with its class and consumer, or close it.\n", len(undeclared))
		return 1
	}
	return 0
}

// listeningPorts reads the TCP ports listening on an all-interfaces address.
func listeningPorts() ([]int, error) {
	out, err := exec.Command("ss", "-ltn").Output()
	if err != nil {
		return nil, err
	}
	return parseListeningPorts(string(out)), nil
}

// parseListeningPorts extracts the ports bound to 0.0.0.0 or [::] from
// `ss -ltn` output, sorted and without repeats.
func parseListeningPorts(ssOutput string) []int {
	seen := map[int]bool{}
	for _, line := range strings.Split(ssOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		addr := fields[3]
		if !strings.HasPrefix(addr, "0.0.0.0:") && !strings.HasPrefix(addr, "[::]:") {
			continue
		}
		p, err := strconv.Atoi(addr[strings.LastIndex(addr, ":")+1:])
		if err != nil {
			continue
		}
		seen[p] = true
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}
