// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/deadlineratchet"
)

func init() {
	register(Gate{
		Name:    "cosign-cutover",
		Summary: "Enforces the cosign keyed→keyless cutover deadline ratchet.",
		// Each MUST name the external state (wiringReason.validate). Measured
		// 2026-08-11 by reading each gate's Run func, not by trusting the label —
		// which is how four of the nine below turned out to be misfiled.
		Wiring: operatorRunOnAmbient(
			"the wall-clock date, compared against the GA marker .github/v1.0-ga-date.txt — a deadline ratchet has no verdict without knowing what day it is"),
		Setup: setupCosignCutover,
	})
}

var setupCosignCutover = ratchetSetup("cosign-cutover", deadlineratchet.Spec{
	MarkerRelPath:    ".github/v1.0-ga-date.txt",
	ExceptionRelPath: ".github/cosign-cutover-exception.txt",
	WarnDay:          75,
	DeadlineDay:      90,
}, ratchetLabels{
	dormant:   "pre-GA — marker absent. Check is dormant.",
	subject:   "cosign keyless cutover",
	exception: ".github/cosign-cutover-exception.txt",
	adr:       "2026-05-12-cosign-keyless-v11-cutover.md",
})
