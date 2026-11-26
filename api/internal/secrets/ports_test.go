// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets_test

import (
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/secrets"
)

// PostgresSecretStore still satisfies the SecretStore port after the T2.2 #4
// migration. Pure compile-time assertion — no DBTX dependency. It sits on the
// consumer's side: the store does not import the feature.
var _ secrets.SecretStore = (*store.PostgresSecretStore)(nil)
