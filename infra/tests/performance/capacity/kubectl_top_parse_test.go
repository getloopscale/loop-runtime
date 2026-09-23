// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package capacity

import "testing"

// parseKubectlTop is the one `kubectl top` parser in the package; pod_memory
// used to carry a second one. This pins the units both relied on.
func TestParseKubectlTopReadsEveryUnitKubectlPrints(t *testing.T) {
	out := "api-7f8b4c9d-x2k4j   1m    23Mi\n" +
		"worker-5d9c         2     1Gi\n" +
		"sidecar-1           250m  2048Ki\n"

	pods, err := parseKubectlTop(out)

	if err != nil {
		t.Fatalf("parseKubectlTop: %v", err)
	}
	want := []PodResourceUsage{
		{Name: "api-7f8b4c9d-x2k4j", CPUMillis: 1, MemoryMi: 23},
		{Name: "worker-5d9c", CPUMillis: 2000, MemoryMi: 1024},
		{Name: "sidecar-1", CPUMillis: 250, MemoryMi: 2},
	}
	if len(pods) != len(want) {
		t.Fatalf("pods = %+v, want %+v", pods, want)
	}
	for i := range want {
		if pods[i] != want[i] {
			t.Errorf("pod[%d] = %+v, want %+v", i, pods[i], want[i])
		}
	}
}
