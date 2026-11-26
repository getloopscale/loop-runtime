// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// decrypt_failure_metric_test.go — B-126. Locks the aggregate signal on the
// secret READ path: every error return of decryptFromStore increments
// metrics.SecretDecryptFailedTotal under the reason that says which response
// the failure calls for, and a successful decrypt increments nothing.
//
// Why this test exists at all: before B-126 a revoked Transit key produced
// one per-request error per reader and nothing that added up. These tests are
// the proof that the failures now aggregate — they fail the moment somebody
// adds a return path to the dispatcher without a counter, which is exactly how
// the signal would silently go back to being per-request only.
//
// Pure unit tests, no PG connection: decryptFromStore operates on bytes plus
// the two wired backends, and never touches the DBTX field (same rationale as
// transit_dispatch_test.go, whose fakes this file reuses).

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// decryptFailureReasons is the closed enum the counter ships with. Kept here
// so the sum-over-all-reasons assertions below cannot silently miss a reason
// added later without updating this list.
var decryptFailureReasons = []string{"empty_ciphertext", "vault_transit_error", "aes_legacy_error"}

// decryptFailuresByReason reads the counter for one reason. Prometheus counters
// are process-global, so every assertion in this file is a DELTA across the
// call under test, never an absolute value — tests in this package must stay
// independent of execution order.
func decryptFailuresByReason(t *testing.T, reason string) float64 {
	t.Helper()
	return testutil.ToFloat64(metrics.SecretDecryptFailedTotal.WithLabelValues(reason))
}

// decryptFailuresTotal sums every reason. Used to prove a single failure is
// counted ONCE, not once per layer that re-wraps the error on its way up.
func decryptFailuresTotal(t *testing.T) float64 {
	t.Helper()
	var sum float64
	for _, reason := range decryptFailureReasons {
		sum += decryptFailuresByReason(t, reason)
	}
	return sum
}

// TestDecryptFromStore_VaultTransitFailureIsCounted — the reason B-126 exists.
// A revoked or rotated transit key makes Transit refuse the decrypt; that
// refusal must land on the counter, under the reason that says "key side".
func TestDecryptFromStore_VaultTransitFailureIsCounted(t *testing.T) {
	// Arrange: a wired vault that refuses this tenant, as a revoked key does.
	vault := &fakeTransitT22{failOnTenant: map[string]error{"tn-A": errors.New("vault: transit/decrypt returned 400: encryption key not found")}}
	store := newStoreWithFakes(t, vault)
	before := decryptFailuresByReason(t, "vault_transit_error")

	// Act
	_, err := store.decryptFromStore(context.Background(), "tn-A", []byte("vault:v1:abc"))

	// Assert
	if err == nil {
		t.Fatalf("expected the transit refusal to surface; got nil")
	}
	if delta := decryptFailuresByReason(t, "vault_transit_error") - before; delta != 1 {
		t.Errorf("vault_transit_error delta = %v, want 1", delta)
	}
}

// TestDecryptFromStore_LegacyAESFailureIsCounted — a pre-T10.3 row that will
// not open is a DIFFERENT operational answer (restore a backup, or the local
// AES key is gone) than a transit refusal, so it carries a different reason.
func TestDecryptFromStore_LegacyAESFailureIsCounted(t *testing.T) {
	// Arrange: bytes with no vault:v1: prefix route to the legacy AES branch,
	// which cannot open them.
	vault := &fakeTransitT22{}
	store := newStoreWithFakes(t, vault)
	bogusAES := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	before := decryptFailuresByReason(t, "aes_legacy_error")

	// Act
	_, err := store.decryptFromStore(context.Background(), "tn-A", bogusAES)

	// Assert
	if err == nil {
		t.Fatalf("expected the AES branch to fail on bogus bytes; got nil")
	}
	if delta := decryptFailuresByReason(t, "aes_legacy_error") - before; delta != 1 {
		t.Errorf("aes_legacy_error delta = %v, want 1", delta)
	}
	if len(vault.decryptCalls) != 0 {
		t.Errorf("the AES branch must not touch the vault; got %d calls", len(vault.decryptCalls))
	}
}

// TestDecryptFromStore_EmptyCiphertextIsCounted — the EC-06 guard fires before
// either backend. It means a row exists whose encrypted_value is empty, which
// no write path in the tree can produce, so it is counted apart.
func TestDecryptFromStore_EmptyCiphertextIsCounted(t *testing.T) {
	vault := &fakeTransitT22{}
	store := newStoreWithFakes(t, vault)
	before := decryptFailuresByReason(t, "empty_ciphertext")

	_, err := store.decryptFromStore(context.Background(), "tn-A", []byte{})

	if err == nil {
		t.Fatalf("expected the EC-06 empty-row guard to fail; got nil")
	}
	if delta := decryptFailuresByReason(t, "empty_ciphertext") - before; delta != 1 {
		t.Errorf("empty_ciphertext delta = %v, want 1", delta)
	}
}

// TestDecryptFromStore_SuccessCountsNothing — the counter measures failures.
// A success that bumped any series would make every rate query wrong and would
// put work on the hot path for nothing.
func TestDecryptFromStore_SuccessCountsNothing(t *testing.T) {
	vault := &fakeTransitT22{plaintext: []byte("postgres://x")}
	store := newStoreWithFakes(t, vault)
	before := decryptFailuresTotal(t)

	plain, err := store.decryptFromStore(context.Background(), "tn-A", []byte("vault:v1:abc123"))

	if err != nil {
		t.Fatalf("decryptFromStore: %v", err)
	}
	if string(plain) != "postgres://x" {
		t.Fatalf("plaintext: got %q want %q", string(plain), "postgres://x")
	}
	if delta := decryptFailuresTotal(t) - before; delta != 0 {
		t.Errorf("a successful decrypt moved the failure counter by %v, want 0", delta)
	}
}

// TestDecryptFromStore_OneFailureIsCountedOnce — the anti-double-count lock.
// decryptFromStore is the only place WITHIN PostgresSecretStore that
// increments; callers such as ListForResync re-wrap the error on the way up
// and must NOT increment again. (B-175: the broker reader in
// api/cmd/setup_secret_fetch.go also increments this counter, but it is a
// separate read path with its own rows, not a caller of this dispatcher, so it
// cannot double-count a failure this test observes.)
// Without this assertion, one orphaned ciphertext could show up as two events
// and every "how many secrets are broken" query would be inflated.
func TestDecryptFromStore_OneFailureIsCountedOnce(t *testing.T) {
	vault := &fakeTransitT22{failOnTenant: map[string]error{"tn-A": errors.New("vault: unreachable (fake)")}}
	store := newStoreWithFakes(t, vault)
	before := decryptFailuresTotal(t)

	if _, err := store.decryptFromStore(context.Background(), "tn-A", []byte("vault:v1:abc")); err == nil {
		t.Fatalf("expected an error; got nil")
	}

	if delta := decryptFailuresTotal(t) - before; delta != 1 {
		t.Errorf("one failure moved the counter by %v across all reasons, want exactly 1", delta)
	}
}
