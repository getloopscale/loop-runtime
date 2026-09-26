// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ErrRegistryMalformed means the registry does not declare what it must.
//
// A registry that accepts garbage declares nothing. It is the same reason the
// `publishhygiene` exemption requires a reason AND a deadline: without both it does not
// declare, it only silences.
var ErrRegistryMalformed = errors.New("malformed layer registry")

// GuardKind is how a layer is protected from degrading in silence.
type GuardKind string

const (
	// GuardGate — a loopzero-ops gate covers the layer. Resolves only if the gate is RUN.
	GuardGate GuardKind = "gate"
	// GuardTest — a test asserts the invariant.
	GuardTest GuardKind = "test"
	// GuardOpen — declared debt, with an owner. Resolves while the item is OPEN.
	GuardOpen GuardKind = "open"
)

// Guard is the declared protection of one layer.
type Guard struct {
	Kind   GuardKind
	Target string
}

// Layer is one independent place where missing billing configuration is tolerated.
type Layer struct {
	ID    string
	Repo  string
	Files []string
	Guard Guard
	Why   string
}

// Reference is a file that touches a billing input without being a layer.
type Reference struct {
	File   string
	Reason string
}

// Debt is one declared open item an `open:` guard points at. It lives in the
// versioned registry so the ratchet reads the same answer on every clone; the
// personal BACKLOG.md it used to read is gitignored and absent in CI.
type Debt struct {
	ID     string
	Status string
	Closes string // what delivering the item puts in place of the debt
}

// Registry is the declared state of the billing chain.
type Registry struct {
	// Anchor lives HERE, as data, and not as literals in Go — see the package doc.
	Anchor     []string
	Floor      int
	Layers     []Layer
	References []Reference
	Debts      map[string]Debt
}

// ParseRegistry reads the declared registry.
// knownSections are the registry's section names. An unknown one is refused by
// name rather than letting its lines read as "outside any section".
var knownSections = map[string]bool{
	"anchor": true, "floor": true, "layers": true, "references": true, "debts": true,
}

func ParseRegistry(r io.Reader) (Registry, error) {
	var reg Registry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 256*1024), 256*1024)

	section := ""
	floorRead := false
	seen := map[string]bool{}
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			if !knownSections[section] {
				return reg, fmt.Errorf("%w: unknown section [%s] on line %d — the sections are [anchor], [floor], [layers], [references] and [debts]",
					ErrRegistryMalformed, section, n)
			}
			continue
		}

		// The section names are the registry's format keys, matched literally
		// against the data file.
		switch section {
		case "anchor":
			reg.Anchor = append(reg.Anchor, line)
		case "floor":
			v, err := strconv.Atoi(line)
			if err != nil || v < 0 {
				return reg, fmt.Errorf("%w: invalid floor on line %d: %q", ErrRegistryMalformed, n, line)
			}
			reg.Floor = v
			floorRead = true
		case "layers":
			l, err := parseLayer(line, n)
			if err != nil {
				return reg, err
			}
			if seen[l.ID] {
				return reg, fmt.Errorf("%w: layer %q declared twice (line %d) — one hides the other and the floor stays satisfied",
					ErrRegistryMalformed, l.ID, n)
			}
			seen[l.ID] = true
			reg.Layers = append(reg.Layers, l)
		case "references":
			ref, err := parseReference(line, n)
			if err != nil {
				return reg, err
			}
			reg.References = append(reg.References, ref)
		case "debts":
			d, err := parseDebt(line, n)
			if err != nil {
				return reg, err
			}
			if reg.Debts == nil {
				reg.Debts = map[string]Debt{}
			}
			if _, dup := reg.Debts[d.ID]; dup {
				return reg, fmt.Errorf("%w: debt %s declared twice (line %d)", ErrRegistryMalformed, d.ID, n)
			}
			reg.Debts[d.ID] = d
		default:
			return reg, fmt.Errorf("%w: line %d is outside any section: %q", ErrRegistryMalformed, n, line)
		}
	}
	if err := sc.Err(); err != nil {
		return reg, fmt.Errorf("%w: %w", ErrRegistryMalformed, err)
	}

	// A missing anchor is not a simpler registry — it is a registry that cannot detect a
	// new layer, which is half of what it exists to do.
	if len(reg.Anchor) == 0 {
		return reg, fmt.Errorf("%w: section [anchor] is empty — without it, a new layer is born invisible", ErrRegistryMalformed)
	}
	// EC-4: with no declared floor, deleting lines makes the gate pass.
	if !floorRead {
		return reg, fmt.Errorf("%w: section [floor] is missing — it is what stops the registry from silencing itself", ErrRegistryMalformed)
	}
	return reg, nil
}

func parseLayer(line string, n int) (Layer, error) {
	fields := strings.Split(line, "|")
	if len(fields) != 5 {
		return Layer{}, fmt.Errorf("%w: layer on line %d has %d fields, want 5 (id | repo | files | guard | why)",
			ErrRegistryMalformed, n, len(fields))
	}
	l := Layer{
		ID:   strings.TrimSpace(fields[0]),
		Repo: strings.TrimSpace(fields[1]),
		Why:  strings.TrimSpace(fields[4]),
	}
	for _, f := range strings.Split(fields[2], ",") {
		if f = strings.TrimSpace(f); f != "" {
			l.Files = append(l.Files, f)
		}
	}
	g, err := parseGuard(fields[3], n)
	if err != nil {
		return Layer{}, err
	}
	l.Guard = g

	if l.ID == "" || l.Repo == "" || len(l.Files) == 0 || l.Why == "" {
		return Layer{}, fmt.Errorf("%w: layer on line %d has an empty field — a layer with no file or no reason declares nothing",
			ErrRegistryMalformed, n)
	}
	return l, nil
}

// parseGuard normalises whitespace and case (EC-7): `Gate: X` and `gate:X` are the same
// intent, and refusing on case is noise. Accepting an unknown form is a hole.
func parseGuard(raw string, n int) (Guard, error) {
	kind, target, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok {
		return Guard{}, fmt.Errorf("%w: guard with no form on line %d: %q", ErrRegistryMalformed, n, raw)
	}
	g := Guard{
		Kind:   GuardKind(strings.ToLower(strings.TrimSpace(kind))),
		Target: strings.TrimSpace(target),
	}
	switch g.Kind {
	case GuardGate, GuardTest, GuardOpen:
	default:
		return Guard{}, fmt.Errorf("%w: unknown guard %q on line %d — the accepted forms are `gate`, `test` and `open`",
			ErrRegistryMalformed, g.Kind, n)
	}
	if g.Target == "" {
		return Guard{}, fmt.Errorf("%w: guard with no target on line %d", ErrRegistryMalformed, n)
	}
	return g, nil
}

func parseReference(line string, n int) (Reference, error) {
	fields := strings.Split(line, "|")
	if len(fields) != 2 {
		return Reference{}, fmt.Errorf("%w: reference on line %d has %d fields, want 2 (file | why it is not a layer)",
			ErrRegistryMalformed, n, len(fields))
	}
	ref := Reference{File: strings.TrimSpace(fields[0]), Reason: strings.TrimSpace(fields[1])}
	if ref.File == "" || ref.Reason == "" {
		return Reference{}, fmt.Errorf("%w: reference on line %d with no file or no reason — a reference without a reason only silences",
			ErrRegistryMalformed, n)
	}
	return ref, nil
}

// parseDebt reads `<B-NNN> | <status> | <what closes it>`. The status must be
// one the ratchet can classify; anything else is refused rather than guessed.
func parseDebt(line string, n int) (Debt, error) {
	fields := strings.Split(line, "|")
	if len(fields) != 3 {
		return Debt{}, fmt.Errorf("%w: debt on line %d has %d fields, want 3 (id | status | what closes it)",
			ErrRegistryMalformed, n, len(fields))
	}
	d := Debt{
		ID:     strings.TrimSpace(fields[0]),
		Status: strings.TrimSpace(fields[1]),
		Closes: strings.TrimSpace(fields[2]),
	}
	if d.ID == "" || d.Closes == "" {
		return Debt{}, fmt.Errorf("%w: debt on line %d has an empty id or no closing criterion", ErrRegistryMalformed, n)
	}
	if !openStatuses[d.Status] && !terminalStatuses[d.Status] {
		return Debt{}, fmt.Errorf("%w: debt %s on line %d has status %q; known statuses are raw, triaged, approved, planned, shipped, killed",
			ErrRegistryMalformed, d.ID, n, d.Status)
	}
	return d, nil
}
