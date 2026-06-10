// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package memory_webhook_worker implements the dispatcher that delivers
// memory_webhook_deliveries rows to customer-registered webhooks. Per
// Memory Webhook Delivery plan v1.0 (D1-D5).
package memory_webhook_worker

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Sign computes the HMAC-SHA256 of the timestamp-prefixed body using the
// per-webhook secret. Per D1, the signed payload is `{ts}.{body}` —
// Stripe's documented pattern. This binds the signature to BOTH the
// content AND a specific moment in time, foiling replay attacks if the
// consumer enforces a window.
//
// Returns the signature in lowercase hex.
func Sign(secret string, unixTimestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", unixTimestamp)))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// HeaderValue formats the signature as the value for the X-LoopZero-Signature
// header. Stripe-aligned: `t={unix},v1={hex_hmac}`. The `v1` marker
// reserves namespace for future signature scheme migrations.
func HeaderValue(unixTimestamp int64, hexHMAC string) string {
	return fmt.Sprintf("t=%d,v1=%s", unixTimestamp, hexHMAC)
}
