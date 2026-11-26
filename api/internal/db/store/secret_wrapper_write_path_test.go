// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// secret_wrapper_write_path_test.go — T10.3 of plan
// 2026-06-02-openbao-adoption v1.3; closes B-162 DoD bullet 2
// ("a test that proves the write path produces ONLY the Transit prefix").
//
// Before T10.3 the write dispatcher encryptForStore carried an AES-256-GCM
// fallback that fired whenever the store was built without a Transit client.
// config.Validate still accepts crypto_mode=aes-local and crypto_mode=plaintext,
// so a boot that missed OPENBAO_TRANSIT_ENABLED wrote tenant secrets under a
// local AES key — or, with no key at all, in the clear — and nothing
// distinguished those rows from Transit rows except a missing "vault:v1:"
// prefix. The write path now has exactly two outcomes: Transit ciphertext, or
// a refusal.
//
// The READ path is deliberately not covered here. decryptFromStore keeps its
// AES branch so rows written before this change stay readable — B-162 DoD
// bullet 3. See docs/wiki/decisions/2026-09-04-t103-aes-write-path-removal.md.

import (
	"context"
	"errors"
	"strings"
	"testing"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// aes256TestKey returns a deterministic 32-byte key. Only the legacy read path
// still consumes one; the write path must refuse to use it.
func aes256TestKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestEncryptForStore_ProducesOnlyTransitCiphertextOrRefuses(t *testing.T) {
	cases := []struct {
		name    string
		store   func(t *testing.T) *PostgresSecretStore
		input   []byte
		refuses bool
	}{
		{
			name: "transit wired encrypts through transit",
			store: func(t *testing.T) *PostgresSecretStore {
				s, err := NewPostgresSecretStoreWithVault(nil, &mockVault{}, aes256TestKey())
				if err != nil {
					t.Fatal(err)
				}
				return s
			},
			input: []byte("plain"),
		},
		{
			name: "transit wired passes an already-Transit payload through",
			store: func(t *testing.T) *PostgresSecretStore {
				s, err := NewPostgresSecretStoreWithVault(nil, &mockVault{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				return s
			},
			input: []byte("vault:v1:already-encrypted-by-the-handler"),
		},
		{
			name: "crypto_mode=aes-local refuses instead of writing AES ciphertext",
			store: func(t *testing.T) *PostgresSecretStore {
				s, err := NewPostgresSecretStore(nil, aes256TestKey())
				if err != nil {
					t.Fatal(err)
				}
				return s
			},
			input:   []byte("plain"),
			refuses: true,
		},
		{
			name: "crypto_mode=plaintext refuses instead of writing the value in the clear",
			store: func(t *testing.T) *PostgresSecretStore {
				s, err := NewPostgresSecretStore(nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				return s
			},
			input:   []byte("plain"),
			refuses: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.store(t)
			got, err := s.encryptForStore(context.Background(), "tenant-A", tc.input)

			if tc.refuses {
				if err == nil {
					t.Fatalf("write path accepted a non-Transit configuration and returned %q; T10.3 requires a refusal", got)
				}
				if got != nil {
					t.Errorf("a refused write must return no ciphertext, got %q", got)
				}
				if !errors.Is(err, loopzeroerrors.ErrSecretsBackendUnavailable) {
					t.Errorf("refusal must wrap ErrSecretsBackendUnavailable so the HTTP boundary answers 503, got %v", err)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(got), string(vaultPrefix)) {
				t.Errorf("write path produced %q; only %q-prefixed ciphertext may reach secrets.encrypted_value", got, vaultPrefix)
			}
		})
	}
}
