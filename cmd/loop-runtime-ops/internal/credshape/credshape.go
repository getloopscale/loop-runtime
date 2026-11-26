// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package credshape is the single owner of the rules that decide whether a
// credential value is usable: whether it is still a substitution marker, and
// whether a Stripe key has the shape of a secret key.
//
// Both the seeds (which refuse to write a marker into the cluster) and the leak
// probe (which reports a marker that reached the cluster anyway) read these
// rules. They were four divergent copies before this package existed and
// already disagreed — `changeme` in one, `CHANGEME` in another, `TODO_SET_ME`
// in only one — so the seed and the probe answered the same question
// differently.
package credshape

import (
	"errors"
	"fmt"
	"strings"
)

// placeholderMarkers are the substrings the repository writes where a real
// credential belongs: Helm `| default` values, the dev-credentials template,
// and the bash seeds' sentinels. Ordered most specific first, so a report
// names the marker an operator can grep for.
var placeholderMarkers = []string{
	"REPLACE_VIA_PULUMI",
	"REPLACE_VIA_",
	"REPLACE_",
	"whsec_dev_",
	"PLACEHOLDER",
	"TODO_SET_ME",
	"CHANGEME",
	"changeme",
}

// PlaceholderMarker returns the first placeholder marker contained in v.
func PlaceholderMarker(v string) (string, bool) {
	for _, m := range placeholderMarkers {
		if strings.Contains(v, m) {
			return m, true
		}
	}
	return "", false
}

// IsPlaceholder reports whether v is blank or still carries a placeholder
// marker, i.e. whether it must be treated as an absent credential.
func IsPlaceholder(v string) bool {
	if strings.TrimSpace(v) == "" {
		return true
	}
	_, ok := PlaceholderMarker(v)
	return ok
}

var (
	// ErrStripeKeyPlaceholder: the key is absent or still a placeholder. Callers
	// decide whether absence is a SKIP or a FAIL (capability policy).
	ErrStripeKeyPlaceholder = errors.New("stripe api key is absent or a placeholder")
	// ErrStripeKeyWrongPrefix: something that is not a Stripe secret/restricted
	// key was supplied — typically a publishable key (pk_*). Always a defect.
	ErrStripeKeyWrongPrefix = errors.New("stripe api key is not a secret or restricted key")
)

var stripeSecretKeyPrefixes = []string{"rk_test_", "rk_live_", "sk_test_", "sk_live_"}

// ParseStripeSecretKey validates that k is usable as the server-side Stripe
// API key. The returned error never contains the key body: at most its first
// eight characters, which carry the prefix and nothing secret.
func ParseStripeSecretKey(k string) error {
	k = strings.TrimSpace(k)
	if IsPlaceholder(k) {
		return ErrStripeKeyPlaceholder
	}
	for _, p := range stripeSecretKeyPrefixes {
		if strings.HasPrefix(k, p) {
			return nil
		}
	}
	return fmt.Errorf("%w: got %q…, expected one of %s", ErrStripeKeyWrongPrefix,
		prefixOf(k), strings.Join(stripeSecretKeyPrefixes, "/"))
}

func prefixOf(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}
