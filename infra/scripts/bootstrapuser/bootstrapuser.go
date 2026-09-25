// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package bootstrapuser holds the one rule every environment's bootstrap seed
// applies: the machine user a Hydra client_credentials JWT names (`sub`) exists,
// owns an account, and resolves to exactly ONE tenant.
//
// The rule matters because the JWT carries only `sub`, and the API answers
// TENANT_REQUIRED when that user belongs to more than one tenant. It used to be
// written three times — infra/helmfile/scripts/seed-local-user.sh, the drill's
// drill-mint-jwt.sh, and the E2E seed — and the copies drifted: when migration
// 000044 made tenants.account_id NOT NULL, only the local script learned to
// create the account, and the other two failed on every migrated database.
//
// SQL renders the rule as one script so each environment keeps its own
// transport (kubectl exec into the in-cluster Postgres, a transient psql pod
// inside the drill cluster's firewall, or a pgx connection) while the
// statements exist once.
package bootstrapuser

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalid is a field the database would reject, or that cannot be rendered
// as a SQL literal.
var ErrInvalid = errors.New("invalid bootstrap user")

// roles mirrors the tenant_members.role CHECK constraint.
var roles = map[string]bool{"owner": true, "admin": true, "member": true}

// User is the bootstrap identity and the one tenant it resolves to.
type User struct {
	ID          string // users.id — the JWT subject
	Email       string
	DisplayName string
	Username    string
	AccountID   string // the account created when the user owns none yet
	TenantID    string // the only tenant the user is left a member of
	Role        string // owner | admin | member
}

// SQL returns the idempotent script that applies the rule. Every statement is
// safe to re-run: rows are upserted or left alone, and the only deletion is of
// memberships outside TenantID.
func (u User) SQL() (string, error) {
	if err := u.validate(); err != nil {
		return "", err
	}
	id, tenant := quote(u.ID), quote(u.TenantID)
	var b strings.Builder
	fmt.Fprintf(&b, `INSERT INTO users (id, email, display_name, username)
VALUES (%s, %s, %s, %s)
ON CONFLICT (id) DO UPDATE SET
  email = EXCLUDED.email,
  display_name = EXCLUDED.display_name,
  username = EXCLUDED.username,
  updated_at = now();
`, id, quote(u.Email), quote(u.DisplayName), quote(u.Username))
	// One account per owner (UNIQUE owner_id, migration 000034): an account the
	// user already owns — minted by EnsureAccount on a first login — is kept.
	fmt.Fprintf(&b, `INSERT INTO accounts (id, display_name, plan, owner_id)
VALUES (%s, %s, 'starter', %s)
ON CONFLICT (owner_id) DO NOTHING;
`, quote(u.AccountID), quote(u.DisplayName), id)
	// account_id is resolved by owner, so the tenant lands on whichever account
	// the user owns, whether it was created above or earlier.
	fmt.Fprintf(&b, `INSERT INTO tenants (id, slug, owner_id, display_name, account_id)
SELECT %s, %s, %s, %s, a.id FROM accounts a WHERE a.owner_id = %s
ON CONFLICT (id) DO NOTHING;
`, tenant, quote(slug(u.TenantID)), id, tenant, id)
	fmt.Fprintf(&b, "DELETE FROM tenant_members WHERE user_id = %s AND tenant_id <> %s;\n", id, tenant)
	fmt.Fprintf(&b, `INSERT INTO tenant_members (tenant_id, user_id, role)
VALUES (%s, %s, %s)
ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = now();
`, tenant, id, quote(u.Role))
	return b.String(), nil
}

func (u User) validate() error {
	fields := []struct{ name, value string }{
		{"id", u.ID}, {"email", u.Email}, {"display name", u.DisplayName},
		{"username", u.Username}, {"account id", u.AccountID},
		{"tenant id", u.TenantID}, {"role", u.Role},
	}
	for _, f := range fields {
		if f.value == "" {
			return fmt.Errorf("%w: %s is empty", ErrInvalid, f.name)
		}
		if strings.ContainsRune(f.value, 0) {
			return fmt.Errorf("%w: %s contains a NUL byte", ErrInvalid, f.name)
		}
	}
	if !roles[u.Role] {
		return fmt.Errorf("%w: role %q is not owner, admin or member", ErrInvalid, u.Role)
	}
	return nil
}

// slug is the tenant slug the seeds have always derived from the id: every
// character outside [a-zA-Z0-9] becomes '-', then the result is lowercased.
func slug(id string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, id))
}

// quote renders s as a Postgres string literal, the way lib/pq's QuoteLiteral
// does: single quotes are doubled, and a value holding a backslash becomes an
// E-prefixed escape-string literal with the backslash escaped, so it reads the
// same whatever standard_conforming_strings is set to.
func quote(s string) string {
	s = strings.ReplaceAll(s, `'`, `''`)
	if strings.Contains(s, `\`) {
		return `E'` + strings.ReplaceAll(s, `\`, `\\`) + `'`
	}
	return `'` + s + `'`
}
