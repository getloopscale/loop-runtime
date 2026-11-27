// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argocd

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
)

// failingProvisioner returns err from every call.
type failingProvisioner struct{ err error }

func (f failingProvisioner) EnsureAppProject(context.Context, AppProjectSpec) error   { return f.err }
func (f failingProvisioner) EnsureApplication(context.Context, ApplicationSpec) error { return f.err }
func (f failingProvisioner) DeleteApplications(context.Context, string) error         { return f.err }
func (f failingProvisioner) DeleteAppProject(context.Context, string) error           { return f.err }
func (f failingProvisioner) DeleteApplication(context.Context, string) error          { return f.err }
func (f failingProvisioner) ListApplicationNames(context.Context, string) ([]string, error) {
	return nil, f.err
}

func breakerWithThreshold(n int) *circuitbreaker.Breaker {
	return circuitbreaker.New(circuitbreaker.Config{Name: "argocd", OpenThreshold: n, RetryInterval: time.Hour}, nil)
}

// Every decorated call goes through circuitbreaker.Execute, so a cancelled
// reconcile does not count as an ArgoCD failure and cannot open the breaker.
func TestCircuitBreakerProvisioner_CancelledCallsDoNotOpenTheBreaker(t *testing.T) {
	calls := map[string]func(*CircuitBreakerProvisioner) error{
		"EnsureAppProject": func(p *CircuitBreakerProvisioner) error {
			return p.EnsureAppProject(context.Background(), AppProjectSpec{})
		},
		"EnsureApplication": func(p *CircuitBreakerProvisioner) error {
			return p.EnsureApplication(context.Background(), ApplicationSpec{})
		},
		"DeleteApplications": func(p *CircuitBreakerProvisioner) error { return p.DeleteApplications(context.Background(), "tn") },
		"DeleteAppProject":   func(p *CircuitBreakerProvisioner) error { return p.DeleteAppProject(context.Background(), "tn") },
		"DeleteApplication":  func(p *CircuitBreakerProvisioner) error { return p.DeleteApplication(context.Background(), "app") },
		"ListApplicationNames": func(p *CircuitBreakerProvisioner) error {
			_, err := p.ListApplicationNames(context.Background(), "tn")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			breaker := breakerWithThreshold(2)
			p := NewCircuitBreakerProvisioner(failingProvisioner{err: context.Canceled}, breaker, slog.New(slog.NewTextHandler(io.Discard, nil)))

			for i := 0; i < 3; i++ {
				if err := call(p); !errors.Is(err, context.Canceled) {
					t.Fatalf("call %d error = %v, want context.Canceled", i, err)
				}
			}

			if breaker.State() != circuitbreaker.Closed {
				t.Fatalf("breaker = %s after cancelled calls, want closed", breaker.State())
			}
		})
	}
}

// A real ArgoCD failure still opens the breaker, and an open breaker skips
// the call.
func TestCircuitBreakerProvisioner_RealFailuresOpenTheBreaker(t *testing.T) {
	breaker := breakerWithThreshold(2)
	p := NewCircuitBreakerProvisioner(failingProvisioner{err: errors.New("argocd down")}, breaker, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_ = p.DeleteApplication(context.Background(), "app")
	_ = p.DeleteApplication(context.Background(), "app")

	if breaker.State() != circuitbreaker.Open {
		t.Fatalf("breaker = %s, want open after two failures", breaker.State())
	}
	if err := p.DeleteApplication(context.Background(), "app"); !errors.Is(err, circuitbreaker.ErrCircuitOpen) {
		t.Fatalf("call with an open breaker = %v, want ErrCircuitOpen", err)
	}
}
