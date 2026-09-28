// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package pgpdb is the Go home for the postgresql-pdb-config gate (migrated from
// infra/tests/scripts/postgresql-pdb-config_test.sh, plan 2026-05-23).
//
// B2 regression (real-paas-cleanroom-dogfood-loop iter 2): the Bitnami
// postgresql chart MUST NOT create its own PodDisruptionBudget. LoopZero applies its
// own PDB (infra/helm/postgresql/templates/pdb.yaml, minAvailable: 1); Bitnami's
// chart-owned PDB uses maxUnavailable: 1, and K8s rejects a PDB with both fields
// set ("minAvailable and maxUnavailable cannot be both set"). The fix is
// `primary.pdb.create: false`; this gate guards it against accidental removal.
package pgpdb

import (
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	"gopkg.in/yaml.v3"
)

// Outcome is the gate result.
type Outcome struct {
	Pass    bool
	Message string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check asserts that primary.pdb.create is explicitly false in the given
// postgresql values.yaml. Faithful to the original: pdb missing, create absent,
// or create != false all fail (only an explicit `false` passes).
func Check(valuesPath string) (*Outcome, error) {
	// #nosec G304 -- valuesPath is a CI-supplied repo path, not user input.
	data, err := os.ReadFile(valuesPath)
	if err != nil {
		return nil, fmt.Errorf("read values.yaml %q: %w", valuesPath, err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse values.yaml %q: %w", valuesPath, err)
	}

	primary, _ := root["primary"].(map[string]any)
	pdb, ok := primary["pdb"].(map[string]any)
	if !ok {
		return &Outcome{Pass: false, Message: "primary.pdb missing — required: primary.pdb.create == false (B2 regression)"}, nil
	}
	create, isBool := pdb["create"].(bool)
	if !isBool || create {
		return &Outcome{Pass: false, Message: fmt.Sprintf("primary.pdb.create must be false, got %v (B2 regression)", pdb["create"])}, nil
	}
	return &Outcome{Pass: true, Message: "primary.pdb.create is false (B2 regression OK)"}, nil
}
