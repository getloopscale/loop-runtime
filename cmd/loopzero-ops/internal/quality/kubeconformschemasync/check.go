// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package kubeconformschemasync provides the `kubeconform-schemas-sync`
// quality gate: every committed loopzero.io strict kubeconform schema is exactly
// what `loopzero-ops kubeconform-schemas` produces from the current CRDs.
//
// Why this gate exists (#389): the schemas are a third copy of each CRD, after
// operators/config/crd/bases and charts/loopzero-crds. crd-semantic-diff compares
// the first two; nothing compared the third, and six of the eight had drifted —
// the offline validation accepted manifests the API server rejects.
package kubeconformschemasync

import (
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/kubeconformschema"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// Fix is the remediation the gate prints on a failure.
const Fix = "run `task helm:sync-kubeconform-schemas` (go run ./cmd/loopzero-ops kubeconform-schemas) and commit the result"

// Config locates the repository.
type Config struct {
	RepoRoot string
}

// Outcome is what the gate measured.
type Outcome struct {
	// Schemas is the number of schemas the CRDs produce.
	Schemas int
	// Drift lists every committed schema out of step with the CRDs.
	Drift []kubeconformschema.Drift
}

// Verdict is Pass when no committed schema drifted, and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.Of(o.Drift) }

// Check compares the committed schemas with the ones the CRDs produce. The
// error means the comparison could not run (no CRD, an unreadable file); drift
// is never an error.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	crdDir := filepath.Join(root, kubeconformschema.DefaultCRDDir)
	schemaDir := filepath.Join(root, kubeconformschema.DefaultSchemaDir)
	want, err := kubeconformschema.Generate(crdDir)
	if err != nil {
		return Outcome{}, err
	}
	drift, err := kubeconformschema.Compare(want, schemaDir)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Schemas: len(want), Drift: drift}, nil
}
