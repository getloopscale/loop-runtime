// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package operatorboot

import (
	"fmt"
	"strconv"
)

// PositiveInt reads key through getenv. Unset returns def; a value that is
// not a positive integer is an error, so a typo stops the operator at start
// instead of being ignored.
func PositiveInt(getenv func(string) string, key string, def int) (int, error) {
	raw := getenv(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s=%q: want a positive integer", key, raw)
	}
	return n, nil
}
