// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package dsn reads the parts of a Postgres connection URL.
//
// The drill scripts extracted the password from a DSN with
// `sed -E 's|^postgres(ql)?://[^:]+:([^@]+)@.*$|\2|'`, copied to four places
// and a fifth, narrower variant for the Gitea DSN that matched only
// `postgres://`. Bug #17 was that regex failing to match a `postgresql://`
// URI: sed then printed the whole URI and the caller used it as the password.
// This package parses with net/url once, accepts both schemes, decodes a
// percent-encoded password, and fails loudly instead of returning the input.
package dsn

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	// ErrUnsupportedScheme is a value that is not a postgres:// or
	// postgresql:// URL.
	ErrUnsupportedScheme = errors.New("not a postgres:// or postgresql:// URL")
	// ErrMissingField is a DSN that parses but does not carry the field asked for.
	ErrMissingField = errors.New("field absent from the DSN")
	// ErrUnknownField is a field name this package does not read.
	ErrUnknownField = errors.New("unknown DSN field")
)

// Fields lists the names Field accepts, in the order a DSN spells them.
var Fields = []string{"user", "password", "host", "port", "database"}

// Field returns one part of a Postgres DSN: user, password (decoded), host,
// port or database. Errors never carry the DSN itself, because it holds the
// password.
func Field(raw, name string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", ErrUnsupportedScheme
	}
	var value string
	switch name {
	case "user":
		value = u.User.Username()
	case "password":
		value, _ = u.User.Password()
	case "host":
		value = u.Hostname()
	case "port":
		value = u.Port()
	case "database":
		value = strings.TrimPrefix(u.Path, "/")
	default:
		return "", fmt.Errorf("%w %q (want one of %s)", ErrUnknownField, name, strings.Join(Fields, ", "))
	}
	if value == "" {
		return "", fmt.Errorf("%w: %s", ErrMissingField, name)
	}
	return value, nil
}
