// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package valuesduplicatekeys refuses a Helm values file that declares the same
// mapping key twice.
//
// WHY THIS NEEDS A GATE. Helm parses a values file with a YAML decoder that
// keeps the LAST occurrence of a duplicated key and says nothing. Measured with
// Helm v3.19.0: infra/helm/gitea/values-ha.yaml declared `gitea:` twice and
// infra/helm/tempo/values.yaml declared `tempo:` twice, so the whole first block
// of each — Gitea's global lock and logging, Tempo's receivers, retention and
// storage — was discarded on every render, while the file read as configured.
// `helm lint` passes such a file; so does kubeconform, because the rendered
// manifests are valid. Only a check on the source YAML sees it.
//
// SCOPE. Values files (`values*.yaml`) under the two chart trees, charts/ and
// infra/helm/. Templates are Go templates, not YAML, and are out of scope.
package valuesduplicatekeys

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
	"gopkg.in/yaml.v3"
)

// DefaultRoots are the chart trees whose values files are scanned.
var DefaultRoots = []string{"charts", filepath.Join("infra", "helm")}

// Config points the check at the repository.
type Config struct {
	RepoRoot string
	Roots    []string // default: DefaultRoots
}

// Finding is one mapping key declared more than once under the same parent.
type Finding struct {
	File      string // repo-relative, slash-separated
	Path      string // dotted path of the duplicated key, e.g. "gitea" or "image.tag"
	Key       string
	Line      int // line of the duplicate (the declaration Helm keeps)
	FirstLine int // line of the first declaration (the one Helm discards)
}

// Outcome is the verdict.
type Outcome struct {
	Pass     bool
	Skipped  bool
	Reason   string
	Scanned  int
	Findings []Finding
}

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

// Check scans every values file under the configured roots. A file that does
// not parse is returned as an error rather than reported clean: Helm could not
// render it either, and "no duplicate found" would be a claim about a file
// nobody read.
func Check(cfg Config) (*Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	roots := cfg.Roots
	if len(roots) == 0 {
		roots = DefaultRoots
	}

	files, err := valuesFiles(cfg.RepoRoot, roots)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return &Outcome{
			Skipped: true,
			Reason:  fmt.Sprintf("no values*.yaml under %s — duplicate keys NOT verified", strings.Join(roots, ", ")),
		}, nil
	}

	repo := os.DirFS(cfg.RepoRoot)
	out := &Outcome{Pass: true, Scanned: len(files)}
	for _, rel := range files {
		body, readErr := fs.ReadFile(repo, rel)
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", rel, readErr)
		}
		var doc yaml.Node
		if parseErr := yaml.Unmarshal(body, &doc); parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, parseErr)
		}
		for _, f := range duplicatesIn(&doc, "") {
			f.File = rel
			out.Findings = append(out.Findings, f)
		}
	}
	if len(out.Findings) > 0 {
		out.Pass = false
	}
	out.Reason = fmt.Sprintf("%d values file(s) scanned", out.Scanned)
	return out, nil
}

// valuesFiles returns the repo-relative, sorted values files under roots.
// A root that does not exist is skipped: a checkout without charts/ has no
// values there to verify, and the empty-scan case above reports it.
func valuesFiles(repoRoot string, roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		base := filepath.Join(repoRoot, root)
		if _, statErr := os.Stat(base); os.IsNotExist(statErr) {
			continue
		}
		walkErr := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// templates/ holds Go templates; a nested charts/ holds vendored
				// subcharts whose values belong to their upstream.
				if d.Name() == "templates" || (d.Name() == "charts" && p != base) {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if !strings.HasPrefix(name, "values") || !strings.HasSuffix(name, ".yaml") {
				return nil
			}
			rel, relErr := filepath.Rel(repoRoot, p)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("walk %s: %w", root, walkErr)
		}
	}
	sort.Strings(files)
	return files, nil
}

// duplicatesIn walks a YAML node tree and reports every key declared twice in
// the same mapping.
func duplicatesIn(n *yaml.Node, prefix string) []Finding {
	var out []Finding
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			out = append(out, duplicatesIn(c, prefix)...)
		}
	case yaml.MappingNode:
		first := map[string]int{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			path := k.Value
			if prefix != "" {
				path = prefix + "." + k.Value
			}
			if line, seen := first[k.Value]; seen {
				out = append(out, Finding{Path: path, Key: k.Value, Line: k.Line, FirstLine: line})
			} else {
				first[k.Value] = k.Line
			}
			out = append(out, duplicatesIn(v, path)...)
		}
	}
	return out
}
