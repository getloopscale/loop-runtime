// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	errorshttp "github.com/getloopscale/loopzero/pkg/errors/http"

	// The api registers the codes it owns from httpio's init; every route
	// package imports httpio, so production always has them. Importing it
	// here makes the test see the same registry the server answers from.
	_ "github.com/getloopscale/loopzero/api/internal/httpio"
)

// A LoopZeroError code with no registered HTTP status is answered as 500 by the
// problem+json writer, whatever status its constructor was passed (the
// argument is ignored). INVALID_APP_NAME and INVALID_APP_PATH reached users
// as 500 that way while the code intended 400. Every code the HTTP layer
// constructs must therefore be registered — by pkg/errors/http for the codes
// pkg/errors owns, by its owning module for every other code.
var constructedCodeRE = regexp.MustCompile(`(?:NewLoopZeroError\(\s*|Code:\s*)"([A-Z][A-Z0-9_]*)"`)

func TestEveryErrorCodeTheHTTPLayerConstructsHasARegisteredStatus(t *testing.T) {
	unregistered := map[string][]string{}
	for _, dir := range []string{"../routes", "../server", "../middleware", "../sse"} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, werr error) error {
			if werr != nil {
				return werr
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				return err
			}
			for _, m := range constructedCodeRE.FindAllStringSubmatch(string(src), -1) {
				if !errorshttp.IsRegistered(m[1]) {
					unregistered[m[1]] = append(unregistered[m[1]], path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scanning %s: %v", dir, err)
		}
	}
	codes := make([]string, 0, len(unregistered))
	for c := range unregistered {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		t.Errorf("code %s has no registered HTTP status (answered as 500): %v", c, unregistered[c])
	}
}
