// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package helmrender is the one seam through which quality gates render Helm
// charts, and the one policy for a machine without helm.
//
// The policy: helm is a documented quality:gates prerequisite, and a gate
// whose subject is rendered output cannot vouch for anything without it. An
// absent binary is ErrHelmUnavailable — an error, never a pass. Before this
// package existed, five gates each ran `helm template` with their own argument
// shapes and three different answers to a missing helm (an advisory pass, a
// hard error, a violation).
package helmrender

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrHelmUnavailable means helm is not on PATH.
var ErrHelmUnavailable = errors.New("helm is not on PATH: a gate that renders charts cannot measure without it " +
	"(helm is a documented quality:gates prerequisite)")

// Renderer turns a chart directory into the manifest stream it installs, under
// the values `sets` (each a `--set` expression; nil renders at chart
// defaults). It is an interface so gates can be tested without helm.
type Renderer interface {
	Render(chartDir string, sets []string) (string, error)
}

// Helm renders with `helm template`. Release is the release name given to
// helm; empty lets helm choose its default. A fixed release name keeps two
// runs of the same chart byte-identical.
type Helm struct {
	Release string
}

// TemplateArgs builds the `helm template` invocation. It is separate from
// Render so the invocation can be asserted without helm installed.
func TemplateArgs(release, chartDir string, sets []string) []string {
	args := []string{"template"}
	if release != "" {
		args = append(args, release)
	}
	args = append(args, chartDir)
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	return args
}

// Render returns the manifests on stdout; stderr (warnings included) never
// mixes into them and is carried in the error instead.
func (h Helm) Render(chartDir string, sets []string) (string, error) {
	if _, err := exec.LookPath("helm"); err != nil {
		return "", ErrHelmUnavailable
	}
	args := TemplateArgs(h.Release, chartDir, sets)
	cmd := exec.Command("helm", args...) // #nosec G204 -- chart paths and --set values come from gate code, not user input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			msg = " — " + msg
		}
		// The --set list is named so the failing invocation can be reproduced
		// from the message alone.
		return "", fmt.Errorf("helm %s: %w%s", strings.Join(args, " "), err, msg)
	}
	return string(out), nil
}
