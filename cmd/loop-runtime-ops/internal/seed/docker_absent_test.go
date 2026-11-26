// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import "testing"

// A missing container is the one docker failure a seed may skip (the optional
// secrets cluster, #398). Docker changed the wording's case: older releases print
// "Error: No such object: X", current ones print "error: no such object: X".
// Matching one spelling turned an absent optional container into a fatal error.
func TestInspectOfAMissingContainerIsRecognisedInEitherSpelling(t *testing.T) {
	for _, stderr := range []string{
		"Error: No such object: k3d-loop-runtime-secrets-server-0\n",
		"error: no such object: k3d-loop-runtime-secrets-server-0\n",
	} {
		if !inspectSaysContainerIsAbsent(stderr) {
			t.Errorf("%q not recognised as a missing container", stderr)
		}
	}
}

func TestAnyOtherInspectFailureIsNotTreatedAsAbsence(t *testing.T) {
	for _, stderr := range []string{
		"Cannot connect to the Docker daemon at unix:///var/run/docker.sock\n",
		"permission denied while trying to connect to the Docker daemon socket\n",
		"",
	} {
		if inspectSaysContainerIsAbsent(stderr) {
			t.Errorf("%q treated as a missing container; a daemon failure must stay fatal", stderr)
		}
	}
}
