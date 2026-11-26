// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/deadlineratchet"
)

func init() {
	register(Gate{
		Name:    "service-auth-rotation",
		Summary: "Enforces the Ed25519 service-auth key rotation cadence.",
		Wiring: operatorRunOnAmbient(
			"the wall-clock date, compared against .github/service-auth-ed25519-rotation-date.txt — same ratchet shape as cosign-cutover; it reports on a deadline, it does not rotate anything"),
		Setup: setupServiceAuthRotation,
	})
}

var setupServiceAuthRotation = ratchetSetup("service-auth-rotation", deadlineratchet.Spec{
	MarkerRelPath:    ".github/service-auth-ed25519-rotation-date.txt",
	ExceptionRelPath: ".github/service-auth-rotation-exception.txt",
	WarnDay:          60,
	DeadlineDay:      90,
}, ratchetLabels{
	dormant:   "no rotation marker — check is dormant. First rotation should create it.",
	subject:   "service-auth Ed25519 rotation",
	exception: ".github/service-auth-rotation-exception.txt",
	adr:       "2026-05-13-service-auth-ed25519-adoption.md",
})
