// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RegistryRelPath is the versioned registry of recorded chart-pair divergences.
//
// It is data and lives beside chart-pairs.txt rather than in Go literals: the
// registry is what a reviewer reads to learn why two charts differ, and each
// entry names the docs/decisions.md row it rests on, so the licence for a
// divergence travels with every clone.
const RegistryRelPath = "infra/quality/helm-drift-registry.yaml"

// decisionsRelPath is the decision record every registry entry must cite.
const decisionsRelPath = "docs/decisions.md"

// minReasonLength is the shortest reason that can say what was observed about
// the two charts; anything shorter is a label, not an observation.
const minReasonLength = 30

// ErrRegistryMalformed means the registry is missing, unparseable, or holds an
// entry the gate refuses to trust.
var ErrRegistryMalformed = errors.New("helm-drift registry missing or malformed")

// Registry holds every recorded divergence, keyed the way the report spells a
// finding so matching a finding to its entry is a literal string comparison.
type Registry struct {
	// kinds is keyed "published:bootstrap:Kind".
	kinds map[string]approval
	// cells is keyed "published:bootstrap:apiGroup/resource/verb".
	cells map[string]rbacApproval
}

// approval is one resource kind's licence to differ between the two charts.
type approval struct {
	Decision string
	Reason   string
}

// kind returns the entry recorded for a kind of one pair.
func (r Registry) kind(pair, kind string) (approval, bool) {
	a, ok := r.kinds[pair+":"+kind]
	return a, ok
}

// cell returns the entry recorded for one RBAC cell of one pair.
func (r Registry) cell(pair string, c Cell) (rbacApproval, bool) {
	a, ok := r.cells[cellApprovalKey(pair, c)]
	return a, ok
}

type registryFile struct {
	Kinds []struct {
		Pair     string `yaml:"pair"`
		Kind     string `yaml:"kind"`
		Decision string `yaml:"decision"`
		Reason   string `yaml:"reason"`
	} `yaml:"kinds"`
	Cells []struct {
		Pair     string   `yaml:"pair"`
		Side     string   `yaml:"side"`
		Rule     string   `yaml:"rule"`
		Decision string   `yaml:"decision"`
		Status   string   `yaml:"status"`
		Reason   string   `yaml:"reason"`
		Cells    []string `yaml:"cells"`
	} `yaml:"cells"`
}

// LoadRegistry reads RegistryRelPath under repoRoot and refuses any entry it
// cannot trust: a decision docs/decisions.md does not record, a reason too
// short to be an observation, an unknown status or side, a key of the wrong
// shape, or the same kind or cell recorded twice.
func LoadRegistry(repoRoot string) (Registry, error) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, RegistryRelPath))
	if err != nil {
		return Registry{}, fmt.Errorf("%w: %w", ErrRegistryMalformed, err)
	}
	decisions, err := os.ReadFile(filepath.Join(repoRoot, decisionsRelPath))
	if err != nil {
		return Registry{}, fmt.Errorf("%w: the decision record every entry cites: %w", ErrRegistryMalformed, err)
	}
	return parseRegistry(raw, decisions)
}

func parseRegistry(raw, decisions []byte) (Registry, error) {
	var f registryFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Registry{}, fmt.Errorf("%w: %s: %w", ErrRegistryMalformed, RegistryRelPath, err)
	}
	reg := Registry{kinds: map[string]approval{}, cells: map[string]rbacApproval{}}
	var defects []string
	for i, k := range f.Kinds {
		key := k.Pair + ":" + k.Kind
		where := fmt.Sprintf("kinds[%d] %s", i, key)
		defects = append(defects, entryDefects(where, k.Pair, k.Decision, k.Reason, decisions)...)
		if !isKindName(k.Kind) {
			defects = append(defects, fmt.Sprintf("%s: %q is not a Kubernetes kind", where, k.Kind))
		}
		if _, dup := reg.kinds[key]; dup {
			defects = append(defects, where+": recorded twice")
		}
		reg.kinds[key] = approval{Decision: k.Decision, Reason: strings.TrimSpace(k.Reason)}
	}
	for i, g := range f.Cells {
		where := fmt.Sprintf("cells[%d] %s %s", i, g.Pair, g.Rule)
		defects = append(defects, entryDefects(where, g.Pair, g.Decision, g.Reason, decisions)...)
		if g.Status != statusDeclared && g.Status != statusUndecided {
			defects = append(defects, fmt.Sprintf("%s: status %q is neither %s nor %s", where, g.Status, statusDeclared, statusUndecided))
		}
		if g.Side != sideBootstrapOnly && g.Side != sidePublishedOnly {
			defects = append(defects, fmt.Sprintf("%s: side %q is neither %s nor %s", where, g.Side, sideBootstrapOnly, sidePublishedOnly))
		}
		if strings.TrimSpace(g.Rule) == "" {
			defects = append(defects, where+": names no rule")
		}
		if len(g.Cells) == 0 {
			defects = append(defects, where+": lists no cell")
		}
		for _, c := range g.Cells {
			if !isCellKey(c) {
				defects = append(defects, fmt.Sprintf("%s: %q is not an apiGroup/resource/verb cell", where, c))
				continue
			}
			key := g.Pair + ":" + c
			if _, dup := reg.cells[key]; dup {
				defects = append(defects, fmt.Sprintf("%s: cell %s recorded twice", where, c))
			}
			reg.cells[key] = rbacApproval{
				Decision: g.Decision,
				Rule:     g.Rule,
				Reason:   strings.TrimSpace(g.Reason),
				Status:   g.Status,
			}
		}
	}
	if len(reg.kinds)+len(reg.cells) == 0 {
		defects = append(defects, "records nothing — an empty registry would read as a clean one")
	}
	if len(defects) > 0 {
		return Registry{}, fmt.Errorf("%w: %s:\n  %s", ErrRegistryMalformed, RegistryRelPath, strings.Join(defects, "\n  "))
	}
	return reg, nil
}

// entryDefects checks what every entry needs: a pair, a decision recorded in
// docs/decisions.md, and a reason long enough to be an observation.
func entryDefects(where, pair, decision, reason string, decisions []byte) []string {
	var defects []string
	if strings.Count(pair, ":") != 1 || strings.HasPrefix(pair, ":") || strings.HasSuffix(pair, ":") {
		defects = append(defects, fmt.Sprintf("%s: pair %q is not published:bootstrap", where, pair))
	}
	switch {
	case strings.TrimSpace(decision) == "":
		defects = append(defects, where+": cites no decision — an approval nobody made")
	case !bytes.Contains(decisions, []byte("**"+decision+"**")):
		defects = append(defects, fmt.Sprintf("%s: decision %q is not a row of %s", where, decision, decisionsRelPath))
	}
	if len(strings.TrimSpace(reason)) < minReasonLength {
		defects = append(defects, where+": reason is too short to say what was observed about the two charts")
	}
	return defects
}

// isKindName reports whether s is shaped like a Kubernetes kind: it starts
// with an uppercase letter and is not a file name. A file-name key would be an
// entry from the era when drift was compared by template file, approving
// something this gate no longer measures.
func isKindName(s string) bool {
	return s != "" && s[0] >= 'A' && s[0] <= 'Z' && !strings.Contains(s, ".")
}

// isCellKey reports whether s is shaped apiGroup/resource/verb with no empty
// segment. A Kind-shaped key would approve something the cell comparison
// never looks at, and an empty segment would match every cell of that shape.
func isCellKey(s string) bool {
	segments := strings.Split(s, "/")
	if len(segments) < 3 {
		return false
	}
	for _, seg := range segments {
		if seg == "" {
			return false
		}
	}
	return true
}
