// Package id provides deterministic ID generation for LoopZero resources.
// IDs use crypto/rand for cryptographic randomness.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateBuildID returns a unique build identifier in the format "build-{8hex}".
func GenerateBuildID() string {
	return fmt.Sprintf("build-%s", randomHex(4))
}

// GenerateReleaseID returns a unique release identifier in the format "rel-{8hex}".
func GenerateReleaseID() string {
	return fmt.Sprintf("rel-%s", randomHex(4))
}

// randomHex generates a random hex string of n*2 characters from n random bytes.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
