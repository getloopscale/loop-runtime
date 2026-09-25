// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package bootstrapuser

import (
	"errors"
	"strings"
	"testing"
)

func validUser() User {
	return User{
		ID:          "loopzero-bootstrap",
		Email:       "bootstrap@loopzero.local",
		DisplayName: "Bootstrap User",
		Username:    "bootstrap",
		AccountID:   "acct-bootstrap",
		TenantID:    "tn-e2etest",
		Role:        "owner",
	}
}

// The rows reference each other: accounts.owner_id -> users, tenants.account_id
// -> accounts (NOT NULL since migration 000044), tenant_members -> both. A
// script that inserts them out of order fails on the first fresh database.
func TestSQLWritesTheRowsInForeignKeyOrder(t *testing.T) {
	sql, err := validUser().SQL()
	if err != nil {
		t.Fatal(err)
	}
	order := []string{
		"INSERT INTO users",
		"INSERT INTO accounts",
		"INSERT INTO tenants",
		"DELETE FROM tenant_members",
		"INSERT INTO tenant_members",
	}
	last := -1
	for _, stmt := range order {
		i := strings.Index(sql, stmt)
		if i < 0 {
			t.Fatalf("SQL has no %q:\n%s", stmt, sql)
		}
		if i < last {
			t.Errorf("%q appears before the statement it depends on:\n%s", stmt, sql)
		}
		last = i
	}
}

// The drill copy of this seed inserted the tenant without account_id and failed
// on every database migrated past 000044.
func TestSQLGivesTheTenantTheUsersAccount(t *testing.T) {
	sql, err := validUser().SQL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "INSERT INTO tenants (id, slug, owner_id, display_name, account_id)") {
		t.Errorf("tenant insert does not set account_id:\n%s", sql)
	}
}

// The rule itself: the user's JWT carries only `sub`, and the API answers
// TENANT_REQUIRED when that user belongs to more than one tenant.
func TestSQLRemovesEveryMembershipOutsideTheCanonicalTenant(t *testing.T) {
	sql, err := validUser().SQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "DELETE FROM tenant_members WHERE user_id = 'loopzero-bootstrap' AND tenant_id <> 'tn-e2etest';"
	if !strings.Contains(sql, want) {
		t.Errorf("SQL lacks %q:\n%s", want, sql)
	}
}

func TestSQLUpsertsTheMembershipRole(t *testing.T) {
	u := validUser()
	u.Role = "admin"
	sql, err := u.SQL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "VALUES ('tn-e2etest', 'loopzero-bootstrap', 'admin')") ||
		!strings.Contains(sql, "ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role") {
		t.Errorf("membership is not upserted with role admin:\n%s", sql)
	}
}

func TestSQLDerivesTheSlugFromTheTenantID(t *testing.T) {
	u := validUser()
	u.TenantID = "Tn_Seed.One"
	sql, err := u.SQL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "SELECT 'Tn_Seed.One', 'tn-seed-one', 'loopzero-bootstrap'") {
		t.Errorf("slug not derived as tn-seed-one:\n%s", sql)
	}
}

// Every value is an operator-supplied string spliced into SQL; quoting is what
// keeps a stray quote from ending the literal.
func TestSQLDoublesASingleQuoteInsideAValue(t *testing.T) {
	u := validUser()
	u.DisplayName = "O'Brien"
	sql, err := u.SQL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "'O''Brien'") {
		t.Errorf("single quote not doubled:\n%s", sql)
	}
	if strings.Contains(sql, "'O'Brien'") {
		t.Errorf("raw single quote reached the SQL:\n%s", sql)
	}
}

func TestSQLEscapesABackslashWhateverTheServerSetting(t *testing.T) {
	u := validUser()
	u.DisplayName = `a\b`
	sql, err := u.SQL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, `E'a\\b'`) {
		t.Errorf("backslash not escaped into an E'' literal:\n%s", sql)
	}
}

func TestSQLRefusesAnEmptyField(t *testing.T) {
	u := validUser()
	u.TenantID = ""
	if _, err := u.SQL(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

// tenant_members.role carries CHECK (role IN ('owner','admin','member')).
func TestSQLRefusesARoleTheSchemaRejects(t *testing.T) {
	u := validUser()
	u.Role = "superuser"
	if _, err := u.SQL(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

// Postgres text cannot hold a NUL byte; quoting cannot make it safe.
func TestSQLRefusesANulByte(t *testing.T) {
	u := validUser()
	u.Email = "a\x00b@x"
	if _, err := u.SQL(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}
