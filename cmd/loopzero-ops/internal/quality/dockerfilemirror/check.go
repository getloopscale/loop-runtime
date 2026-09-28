// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package dockerfilemirror is the Go home for the dockerfile-no-docker-hub gate
// (migrated from infra/tests/scripts/dockerfile-no-docker-hub_test.sh, plan
// 2026-05-23).
//
// B8 regression: Dockerfiles under infra/images/ MUST NOT reference plain
// `alpine:`/`golang:`/`node:` (which resolve to docker.io and hit Docker Hub's
// anonymous rate limit on the build host). They MUST use
// mirror.gcr.io/library/<image>, a DOCR ref, or a full-path private registry.
package dockerfilemirror

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// Config tells Check where the per-image Dockerfiles live.
type Config struct {
	ImagesDir string // path to infra/images
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	OK         int
	Total      int
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check scans every infra/images/*/Dockerfile FROM directive and fails any that
// would resolve to Docker Hub (no mirror/DOCR/full-path prefix).
func Check(cfg Config) (*Outcome, error) {
	entries, err := os.ReadDir(cfg.ImagesDir)
	if err != nil {
		return nil, fmt.Errorf("read images dir %q: %w", cfg.ImagesDir, err)
	}
	var dockerfiles []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		df := filepath.Join(cfg.ImagesDir, e.Name(), "Dockerfile")
		if _, err := os.Stat(df); err == nil {
			dockerfiles = append(dockerfiles, df)
		}
	}
	sort.Strings(dockerfiles)

	out := &Outcome{Pass: true}
	for _, df := range dockerfiles {
		if err := scanDockerfile(df, out); err != nil {
			return nil, err
		}
	}
	if out.Total == 0 {
		return nil, fmt.Errorf("no FROM directives found under %s", cfg.ImagesDir)
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

func scanDockerfile(path string, out *Outcome) error {
	// #nosec G304 -- path is built from os.ReadDir of the configured images dir.
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "FROM ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := fields[1]
		out.Total++
		if isAllowedRef(ref) {
			out.OK++
		} else {
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s — must use mirror.gcr.io/library/* (not Docker Hub default)", strings.TrimSpace(line)))
		}
	}
	return scanner.Err()
}

// isAllowedRef mirrors the bash case: mirror.gcr.io/*, DOCR, or a full-path ref
// (>=2 slashes, i.e. registry/namespace/image) are allowed; bare docker.io
// defaults (alpine:, node:) are not.
func isAllowedRef(ref string) bool {
	switch {
	case strings.HasPrefix(ref, "mirror.gcr.io/"):
		return true
	case strings.HasPrefix(ref, "registry.digitalocean.com/"):
		return true
	case strings.Count(ref, "/") >= 2:
		return true
	default:
		return false
	}
}
