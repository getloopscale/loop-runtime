// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package seed hosts the Go replacements for the canonical post-Cleanroom
// shell seeds (M15 — operator-glue-go-migration). Each seed implements the
// Seed interface and follows the canonical pipeline (blueprint ADR D2):
// read → guard(no-op) → precondition → CreateOrUpdate/patch → verify.
//
// The Verdict vocabulary is the parity contract with the bash seeds:
//
//	PASS — the seed converged the cluster (or was already converged)
//	SKIP — a sponsor input was absent/placeholder (SKIP-graceful semantics)
//	FAIL — a typed error the operator must fix (bad mode, malformed input, k8s error)
package seed

import (
	"context"
	"errors"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/exitcode"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrNoClientCtor signals that a seed asked for an alternate-context KubeClient
// but the composition root did not wire a ClientCtor factory. The seed maps
// this to FAIL — the operator must fix the composition root, not the seed.
var ErrNoClientCtor = errors.New("seed: Deps.ClientCtor not wired (multi-cluster seed needs a ClientCtor)")

// Verdict is the tri-state outcome of a seed Run/Verify. It mirrors the
// PASS/SKIP/FAIL vocabulary the bash seeds print, so parity is a verdict
// comparison, not a string diff.
type Verdict int

const (
	// PASS — the seed applied (or the cluster was already converged).
	PASS Verdict = iota
	// SKIP — a required sponsor input was absent or a placeholder; not an error.
	SKIP
	// FAIL — a typed error the operator must resolve.
	FAIL
)

func (v Verdict) String() string {
	switch v {
	case PASS:
		return "PASS"
	case SKIP:
		return "SKIP"
	case FAIL:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// ExitCode maps a verdict onto loopzero-ops' exit table (internal/exitcode):
// PASS/SKIP are success; FAIL is the same failure status a gate returns.
func (v Verdict) ExitCode() int {
	if v == FAIL {
		return exitcode.Failure
	}
	return exitcode.OK
}

// Result is a verdict plus a human-readable reason. Reason is REQUIRED for
// SKIP and FAIL so the operator (and the parity comparison) knows why.
type Result struct {
	Verdict Verdict
	Reason  string
}

// Deps carries the runtime dependencies a seed needs. It is populated once by
// the composition root (seed_subcmd.go) — never constructed inside seed logic
// (architecture.md § Dependency Inversion). DryRun makes Run print intended
// actions without mutating the cluster.
//
// Clients holds additional per-context KubeClients for multi-cluster seeds
// (build / workload / vault). The default Kube points at the k3d-loopzero-core
// context; alternate contexts are resolved lazily via Deps.ClientFor.
type Deps struct {
	Ctx        context.Context
	Kube       KubeClient // mgmt (default) context
	Clients    map[string]KubeClient
	ClientCtor func(context string) (KubeClient, error) // lazy factory for missing contexts
	// PortForwardCtor opens a Tunnel to a Pod matching podLabels in namespace.
	// Composed by the composition root so seeds do not import client-go rest
	// config directly. Seeds pass namespace/labels/remotePort/probePath.
	PortForwardCtor func(ctx context.Context, target TunnelTarget) (*Tunnel, error)
	// RestConfig + CtrlClient are the raw client-go artifacts for the mgmt
	// context — kept on Deps so the PortForwardCtor default implementation
	// (in the composition root) can lazily resolve pods without re-reading
	// kubeconfig. Test injection substitutes them.
	RestConfig *rest.Config
	CtrlClient ctrlclient.Client
	// Host is what the seed reads from outside the cluster (environment,
	// local tools, HTTP, Postgres). The zero value uses the real ones.
	Host   Host
	Creds  *Creds
	DryRun bool
	// Policy answers whether a missing capability input is tolerable in this
	// environment. Nil means nothing is required, which keeps the behaviour from
	// before 2026-08-11 for every caller that does not inject it. Set only at
	// the composition root, like the rest of Deps.
	Policy RequirementPolicy
	// RolloutSettleTimeout is how long a seed waits for a Deployment to settle
	// before moving on. Zero (or negative) means the seed's own default, which
	// keeps the behaviour from before 2026-08-13 for every caller that does not
	// inject it — the same contract as Policy above, for the same reason.
	//
	// It exists because the wait is the ENVIRONMENT's policy, not the seed's:
	// against a real cluster 120 s is right; against a hermetic test's fake
	// client the Deployment never settles and the value is pure wall clock.
	// Measured: four internal/seed tests spent 390 s of the 408 s of the whole
	// cmd/loopzero-ops suite, and that cost kept the gate tests off the push path
	// (B-101, and item 2 of B-088).
	RolloutSettleTimeout time.Duration
	// CellContextPrefix is what a cell's kube context is named, before its cell id.
	// Nil keeps the k3d convention (`k3d-loopzero-<cell>`); an environment whose
	// kubeconfig names each context after the cell id (staging's EKS clusters) sets it
	// to "". The composition root reads it from LOOPZERO_CELL_CONTEXT_PREFIX.
	CellContextPrefix *string
}

// cellContext is the kube context of cellID under CellContextPrefix.
func (d Deps) cellContext(cellID string) string {
	if d.CellContextPrefix == nil {
		return brokerCellContext(cellID)
	}
	return *d.CellContextPrefix + cellID
}

// settleTimeout resolves the effective wait: an injected value wins, and zero
// falls back to the default the seed passes.
//
// The default is an ARGUMENT, not a package constant, because the defaults
// differ on purpose — loopzero-api waits 120 s and CoreDNS 60 s. A single default
// would silently double the CoreDNS wait, a production behaviour change
// smuggled inside a test speed-up.
func (d Deps) settleTimeout(seedDefault time.Duration) time.Duration {
	if d.RolloutSettleTimeout <= 0 {
		return seedDefault
	}
	return d.RolloutSettleTimeout
}

// ClientFor returns the KubeClient for the given kubeconfig context name.
// The empty string returns the default (mgmt) client. Missing entries are
// lazily constructed via ClientCtor; a nil ClientCtor + missing entry is a
// FAIL-shaped error (the operator forgot to wire the second cluster).
func (d *Deps) ClientFor(ctxName string) (KubeClient, error) {
	if ctxName == "" {
		return d.Kube, nil
	}
	if k, ok := d.Clients[ctxName]; ok && k != nil {
		return k, nil
	}
	if d.ClientCtor == nil {
		return nil, ErrNoClientCtor
	}
	k, err := d.ClientCtor(ctxName)
	if err != nil {
		return nil, err
	}
	if d.Clients == nil {
		d.Clients = map[string]KubeClient{}
	}
	d.Clients[ctxName] = k
	return k, nil
}

// Seed is one migrated canonical seed. Name is the CLI token (e.g. "github-app").
// Run applies the seed; Verify probes the post-condition (the --verify path).
type Seed interface {
	Name() string
	Run(Deps) Result
	Verify(Deps) Result
}

// Registry holds the seeds in deterministic insertion order (the canonical
// chain order). Iteration is stable — never map-random — so `seed all` runs
// the chain in the documented dependency order.
type Registry struct {
	names  []string
	byName map[string]Seed
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Seed)}
}

// Register adds a seed. It panics on a duplicate name — that is a programming
// error (two seeds claiming the same CLI token), not a runtime condition.
func (r *Registry) Register(s Seed) {
	if _, dup := r.byName[s.Name()]; dup {
		panic("seed: duplicate registration for " + s.Name())
	}
	r.byName[s.Name()] = s
	r.names = append(r.names, s.Name())
}

// Names returns the registered seed names in insertion order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.names))
	copy(out, r.names)
	return out
}

// Lookup returns the seed for name, or (nil, false) if unregistered.
func (r *Registry) Lookup(name string) (Seed, bool) {
	s, ok := r.byName[name]
	return s, ok
}
