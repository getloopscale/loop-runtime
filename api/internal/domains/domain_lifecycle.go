// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domains

import (
	"context"
	"fmt"

	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// domainMachine is the one definition of the custom-domain lifecycle
// (pkg/statemachine/domain.go). Every status change the domain services make
// is derived from it — the services name an EVENT, never a target status, so
// the declared machine and the implemented lifecycle cannot drift apart.
var domainMachine = statemachine.NewDomainMachine()

// domainStatusCAS is the compare-and-set status write: it moves the domain to
// next only if it is still in expected, so a concurrent change (a user delete,
// the provisioner claiming it) makes the write fail instead of being overwritten.
type domainStatusCAS interface {
	TransitionStatus(ctx context.Context, domain, expectedStatus, newStatus string) error
}

// nextDomainStatus returns the status event leads to from current, or an error
// when the machine has no such transition.
func nextDomainStatus(domain, current string, event statemachine.Event) (string, error) {
	next, err := domainMachine.TryTransition(statemachine.State(current), event)
	if err != nil {
		return "", fmt.Errorf("domain %s: %w", domain, err)
	}
	return string(next), nil
}

// transitionDomain applies event to a domain the caller read in status current,
// validated by the machine and written as a compare-and-set from current.
func transitionDomain(ctx context.Context, store domainStatusCAS, domain, current string, event statemachine.Event) (string, error) {
	next, err := nextDomainStatus(domain, current, event)
	if err != nil {
		return "", err
	}
	if err := store.TransitionStatus(ctx, domain, current, next); err != nil {
		return "", err
	}
	return next, nil
}
