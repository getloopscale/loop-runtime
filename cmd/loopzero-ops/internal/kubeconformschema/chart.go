// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package kubeconformschema

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxCRDBytes bounds how much of one archive entry is read. The largest CRD a
// chart in this repository ships is well under 1 MiB; the bound only stops a
// corrupt or hostile archive from decompressing without limit.
const maxCRDBytes = 16 << 20

// GenerateFromChart returns the schemas of the CRDs that the dependencies of
// chartDir ship, at the versions its Chart.lock pins.
//
// Third-party CRDs are not in this repository: they come inside the upstream
// chart a wrapper under infra/helm depends on, and Helm installs them from that
// chart's crds/ directory. Reading them from the archive `helm dependency
// build` vendors for the pinned version makes the schemas a function of the
// lock, so refreshing them after an upgrade is the same command run again.
// Only each dependency's own crds/ directory is read; the CRDs of a chart it
// nests are that chart's business.
func GenerateFromChart(chartDir string) (_ []Schema, err error) {
	root, err := os.OpenRoot(chartDir)
	if err != nil {
		return nil, fmt.Errorf("chart directory: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()

	deps, err := lockedDependencies(root, chartDir)
	if err != nil {
		return nil, err
	}
	var docs []namedDocument
	for _, dep := range deps {
		archive := path.Join("charts", fmt.Sprintf("%s-%s.tgz", dep.Name, dep.Version))
		found, err := crdsInArchive(root, archive, dep.Name)
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s is not built: run `helm dependency build %s`",
				filepath.Join(chartDir, archive), chartDir)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(chartDir, archive), err)
		}
		docs = append(docs, found...)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no dependency of %s ships a CRD in its crds/ directory", chartDir)
	}
	return fromDocuments(docs)
}

type lockedDependency struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

func lockedDependencies(root *os.Root, chartDir string) ([]lockedDependency, error) {
	body, err := root.ReadFile("Chart.lock")
	if err != nil {
		return nil, fmt.Errorf("%s pins no dependency version: %w", chartDir, err)
	}
	var lock struct {
		Dependencies []lockedDependency `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(body, &lock); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Join(chartDir, "Chart.lock"), err)
	}
	if len(lock.Dependencies) == 0 {
		return nil, fmt.Errorf("%s locks no dependency", filepath.Join(chartDir, "Chart.lock"))
	}
	for _, d := range lock.Dependencies {
		if d.Name == "" || d.Version == "" || strings.ContainsAny(d.Name+d.Version, `/\`) {
			return nil, fmt.Errorf("%s: dependency %q %q is not a name and a version", filepath.Join(chartDir, "Chart.lock"), d.Name, d.Version)
		}
	}
	return lock.Dependencies, nil
}

// crdsInArchive returns every <dep>/crds/*.yaml document of the archive, in
// archive order.
func crdsInArchive(root *os.Root, archive, dep string) (_ []namedDocument, err error) {
	f, err := root.Open(archive)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer func() { err = errors.Join(err, gz.Close()) }()
	tr := tar.NewReader(gz)
	crdDir := dep + "/crds"
	var out []namedDocument
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		name := path.Clean(hdr.Name)
		if hdr.Typeflag != tar.TypeReg || path.Dir(name) != crdDir || !isYAML(name) {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxCRDBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if len(data) > maxCRDBytes {
			return nil, fmt.Errorf("%s exceeds %d bytes", name, maxCRDBytes)
		}
		out = append(out, namedDocument{source: archive + ":" + name, data: data})
	}
}

func isYAML(name string) bool {
	ext := path.Ext(name)
	return ext == ".yaml" || ext == ".yml"
}
