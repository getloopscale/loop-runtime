// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secretsync

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

// The secret-fetch broker answers {"plaintext_b64": base64(value)}, and External Secrets'
// webhook provider writes what the result jsonPath selects as is. Without the Base64
// decoding strategy the app received the base64 text instead of its secret.
func TestExternalSecretDecodesTheBrokersBase64Answer(t *testing.T) {
	ref := ports.SecretRef{TenantID: "tn-a", Project: "shop", Environment: "production", App: "api", Name: "DB_URL"}

	es := externalSecretObject(ref, "loopzero-tn-a-shop-production")

	data, _, _ := unstructured.NestedSlice(es.Object, "spec", "data")
	if len(data) != 1 {
		t.Fatalf("spec.data has %d entries, want 1", len(data))
	}
	entry, _ := data[0].(map[string]interface{})
	got, _, _ := unstructured.NestedString(entry, "remoteRef", "decodingStrategy")
	if got != "Base64" {
		t.Errorf("remoteRef.decodingStrategy = %q, want Base64: the broker's value is base64-encoded", got)
	}
}
