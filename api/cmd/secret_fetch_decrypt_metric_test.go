// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// B-175 — the broker secret-fetch endpoint is the one External Secrets
// Operator calls for EVERY user secret, and until these tests existed it
// decrypted via Transit off its own SQL query without incrementing anything.
// `loopzero_secret_decrypt_failed_total` therefore read zero while the path that
// actually serves users failed once per request. That is worse than having no
// metric: an operator who reads zero concludes nothing broke.
//
// The tests lock the counter to this path under the SAME reason vocabulary
// PostgresSecretStore.decryptFromStore uses — empty_ciphertext,
// vault_transit_error, aes_legacy_error — so one PromQL query covers both
// readers instead of one query per reader nobody remembers to write.

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/routes/secrets"
)

// decryptFailuresFor reads the counter for one reason. Always used as a DELTA
// around the call under test: the counter is a package-level global and every
// other test in this binary shares it.
func decryptFailuresFor(reason string) float64 {
	return testutil.ToFloat64(metrics.SecretDecryptFailedTotal.WithLabelValues(reason))
}

// brokerRowReturning builds a fakeQuerier (declared in
// setup_secret_fetch_test.go) whose single row scans to the given bytes.
func brokerRowReturning(value []byte) *fakeQuerier {
	return &fakeQuerier{row: &fakeRow{scan: func(dest ...any) error {
		dst, ok := dest[0].(*[]byte)
		if !ok {
			return errors.New("fake row: unexpected scan destination")
		}
		*dst = value
		return nil
	}}}
}

// TestBrokerReadSecret_DecryptFailureMovesCounter is the B-175 regression.
// Each case is a decrypt failure on the ESO path; each must move the counter
// exactly once, under the reason that tells the operator which recovery the
// failure calls for (restore a key vs restore a backup).
func TestBrokerReadSecret_DecryptFailureMovesCounter(t *testing.T) {
	cases := []struct {
		name       string
		row        []byte
		transitErr error
		reason     string
	}{
		{
			name:       "transit refused — the revoked-key failure B-126 exists for",
			row:        []byte("vault:v1:abc"),
			transitErr: errors.New("permission denied by policy (fake)"),
			reason:     "vault_transit_error",
		},
		{
			name:   "row is not vault-encoded — a legacy AES row this path cannot open",
			row:    []byte("legacy-aes-bytes"),
			reason: "aes_legacy_error",
		},
		{
			name:   "row exists but its ciphertext is empty — the EC-06 guard",
			row:    []byte{},
			reason: "empty_ciphertext",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &rawSecretReader{
				pool:    brokerRowReturning(tc.row),
				transit: &fakeTransitForRead{err: tc.transitErr},
			}

			before := decryptFailuresFor(tc.reason)
			_, err := reader.ReadSecret(context.Background(), "tn-A", "proj-x", "production", "API_KEY")
			require.Error(t, err,
				"the read must still fail — the counter is an addition to the error, never a replacement for it")

			require.Equal(t, before+1, decryptFailuresFor(tc.reason),
				"the broker path must increment %q exactly once; ESO calls this endpoint for every user secret, so an uncounted failure here is the whole of B-175", tc.reason)
		})
	}
}

// TestBrokerReadSecret_SuccessDoesNotMoveCounter — a counter that also moves
// on success cannot be alerted on.
func TestBrokerReadSecret_SuccessDoesNotMoveCounter(t *testing.T) {
	reader := &rawSecretReader{
		pool:    brokerRowReturning([]byte("vault:v1:abc")),
		transit: &fakeTransitForRead{plaintext: []byte("plain")},
	}

	before := decryptFailuresTotalAllReasons()
	plain, err := reader.ReadSecret(context.Background(), "tn-A", "proj-x", "production", "API_KEY")
	require.NoError(t, err)
	require.Equal(t, []byte("plain"), plain)

	require.Equal(t, before, decryptFailuresTotalAllReasons(),
		"a successful decrypt must leave every reason untouched")
}

// TestBrokerReadSecret_MissingRowIsNotADecryptFailure — a row that is absent
// was never ciphertext. Counting a 404 here would make the alert fire on
// ordinary "the user has not created that secret yet" traffic, which is how a
// page gets muted and then ignored.
func TestBrokerReadSecret_MissingRowIsNotADecryptFailure(t *testing.T) {
	reader := &rawSecretReader{
		pool: &fakeQuerier{row: &fakeRow{scan: func(...any) error {
			return pgx.ErrNoRows
		}}},
		transit: &fakeTransitForRead{},
	}

	before := decryptFailuresTotalAllReasons()
	_, err := reader.ReadSecret(context.Background(), "tn-A", "proj-x", "production", "ABSENT")
	require.ErrorIs(t, err, secrets.ErrSecretNotFound)

	require.Equal(t, before, decryptFailuresTotalAllReasons(),
		"a missing row is not a decrypt failure and must not move the counter")
}

// decryptFailuresTotalAllReasons sums the three reasons the two read paths
// share. Summing is what catches a failure counted under a reason the test did
// not expect — a per-reason assertion alone would read zero and pass.
func decryptFailuresTotalAllReasons() float64 {
	total := 0.0
	for _, reason := range []string{"empty_ciphertext", "vault_transit_error", "aes_legacy_error"} {
		total += decryptFailuresFor(reason)
	}
	return total
}
