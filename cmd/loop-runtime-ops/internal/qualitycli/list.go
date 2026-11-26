// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/cliout"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/exitcode"
)

// taskWiredWord captures the gate name in a `loop-runtime-ops quality <gate>` invocation.
// The gate name must start alphanumeric ([a-z0-9]) so the tool's own meta-flags
// (`--list`, `--names`, `--json`, `--help`) are NOT counted as gates. Applied
// ONLY to command strings extracted from `cmds:` blocks (see collectCmdStrings),
// so prose, `desc:` block scalars, and comments cannot leak a false gate.
// Compiled once at package scope.
var taskWiredWord = regexp.MustCompile(`loop-runtime-ops quality ([a-z0-9][a-z0-9-]*)`)

// taskGateTasks returns, per gate name, the SORTED set of top-level task names
// in the given taskfile whose `cmds:` invoke that gate. Detection is STRUCTURAL:
// the taskfile is YAML-parsed and only command strings under `cmds:` sequences
// are scanned — a `desc:` mention or a `#` comment can never be mistaken for an
// invocation. A read or parse failure returns a typed error naming the path —
// never a silent empty map (which would mislabel every gate un-wired).
//
// Per-task attribution (MEET-2026-07-02-001 MA-1; hardened per the
// verification-architect sign-off, MEET-2026-07-02-002-adjacent) replaces the
// former boolean whole-file UNION. The union masked a silent un-wire: a gate
// dropped from `quality:gates` but still referenced by `quality:diff` (or a
// stray `echo`) kept `wired=true`, so the baseline stayed byte-identical and the
// un-wire went undetected. Attributing each gate to the EXACT task set makes any
// drop / move (gates→diff) / removal change the baseline bytes, and lets the
// positive-control test assert the oracle fires (which the boolean union could
// not express). It is also honest: it shows whether a gate is enforced by the
// full-scan hard set (`quality:gates`), the changed-files set (`quality:diff`),
// both, or neither.
//
// Known limitation (no occurrences in the live taskfile): a gate name embedded
// inside a shell string within a cmd (e.g. `echo "loop-runtime-ops quality X"`) attributes
// X to that task, and a gate invoked only via `preconditions:` (not `cmds:`) is
// not counted. Both are acceptable for the repo's convention (all real
// invocations are direct `- <path>loop-runtime-ops quality <gate>` cmds items).
func taskGateTasks(taskfilePath string) (map[string][]string, error) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		return nil, fmt.Errorf("read taskfile %s: %w", taskfilePath, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse taskfile %s: %w", taskfilePath, err)
	}
	acc := map[string]map[string]bool{} // gate -> set of task names
	for taskName, cmds := range collectTaskCmds(&doc) {
		for _, cmd := range cmds {
			for _, m := range taskWiredWord.FindAllStringSubmatch(cmd, -1) {
				if acc[m[1]] == nil {
					acc[m[1]] = map[string]bool{}
				}
				acc[m[1]][taskName] = true
			}
		}
	}
	out := make(map[string][]string, len(acc))
	for gate, tasks := range acc {
		list := make([]string, 0, len(tasks))
		for name := range tasks {
			list = append(list, name)
		}
		sort.Strings(list)
		out[gate] = list
	}
	return out, nil
}

// collectTaskCmds maps each top-level task name (under `tasks:`) to the command
// strings reachable under ITS OWN `cmds:` sequence. Task definitions do not nest
// task definitions, so a task body's cmds are unambiguously that task's. This is
// the per-task counterpart to the whole-file collectCmdStrings, and is what makes
// the wired attribution task-scoped instead of a file-wide union.
func collectTaskCmds(n *yaml.Node) map[string][]string {
	out := map[string][]string{}
	root := n
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "tasks" {
			continue
		}
		tasksMap := root.Content[i+1]
		if tasksMap.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(tasksMap.Content); j += 2 {
			taskName := tasksMap.Content[j].Value
			// collectCmdStrings on a single task body returns exactly that
			// task's cmds (a task body carries at most one `cmds:`).
			out[taskName] = collectCmdStrings(tasksMap.Content[j+1])
		}
	}
	return out
}

// collectCmdStrings walks a parsed taskfile and returns every command string
// reachable under a `cmds:` sequence. A cmds item is either a scalar (the
// command) or a mapping with a `cmd:` key (Task's map form); `task:`/`defer:`
// items carry no shell command and are skipped. Nodes outside `cmds:` (desc,
// vars, comments) are never scanned.
func collectCmdStrings(n *yaml.Node) []string {
	var out []string
	var walk func(node *yaml.Node)
	walk = func(node *yaml.Node) {
		if node == nil {
			return
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, val := node.Content[i], node.Content[i+1]
				if key.Value == "cmds" && val.Kind == yaml.SequenceNode {
					for _, item := range val.Content {
						switch item.Kind {
						case yaml.ScalarNode:
							out = append(out, item.Value)
						case yaml.MappingNode:
							for j := 0; j+1 < len(item.Content); j += 2 {
								if item.Content[j].Value == "cmd" {
									out = append(out, item.Content[j+1].Value)
								}
							}
						}
					}
				}
				walk(val)
			}
			return
		}
		for _, c := range node.Content {
			walk(c)
		}
	}
	walk(n)
	return out
}

// runList renders the gate inventory to w. Modes:
//
//	default  -> NAME\tTASKS\tSUMMARY  (one gate per line; TASKS = comma-joined
//	            sorted task names invoking the gate, or "-" if none)
//	--names  -> NAME                  (names only; feeds the per-gate liveness loop)
//	--json   -> [{"name","tasks":[...],"summary"}, ...]
//
// TASKS replaces the former boolean WIRED bit (per-task attribution — see
// taskGateTasks). Output is always sorted by gate name and deterministic, so it
// can be captured as a byte-stable baseline (the MA-1 oracle). Any drop / move /
// removal of a gate invocation changes a TASKS cell, turning the baseline diff
// (and CI) red.
func runList(w, stderr *cliout.Writer, args []string) int {
	fs := flag.NewFlagSet("quality --list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	namesOnly := fs.Bool("names", false, "print gate names only, one per line")
	asJSON := fs.Bool("json", false, "print the gate inventory as JSON")
	taskfile := fs.String("taskfile", "taskfiles/quality.yml", "taskfile scanned for per-gate task attribution")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}

	gateTasks, err := taskGateTasks(*taskfile)
	if err != nil {
		stderr.Printf("quality --list: %v\n", err)
		return exitcode.Incomplete
	}

	specs := Gates()

	switch {
	case *asJSON:
		type row struct {
			Name    string   `json:"name"`
			Tasks   []string `json:"tasks"`
			Summary string   `json:"summary"`
		}
		rows := make([]row, 0, len(specs))
		for _, g := range specs {
			tasks := gateTasks[g.Name]
			if tasks == nil {
				tasks = []string{} // render [] not null
			}
			rows = append(rows, row{g.Name, tasks, g.Summary})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			stderr.Printf("quality --list: encode json: %v\n", err)
			return exitcode.Incomplete
		}
	case *namesOnly:
		for _, g := range specs {
			w.Println(g.Name)
		}
	default:
		for _, g := range specs {
			tasksCol := "-"
			if ts := gateTasks[g.Name]; len(ts) > 0 {
				tasksCol = strings.Join(ts, ",")
			}
			w.Printf("%s\t%s\t%s\n", g.Name, tasksCol, g.Summary)
		}
	}
	return exitcode.OK
}
