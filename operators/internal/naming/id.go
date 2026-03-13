package naming

import (
	"crypto/rand"
	"fmt"
)

// GenerateBuildID returns a unique build identifier in the format "build-{8 hex chars}".
func GenerateBuildID() string {
	return fmt.Sprintf("build-%s", randomHex(8))
}

// GenerateReleaseID returns a unique release identifier in the format "rel-{8 hex chars}".
func GenerateReleaseID() string {
	return fmt.Sprintf("rel-%s", randomHex(8))
}

// randomHex generates a random hex string of the specified number of characters.
// It uses crypto/rand for cryptographic randomness.
func randomHex(n int) string {
	// Each byte produces 2 hex characters, so we need n/2 bytes (rounded up).
	byteLen := (n + 1) / 2
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate random bytes: %v", err))
	}
	return fmt.Sprintf("%x", b)[:n]
}
