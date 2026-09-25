// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// decisionsFixture is a decision record carrying one row the test registries cite.
var decisionsFixture = []byte("| 2026-09-25 | **Fixture decision** |\n")

const validKind = `
kinds:
  - pair: loopzero-api:loopzero-api
    kind: ConfigMap
    decision: Fixture decision
    reason: the published chart mounts configuration through a ConfigMap; bootstrap uses env
`

func cellsRegistry(status, cell string) string {
	return `
cells:
  - pair: loopzero-api:loopzero-api
    side: bootstrap-only
    rule: "apiGroups [(core)], resources [secrets], verbs [delete]"
    decision: Fixture decision
    status: ` + status + `
    reason: bootstrap grants the cell and the published chart withholds it under every flag
    cells:
      - ` + cell + "\n"
}

func TestParseRegistry_AcceptsAWellFormedRegistry(t *testing.T) {
	reg, err := parseRegistry([]byte(validKind+cellsRegistry(statusDeclared, "(core)/secrets/delete")), decisionsFixture)
	if err != nil {
		t.Fatalf("parseRegistry: %v", err)
	}
	if _, ok := reg.kind("loopzero-api:loopzero-api", "ConfigMap"); !ok {
		t.Error("the ConfigMap entry was not loaded")
	}
	a, ok := reg.cell("loopzero-api:loopzero-api", Cell{Resource: "secrets", Verb: "delete"})
	if !ok || a.Status != statusDeclared || a.Rule == "" {
		t.Errorf("the secrets/delete cell was not loaded with its rule and status: %+v", a)
	}
}

// Every way an entry can be untrustworthy is refused at load, so the gate never
// runs against a registry that would license something nobody recorded.
func TestParseRegistry_RefusesAnUntrustworthyEntry(t *testing.T) {
	cases := map[string]string{
		"decision missing from the record":    strings.Replace(validKind, "Fixture decision", "B-110", 1),
		"reason too short":                    strings.Replace(validKind, "the published chart mounts configuration through a ConfigMap; bootstrap uses env", "differs", 1),
		"file-name key in the kind registry":  strings.Replace(validKind, "kind: ConfigMap", "kind: configmap.yaml", 1),
		"pair not shaped published:bootstrap": strings.Replace(validKind, "pair: loopzero-api:loopzero-api", "pair: loopzero-api", 1),
		"unknown status":                      cellsRegistry("reviewed", "(core)/secrets/delete"),
		"kind-shaped cell key":                cellsRegistry(statusDeclared, "ConfigMap"),
		"cell with an empty segment":          cellsRegistry(statusDeclared, "(core)//delete"),
		"the same kind recorded twice":        validKind + strings.TrimPrefix(validKind, "\nkinds:\n"),
		"an unknown field":                    validKind + "extra: true\n",
		"a registry that records nothing":     "kinds: []\ncells: []\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRegistry([]byte(raw), decisionsFixture); !errors.Is(err, ErrRegistryMalformed) {
				t.Fatalf("want ErrRegistryMalformed, got %v", err)
			}
		})
	}
}

func TestLoadRegistry_MissingFileIsAnError(t *testing.T) {
	if _, err := LoadRegistry(t.TempDir()); !errors.Is(err, ErrRegistryMalformed) {
		t.Fatalf("want ErrRegistryMalformed for an absent registry, got %v", err)
	}
}

// The committed registry loads, and every decision it cites is a row of
// docs/decisions.md — which LoadRegistry itself enforces.
func TestCommittedRegistryLoads(t *testing.T) {
	reg := realRegistry(t)
	if len(reg.kinds) == 0 || len(reg.cells) == 0 {
		t.Fatalf("the committed registry holds %d kind(s) and %d cell(s); both comparisons have "+
			"recorded divergences", len(reg.kinds), len(reg.cells))
	}
}

// Negative case: a cell with an empty resource must not produce a key anything
// can match, because such a key would approve every cell of that shape.
func TestCellApprovalKeyWithAnEmptyResourceIsRefused(t *testing.T) {
	key := cellApprovalKey("loopzero-api:loopzero-api", Cell{Resource: "", Verb: "delete"})
	cell := strings.TrimPrefix(key, "loopzero-api:loopzero-api:")
	if isCellKey(cell) {
		t.Errorf("cell %q was accepted despite an empty segment", cell)
	}
}

// The registry must be load-bearing PER CELL. A gate that goes green over a
// complete registry proves nothing until removing ONE entry turns it red — and
// turns it red naming exactly that cell, not the pair.
//
// Hermetic: the fixture pair diverges on six cells, all six are declared, and one
// declaration is withdrawn. If the lookup were pair-wide the gate would stay
// green; if it failed the whole pair it would report six. Exactly one is the
// answer that distinguishes a per-cell registry from a blessing.
func TestRemovingOneDeclarationFailsTheGateNamingThatCell(t *testing.T) {
	const pair = "loopzero-declprobe:loopzero-declprobe"
	bootstrap := []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: declprobe
rules:
  - apiGroups: [""]
    resources: ["secrets", "configmaps"]
    verbs: ["get", "list", "delete"]
`)
	published := []byte(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: declprobe
rules: []
`)

	reg := Registry{cells: map[string]rbacApproval{}}
	for _, c := range []string{
		"(core)/configmaps/delete", "(core)/configmaps/get", "(core)/configmaps/list",
		"(core)/secrets/delete", "(core)/secrets/get", "(core)/secrets/list",
	} {
		reg.cells[pair+":"+c] = rbacApproval{
			Decision: "Fixture decision", Status: statusDeclared,
			Rule:   `apiGroups [(core)], resources [secrets, configmaps], verbs [get, list, delete]`,
			Reason: "fixture declaration for the per-cell registry test",
		}
	}

	out, drift := parityTreeWith(t, "loopzero-declprobe", bootstrap, published, reg)
	if drift == nil {
		t.Fatal("the fixture pair diverges on six cells and produced no finding")
	}
	if !out.Pass || len(drift.Undeclared) != 0 || drift.Declared != 6 {
		t.Fatalf("with all six declared the gate must be green and report 6 declared, "+
			"0 undeclared; got pass=%v declared=%d undeclared=%v",
			out.Pass, drift.Declared, drift.Undeclared)
	}

	delete(reg.cells, pair+":(core)/secrets/delete")

	out, drift = parityTreeWith(t, "loopzero-declprobe", bootstrap, published, reg)
	if drift == nil {
		t.Fatal("withdrawing a declaration removed the finding altogether")
	}
	if out.Pass {
		t.Error("an undeclared cell must fail the gate")
	}
	if len(drift.Undeclared) != 1 || drift.Undeclared[0].String() != "(core)/secrets/delete" {
		t.Fatalf("expected exactly one undeclared cell, (core)/secrets/delete — one, and not "+
			"six, is what tells a per-cell registry from a pair-wide blessing; got %v",
			drift.Undeclared)
	}
	if !strings.Contains(strings.Join(drift.ReportLines(), "\n"), "UNDECLARED (core)/secrets/delete") {
		t.Errorf("the report does not name the withdrawn cell: %v", drift.ReportLines())
	}
}

// An undecided cell is counted as declared for the VERDICT — a gate nobody can
// turn green is a gate people route around — and listed by name in the report, so
// "recorded" can never be read as "approved".
func TestAnUndecidedCellIsReportedSeparatelyFromADeclaredOne(t *testing.T) {
	reg := realRegistry(t)
	var undecided []string
	for key, a := range reg.cells {
		if a.Status == statusUndecided {
			undecided = append(undecided, key)
			if a.Decision != openRBACDecision {
				t.Errorf("%s cites %q; an undecided cell must cite the open decision it waits on", key, a.Decision)
			}
		}
	}
	sort.Strings(undecided)
	want := []string{"loopzero-api:loopzero-api:(core)/pods/exec/create", "loopzero-api:loopzero-api:(core)/secrets/delete"}
	if strings.Join(undecided, ",") != strings.Join(want, ",") {
		t.Fatalf("expected the two security cells to be the only undecided entries, got %v", undecided)
	}

	d := RBACDrift{
		Pair:      "loopzero-api:loopzero-api",
		Declared:  2,
		Undecided: []Cell{{Resource: "secrets", Verb: "delete"}},
	}
	lines := strings.Join(d.ReportLines(), "\n")
	if !strings.Contains(lines, "UNDECIDED (core)/secrets/delete") {
		t.Errorf("an undecided cell is not named in the report: %s", lines)
	}
	if strings.Contains(lines, "UNDECLARED (core)/secrets/delete") {
		t.Errorf("an undecided cell was reported as undeclared: %s", lines)
	}
}
