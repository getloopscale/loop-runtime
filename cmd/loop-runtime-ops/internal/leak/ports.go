// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package leak

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The host's network surface is declared, and closed by default.
//
// MEASURED 2026-08-10, from the internet and not from the droplet: 39 ports
// listened on 0.0.0.0, the firewall blocked 20, and of the remaining 19 only 2
// had a justification. The other 17 were on no list — among them ArgoCD, the
// secrets cluster serverlb and identity served WITHOUT TLS on the raw port while
// the edge already published it over HTTPS.
//
// The cause was not carelessness. The firewall's block list was written when
// the clusters had other names, and everything that came up afterwards was born
// outside it. A DENY list ages towards the unsafe side: what nobody remembered
// to block stays open. An ALLOW list ages towards the safe side, and it is the
// only acceptable polarity here.

// PortClass decides whether a port may be reached from the internet.
type PortClass string

const (
	// ClassExternal is the ONLY class that stays reachable from outside, and
	// it requires the consumer to be named with measured evidence.
	ClassExternal PortClass = "external"
	ClassEdge     PortClass = "edge"    // the edge publishes it over HTTPS; the raw port closes
	ClassHost     PortClass = "host"    // consumed by the host itself
	ClassAdmin    PortClass = "admin"   // infrastructure panel or API
	ClassPending  PortClass = "pending" // declared, consumer not yet measured
)

// PortDecl is one registry row.
type PortDecl struct {
	Port   int
	Class  PortClass
	Reason string
	Line   int
}

// PortFinding is a port that disagrees with the registry.
type PortFinding struct {
	Port int
	Why  string
}

// ParsePorts reads the declared registry.
//
// A malformed line is an ERROR, not skipped: in a security registry, the line
// the parser skips in silence is exactly the port nobody reviewed.
func ParsePorts(path string) (map[int]PortDecl, error) {
	f, err := os.Open(path) // #nosec G304 -- operator-supplied path, not network input
	if err != nil {
		return nil, fmt.Errorf("read port registry %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	decls := map[int]PortDecl{}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "|", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: malformed line, want `port | class | reason`: %q",
				path, n, line)
		}
		port, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: invalid port %q", path, n, strings.TrimSpace(fields[0]))
		}
		class := PortClass(strings.TrimSpace(fields[1]))
		switch class {
		case ClassExternal, ClassEdge, ClassHost, ClassAdmin, ClassPending:
		default:
			return nil, fmt.Errorf("%s:%d: unknown class %q (use external|edge|host|admin|pending)",
				path, n, class)
		}
		reason := strings.TrimSpace(fields[2])
		if reason == "" {
			return nil, fmt.Errorf("%s:%d: port %d has no declared reason — who consumes it, and how it was measured",
				path, n, port)
		}
		if previous, dup := decls[port]; dup {
			return nil, fmt.Errorf("%s:%d: port %d declared twice (first on line %d)",
				path, n, port, previous.Line)
		}
		decls[port] = PortDecl{Port: port, Class: class, Reason: reason, Line: n}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return decls, nil
}

// AuditPorts compares what listens with what was declared.
//
// It returns two lists, and the distinction matters to the reader:
//   - undeclared: listening and not in the registry. The failure this check
//     exists to prevent — a new reachable port nobody decided on.
//   - stale: declared and no longer listening. Not a risk, but a registry that
//     accumulates dead entries stops being reviewable.
func AuditPorts(listening []int, decls map[int]PortDecl) (undeclared, stale []PortFinding) {
	live := map[int]bool{}
	for _, p := range listening {
		live[p] = true
		if _, ok := decls[p]; !ok {
			undeclared = append(undeclared, PortFinding{
				Port: p,
				Why: "listens on 0.0.0.0 and is not declared — the registry is closed by default, " +
					"so a port outside it is a decision nobody made",
			})
		}
	}
	for p, d := range decls {
		if !live[p] {
			stale = append(stale, PortFinding{
				Port: p,
				Why:  fmt.Sprintf("declared as %s and no longer listens — a dead entry makes the registry less reviewable", d.Class),
			})
		}
	}
	sort.Slice(undeclared, func(i, j int) bool { return undeclared[i].Port < undeclared[j].Port })
	sort.Slice(stale, func(i, j int) bool { return stale[i].Port < stale[j].Port })
	return undeclared, stale
}

// PortsToClose returns the listening ports whose class does NOT admit external
// reach.
//
// `pending` is left out on purpose: closing without measuring the consumer is
// how two CI ports were taken down on 2026-07-30, breaking the gates that
// detect deploys. An explicit pending entry is honest; a blind close trades
// exposure for blindness.
func PortsToClose(listening []int, decls map[int]PortDecl) []PortDecl {
	var toClose []PortDecl
	for _, p := range listening {
		d, ok := decls[p]
		if !ok {
			continue // undeclared — reported by AuditPorts, not closed blindly
		}
		if d.Class == ClassExternal || d.Class == ClassPending {
			continue
		}
		toClose = append(toClose, d)
	}
	sort.Slice(toClose, func(i, j int) bool { return toClose[i].Port < toClose[j].Port })
	return toClose
}
