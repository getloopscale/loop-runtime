// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"os"
)

// statPath is a stdlib shim so tests can substitute filesystem lookups.
var statPath = os.Stat
