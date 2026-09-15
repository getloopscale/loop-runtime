// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"sort"
	"strings"
	"testing"
)

// B-001 T4.1. The cell registry carries the same teeth as the kind registry next
// door, plus the one field it needs.
//
// It is a SECOND registry rather than more rows in the first, and the reason is
// mechanical rather than aesthetic: TestEveryApprovalIsMeasuredAssimetry requires
// the third key part to be a Kubernetes Kind — uppercase first letter, no dot —
// and `(core)/secrets/delete` fails on both counts. Reusing the registry would
// have meant weakening the assertion that cut twenty-one kind entries to eleven,
// thirteen of which described nothing that diverged.

// rbacApprovalDefects returns what is wrong with one entry, if anything. It lives
// in the test file because it exists to judge the registry, and a judge compiled
// into the gate would be a symbol the gate itself never calls.
func rbacApprovalDefects(key string, a rbacApproval) []string {
	var defects []string
	if strings.TrimSpace(a.ADR) == "" {
		defects = append(defects, "carries no ADR — an approval nobody signed")
	}
	if len(strings.TrimSpace(a.Reason)) < 30 {
		defects = append(defects, "reason is too short to say what was observed about the two charts")
	}
	switch a.Status {
	case statusDeclared, statusUndecided:
	default:
		defects = append(defects, "status "+a.Status+" is neither "+statusDeclared+" nor "+
			statusUndecided+" — a typo must not invent a third claim")
	}

	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return append(defects, "is not shaped published:bootstrap:apiGroup/resource/verb")
	}
	cell := parts[2]
	// A Kind-shaped key would be a kind-era entry surviving into this registry,
	// approving something this comparison does not measure.
	segments := strings.Split(cell, "/")
	if len(segments) < 3 {
		return append(defects, "third part "+cell+" is not a cell — a Kind-shaped key here "+
			"would approve something the cell comparison never looks at")
	}
	for _, s := range segments {
		if s == "" {
			defects = append(defects, "third part "+cell+" has an empty segment, which would "+
				"match every cell of that shape")
			break
		}
	}
	return defects
}

func TestEveryRBACApprovalIsMeasuredAssimetry(t *testing.T) {
	if len(approvedRBACCells) == 0 {
		t.Fatal("no RBAC approvals to check — the test would pass by vacuity, which is the " +
			"state a registry gate must never be able to reach")
	}
	for key, a := range approvedRBACCells {
		for _, d := range rbacApprovalDefects(key, a) {
			t.Errorf("%s: %s", key, d)
		}
	}
}

func TestCellRegistryRefusesAKindShapedKey(t *testing.T) {
	got := rbacApprovalDefects("loopzero-api:loopzero-api:ConfigMap", rbacApproval{
		ADR: "B-110", Status: statusDeclared,
		Reason: "a kind-era entry that migrated into the cell registry by accident",
	})
	if len(got) == 0 {
		t.Error("a Kind-shaped key was accepted by the cell registry's shape check")
	}
}

func TestCellRegistryRefusesAnUnknownStatus(t *testing.T) {
	got := rbacApprovalDefects("loopzero-api:loopzero-api:(core)/secrets/delete", rbacApproval{
		ADR: "B-001", Status: "reviewed",
		Reason: "a status nobody defined, which would read as a third kind of claim",
	})
	if len(got) == 0 {
		t.Error("an unknown status was accepted; the enum is what stops the registry making " +
			"a claim nobody defined")
	}
}

// Negative case: a cell with an empty resource must not produce a key anything
// can match, because such a key would approve every cell of that shape.
func TestCellApprovalKeyWithAnEmptyResourceIsRefused(t *testing.T) {
	key := cellApprovalKey("loopzero-api:loopzero-api", Cell{Resource: "", Verb: "delete"})
	if len(rbacApprovalDefects(key, rbacApproval{
		ADR: "B-001", Status: statusDeclared,
		Reason: "an entry whose resource segment is empty, which matches by accident",
	})) == 0 {
		t.Errorf("key %q was accepted despite an empty segment", key)
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

	cells := []string{
		"(core)/configmaps/delete", "(core)/configmaps/get", "(core)/configmaps/list",
		"(core)/secrets/delete", "(core)/secrets/get", "(core)/secrets/list",
	}
	for _, c := range cells {
		approvedRBACCells[pair+":"+c] = rbacApproval{
			ADR: "B-001", Status: statusDeclared,
			Reason: "fixture declaration installed by TestRemovingOneDeclarationFailsTheGate" +
				"NamingThatCell; removed again before the test returns",
		}
	}
	defer func() {
		for _, c := range cells {
			delete(approvedRBACCells, pair+":"+c)
		}
	}()

	out, drift := parityTreeNamed(t, "loopzero-declprobe", bootstrap, published)
	if drift == nil {
		t.Fatal("the fixture pair diverges on six cells and produced no finding")
	}
	if !out.Pass || len(drift.Undeclared) != 0 || drift.Declared != 6 {
		t.Fatalf("with all six declared the gate must be green and report 6 declared, "+
			"0 undeclared; got pass=%v declared=%d undeclared=%v",
			out.Pass, drift.Declared, drift.Undeclared)
	}

	withdrawn := pair + ":(core)/secrets/delete"
	delete(approvedRBACCells, withdrawn)

	out, drift = parityTreeNamed(t, "loopzero-declprobe", bootstrap, published)
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
	var undecided []string
	for key, a := range approvedRBACCells {
		if a.Status == statusUndecided {
			undecided = append(undecided, key)
		}
	}
	sort.Strings(undecided)
	if len(undecided) != 2 {
		t.Fatalf("expected the two security cells to be the only undecided entries, got %v", undecided)
	}
	for _, key := range undecided {
		if !strings.Contains(approvedRBACCells[key].Reason, "B-048") {
			t.Errorf("%s does not name the item the decision belongs to", key)
		}
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
