// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package kubeconformschema

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Default locations, relative to the repository root.
const (
	DefaultCRDDir    = "operators/config/crd/bases"
	DefaultSchemaDir = "infra/kubeconform-schemas/master-standalone-strict"
)

// DriftKind says how a committed schema differs from what the CRDs produce.
type DriftKind string

const (
	// Missing — a CRD version has no committed schema.
	Missing DriftKind = "missing"
	// Stale — the committed schema differs from the one the CRD produces.
	Stale DriftKind = "stale"
	// Orphan — a committed schema of a group the CRDs own matches no CRD version.
	Orphan DriftKind = "orphan"
)

// Drift is one schema file out of step with the CRDs.
type Drift struct {
	Kind DriftKind
	Name string
}

func (d Drift) String() string { return fmt.Sprintf("%s %s", d.Kind, d.Name) }

// Generate reads every *.yaml CRD in crdDir and returns their schemas, sorted
// by file name. A directory holding no CRD is an error: a comparison against
// nothing would pass every time.
func Generate(crdDir string) (_ []Schema, err error) {
	root, err := os.OpenRoot(crdDir)
	if err != nil {
		return nil, fmt.Errorf("CRD directory: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	names, err := fs.Glob(root.FS(), "*.yaml")
	if err != nil {
		return nil, fmt.Errorf("list CRDs in %s: %w", crdDir, err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no CRD (*.yaml) in %s", crdDir)
	}
	docs := make([]namedDocument, 0, len(names))
	for _, name := range names {
		data, err := root.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read CRD: %w", err)
		}
		docs = append(docs, namedDocument{source: filepath.Join(crdDir, name), data: data})
	}
	return fromDocuments(docs)
}

// namedDocument is one CRD document and where it was read from.
type namedDocument struct {
	source string
	data   []byte
}

// fromDocuments turns CRD documents into schemas sorted by file name, and
// refuses two documents that produce the same schema.
func fromDocuments(docs []namedDocument) ([]Schema, error) {
	var out []Schema
	seen := map[string]string{}
	for _, d := range docs {
		schemas, err := FromCRD(d.data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.source, err)
		}
		for _, s := range schemas {
			if prev, dup := seen[s.Name]; dup {
				return nil, fmt.Errorf("%s and %s both produce %s", prev, d.source, s.Name)
			}
			seen[s.Name] = d.source
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Compare reports how the schemas committed in schemaDir differ from want.
func Compare(want []Schema, schemaDir string) (_ []Drift, err error) {
	root, err := os.OpenRoot(schemaDir)
	if err != nil {
		return nil, fmt.Errorf("schema directory: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	return compare(want, root)
}

func compare(want []Schema, root *os.Root) ([]Drift, error) {
	committed, err := ownedFiles(root, groupsOf(want))
	if err != nil {
		return nil, err
	}
	var drift []Drift
	for _, s := range want {
		if !committed[s.Name] {
			drift = append(drift, Drift{Kind: Missing, Name: s.Name})
			continue
		}
		delete(committed, s.Name)
		body, err := root.ReadFile(s.Name)
		if err != nil {
			return nil, fmt.Errorf("read committed schema: %w", err)
		}
		if !bytes.Equal(body, s.Content) {
			drift = append(drift, Drift{Kind: Stale, Name: s.Name})
		}
	}
	for _, name := range sortedKeys(committed) {
		drift = append(drift, Drift{Kind: Orphan, Name: name})
	}
	return drift, nil
}

// WriteSchemas brings the schemas of want's groups in schemaDir in step with
// want: it writes every missing or stale one and removes every orphan of
// those groups. Files of any other group are never touched.
func WriteSchemas(want []Schema, schemaDir string) (_ []Drift, err error) {
	root, err := os.OpenRoot(schemaDir)
	if err != nil {
		return nil, fmt.Errorf("schema directory: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	drift, err := compare(want, root)
	if err != nil {
		return nil, err
	}
	byName := make(map[string][]byte, len(want))
	for _, s := range want {
		byName[s.Name] = s.Content
	}
	for _, d := range drift {
		if d.Kind == Orphan {
			if err := root.Remove(d.Name); err != nil {
				return nil, fmt.Errorf("remove orphan schema: %w", err)
			}
			continue
		}
		if err := root.WriteFile(d.Name, byName[d.Name], schemaFileMode); err != nil {
			return nil, fmt.Errorf("write schema: %w", err)
		}
	}
	return drift, nil
}

// schemaFileMode is the mode of a schema written for the first time: a
// committed, world-readable repository file, like every other one beside it.
const schemaFileMode fs.FileMode = 0o644

func groupsOf(schemas []Schema) map[string]bool {
	groups := map[string]bool{}
	for _, s := range schemas {
		groups[s.Group] = true
	}
	return groups
}

// ownedFiles returns the name of every *.json in root whose group segment is
// one of groups. The group sits between the first and the last dash of
// `<kind>-<group>-<version>.json`; a Kubernetes kind never contains a dash.
func ownedFiles(root *os.Root, groups map[string]bool) (map[string]bool, error) {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("list schema directory: %w", err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if group, ok := groupOf(name); ok && groups[group] {
			out[name] = true
		}
	}
	return out, nil
}

func groupOf(name string) (string, bool) {
	stem := strings.TrimSuffix(name, ".json")
	first, last := strings.Index(stem, "-"), strings.LastIndex(stem, "-")
	if first < 0 || first == last {
		return "", false
	}
	return stem[first+1 : last], true
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
