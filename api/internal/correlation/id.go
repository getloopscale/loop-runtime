// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package correlation

import (
	"github.com/google/uuid"
)

// GenerateID returns a new UUID v4 correlation ID.
func GenerateID() string {
	return uuid.New().String()
}
