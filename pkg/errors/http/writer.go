// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package http

import (
	"encoding/json"
	stderrors "errors"
	"net/http"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// ProblemTypeBase is the URI prefix for RFC 9457 "type" fields.
// The full URI is constructed as ProblemTypeBase + kebab-case(code).
//
// Example: code "LOCK_TIMEOUT" → "https://loopzero.example/errors/lock-timeout".
//
// Per ADR-0030 + gate PKG-G15.
const ProblemTypeBase = "https://loopzero.example/errors/"

// problemDetails is the RFC 9457 wire payload. Shape:
//
//	{
//	  "type":     "https://loopzero.example/errors/lock-timeout",
//	  "title":    "Lock Timeout",
//	  "status":   409,
//	  "detail":   "could not acquire environment lock within timeout",
//	  "message":  "could not acquire environment lock within timeout",
//	  "code":     "LOCK_TIMEOUT",
//	  "instance": "/projects/foo/environments/staging"
//	}
//
// `message` is emitted alongside `detail` for backward compatibility
// with pre-Stage-C clients/tests that read the legacy ToJSON field.
// The two values are identical; clients can choose either. RFC 9457
// itself permits extension members.
type problemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Message  string `json:"message"` // backward-compat alias for legacy clients
	Code     string `json:"code"`
	Instance string `json:"instance,omitempty"`
}

// WriteJSON writes a RFC 9457 problem+json response for the given error.
// Sets Content-Type: application/problem+json and writes the resolved
// HTTP status code (via Status). When r is non-nil, populates the
// "instance" field with r.URL.Path.
//
// For non-LoopZeroError values, emits a generic INTERNAL_ERROR payload at
// status 500. This is safe and consistent — no stack traces or
// internal strings leak.
//
// Per ADR-0030 + gate PKG-G15.
func WriteJSON(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("Content-Type", "application/problem+json")
	status := Status(err)
	w.WriteHeader(status)

	var loopzeroErr *loopzeroerrors.LoopZeroError
	stderrors.As(err, &loopzeroErr)

	code := "INTERNAL_ERROR"
	detail := "An internal error occurred."
	if loopzeroErr != nil {
		code = loopzeroErr.Code
		detail = loopzeroErr.Message
	}

	pd := problemDetails{
		Type:    ProblemTypeBase + kebabCase(code),
		Title:   titleFromCode(code),
		Status:  status,
		Detail:  detail,
		Message: detail, // backward-compat alias (Stage C migration)
		Code:    code,
	}
	if r != nil {
		pd.Instance = r.URL.Path
	}

	_ = json.NewEncoder(w).Encode(pd)
}

// kebabCase converts an UPPER_SNAKE_CASE code to lower-kebab-case.
//   "LOCK_TIMEOUT" → "lock-timeout".
func kebabCase(code string) string {
	out := make([]byte, len(code))
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '_':
			out[i] = '-'
		case c >= 'A' && c <= 'Z':
			out[i] = c + ('a' - 'A')
		default:
			out[i] = c
		}
	}
	return string(out)
}

// titleFromCode produces a human-readable title from an error code.
//   "LOCK_TIMEOUT" → "Lock Timeout".
func titleFromCode(code string) string {
	if code == "" {
		return "Error"
	}
	out := make([]byte, 0, len(code))
	upperNext := true
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '_':
			out = append(out, ' ')
			upperNext = true
		case c >= 'A' && c <= 'Z':
			if upperNext {
				out = append(out, c)
			} else {
				out = append(out, c+('a'-'A'))
			}
			upperNext = false
		default:
			out = append(out, c)
			upperNext = false
		}
	}
	return string(out)
}
