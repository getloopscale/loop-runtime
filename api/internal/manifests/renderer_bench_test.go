// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Plan v1.2 T4.3 (O2) — Concat strings.Builder benchmark.

package manifests

import (
	"strings"
	"testing"
)

func fixtureDocs(n int) []K8sDoc {
	docs := make([]K8sDoc, 0, n)
	body := strings.Repeat("a", 256) // ~typical YAML doc line length × 4
	for i := 0; i < n; i++ {
		docs = append(docs, K8sDoc{YAML: body})
	}
	return docs
}

// Baseline reference — emulate the pre-T4.3 implementation (strings.Join over
// trimmed bodies) so the benchmark suite can show the relative cost.
func referenceJoin(docs []K8sDoc) string {
	parts := make([]string, len(docs))
	for i := range docs {
		parts[i] = strings.TrimRight(docs[i].YAML, "\n")
	}
	return strings.Join(parts, "\n---\n")
}

func BenchmarkConcat_Builder_50Docs(b *testing.B) {
	docs := fixtureDocs(50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Concat(docs)
	}
}

func BenchmarkConcat_StringsJoinReference_50Docs(b *testing.B) {
	docs := fixtureDocs(50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = referenceJoin(docs)
	}
}
