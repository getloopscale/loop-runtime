// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package baseimagedigest implements the T4.6 gate: production Dockerfile base
// images must be digest-pinned (FROM image:tag@sha256:<64-hex>), not tag-only,
// to preserve SOURCE_DATE_EPOCH build reproducibility.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check_base_image_digest_pin.sh. Pure-Go (file read +
// regexp) — replaces bash grep/sed. No external CLI. (The "cosign"/"k3d" in the
// default lists are a Dockerfile path and a registry pattern, not invocations.)
package baseimagedigest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// DefaultDockerfiles are the production Dockerfiles linted (test fixtures excluded).
//
// cloud/Dockerfile and cli/Dockerfile left with the M4/M2 extractions to
// loopzero-cloud and loopzero-cli; infra/images/packs-runner/Dockerfile was deleted as
// a dead image ref (896505fc). They are dropped rather than kept as absent
// paths: a path that no longer exists is scanned to a vacuous pass, which reads
// as coverage while asserting nothing (issue #255).
var DefaultDockerfiles = []string{
	"api/Dockerfile",
	"operators/Dockerfile.app-operator",
	"operators/Dockerfile.tenant-operator",
	"operators/Dockerfile.preview-operator",
	"operators/Dockerfile.platform-operator",
	"infra/images/clone-runner/Dockerfile",
	"infra/images/syft-scanner/Dockerfile",
	"infra/images/cosign-signer/Dockerfile",
}

// DefaultExceptionsBaseline is EMPTY, and that is the point.
//
// It used to grandfather every pre-T4.6 tag-only FROM, on the premise that
// "Renovate digest-pins them on first bump". The bump came (golang 1.26.0 ->
// 1.26.1) and the pin did not: the baseline entries stopped matching, the gate
// went red, and it was quarantined as a failing gate instead of being fixed.
//
// Every production FROM is now pinned to its multi-arch INDEX digest (not a
// per-platform manifest digest, which would break arm64 builds), verified with
// `docker buildx imagetools inspect` and by a real build on 2026-08-06. With
// the list empty, un-pinning any base image fails the gate immediately rather
// than falling through to a grandfather clause. Re-adding an entry needs a
// reason in review — see TestExceptionsBaselineStaysEmpty.
var DefaultExceptionsBaseline = []string{}

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
	FilesRead  int      // configured Dockerfiles actually opened
	Violations []string // "<dockerfile>: <cleaned FROM line>"
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

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
			continue // an individually missing Dockerfile is tolerated; all of them are not — see below
		}
		out.FilesRead++
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
	// Fail closed when nothing was read. Tolerating one absent Dockerfile is
	// reasonable; tolerating ALL of them turns a wrong working directory into
	// "PASS — 0 FROM directive(s)", a green verdict that asserted nothing. That
	// is the shape issue #255 exists to remove, so it is a failure, not a pass.
	if out.FilesRead == 0 {
		out.Violations = append(out.Violations,
			"none of the configured Dockerfiles could be read — wrong working directory, or DefaultDockerfiles is stale")
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
