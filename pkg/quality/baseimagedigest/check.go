// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package baseimagedigest implements the T4.6 gate: production Dockerfile base
// images must be digest-pinned (FROM image:tag@sha256:<64-hex>), not tag-only,
// to preserve SOURCE_DATE_EPOCH build reproducibility.
//
// Plan T5.3 of docs/plans/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/check_base_image_digest_pin.sh. Pure-Go (file read +
// regexp) — replaces bash grep/sed. No external CLI. (The "cosign"/"k3d" in the
// default lists are a Dockerfile path and a registry pattern, not invocations.)
package baseimagedigest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultDockerfiles are the production Dockerfiles linted (test fixtures excluded).
var DefaultDockerfiles = []string{
	"api/Dockerfile",
	"cloud/Dockerfile",
	"operators/Dockerfile.app-operator",
	"operators/Dockerfile.tenant-operator",
	"operators/Dockerfile.preview-operator",
	"operators/Dockerfile.platform-operator",
	"tools/loopzero-dockerfile-lint/Dockerfile",
	"infra/images/packs-runner/Dockerfile",
	"infra/images/clone-runner/Dockerfile",
	"infra/images/syft-scanner/Dockerfile",
	"infra/images/cosign-signer/Dockerfile",
	"cli/Dockerfile",
}

// DefaultExceptionsBaseline grandfathers pre-T4.6 tag-only FROM lines (Renovate
// digest-pins them on first bump). Format: "<dockerfile>:<cleaned FROM line>".
var DefaultExceptionsBaseline = []string{
	"api/Dockerfile:FROM golang:1.25.9-alpine3.22 AS builder",
	"api/Dockerfile:FROM golang:1.25.9-alpine3.22 AS migrate-builder",
	"api/Dockerfile:FROM alpine:3.22",
	"cloud/Dockerfile:FROM golang:1.25.9-alpine3.22 AS builder",
	"cloud/Dockerfile:FROM alpine:3.22",
	"operators/Dockerfile.app-operator:FROM golang:1.25.9-alpine3.22 AS builder",
	"operators/Dockerfile.app-operator:FROM gcr.io/distroless/static-debian12:nonroot",
	"operators/Dockerfile.tenant-operator:FROM golang:1.25.9-alpine3.22 AS builder",
	"operators/Dockerfile.tenant-operator:FROM gcr.io/distroless/static-debian12:nonroot",
	"operators/Dockerfile.preview-operator:FROM golang:1.25.9-alpine3.22 AS builder",
	"operators/Dockerfile.preview-operator:FROM gcr.io/distroless/static-debian12:nonroot",
	"operators/Dockerfile.platform-operator:FROM golang:1.25.9-alpine3.22 AS builder",
	"operators/Dockerfile.platform-operator:FROM gcr.io/distroless/static-debian12:nonroot",
	"tools/loopzero-dockerfile-lint/Dockerfile:FROM golang:1.25.9-alpine3.22 AS build",
	"tools/loopzero-dockerfile-lint/Dockerfile:FROM alpine:3.22",
	"infra/images/packs-runner/Dockerfile:FROM mirror.gcr.io/library/golang:1.25.9-alpine3.22 AS build",
	"infra/images/packs-runner/Dockerfile:FROM mirror.gcr.io/library/alpine:3.21.6",
	"infra/images/syft-scanner/Dockerfile:FROM mirror.gcr.io/library/alpine:3.21.3",
	"infra/images/cosign-signer/Dockerfile:FROM mirror.gcr.io/library/alpine:3.21.3",
	"infra/images/clone-runner/Dockerfile:FROM mirror.gcr.io/library/alpine:3.21.6",
}

const pragmaLookback = 2 // lines before a FROM scanned for the pragma

var (
	digestRe   = regexp.MustCompile(`@sha256:[a-f0-9]{64}`)
	k3dRegRe   = regexp.MustCompile(`k3d-loopzero-registry:`)
	trailWSRe  = regexp.MustCompile(`[[:space:]]*$`)
	commentRe  = regexp.MustCompile(`#.*$`)
	pragmaText = "pragma: allow-tag-pin"
)

// Config drives the gate.
type Config struct {
	Root               string
	Dockerfiles        []string // default DefaultDockerfiles
	ExceptionsBaseline []string // default DefaultExceptionsBaseline
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Scanned    int      // total FROM directives scanned
	Violations []string // "<dockerfile>: <cleaned FROM line>"
}

// Check lints the configured Dockerfiles for tag-only base images.
func Check(cfg Config) *Outcome {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	dockerfiles := cfg.Dockerfiles
	if dockerfiles == nil {
		dockerfiles = DefaultDockerfiles
	}
	baseline := cfg.ExceptionsBaseline
	if baseline == nil {
		baseline = DefaultExceptionsBaseline
	}
	excepted := make(map[string]struct{}, len(baseline))
	for _, e := range baseline {
		excepted[e] = struct{}{}
	}

	out := &Outcome{Pass: true}
	for _, df := range dockerfiles {
		data, err := os.ReadFile(filepath.Join(root, df))
		if err != nil {
			continue // missing Dockerfile tolerated (bash: [ -e ] || continue)
		}
		lines := strings.Split(string(data), "\n")
		for i, raw := range lines {
			if !strings.HasPrefix(raw, "FROM ") {
				continue
			}
			clean := strings.TrimRight(commentRe.ReplaceAllString(raw, ""), " \t")
			clean = trailWSRe.ReplaceAllString(clean, "")
			if clean == "" {
				continue
			}
			out.Scanned++
			if digestRe.MatchString(clean) || k3dRegRe.MatchString(clean) || clean == "FROM scratch" {
				continue
			}
			if pragmaAbove(lines, i) {
				continue
			}
			if _, ok := excepted[df+":"+clean]; ok {
				continue
			}
			out.Violations = append(out.Violations, df+": "+clean)
		}
	}
	if len(out.Violations) > 0 {
		out.Pass = false
	}
	return out
}

// pragmaAbove reports whether `pragma: allow-tag-pin` appears within the
// pragmaLookback lines up to and including line index i.
func pragmaAbove(lines []string, i int) bool {
	start := i - pragmaLookback
	if start < 0 {
		start = 0
	}
	for j := start; j <= i; j++ {
		if strings.Contains(lines[j], pragmaText) {
			return true
		}
	}
	return false
}
