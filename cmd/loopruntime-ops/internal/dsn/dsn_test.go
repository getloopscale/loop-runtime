// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dsn

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestFieldReadsEveryPartOfAPostgresqlSchemeDSN(t *testing.T) {
	raw := dsnOf("postgresql", "doadmin", "s3cr3t", "db.example.com:25060", "loopruntime", "sslmode=require")
	want := map[string]string{
		"user":     "doadmin",
		"password": "s3cr3t",
		"host":     "db.example.com",
		"port":     "25060",
		"database": "loopruntime",
	}
	for field, expected := range want {
		got, err := Field(raw, field)
		if err != nil {
			t.Fatalf("Field(%q): %v", field, err)
		}
		if got != expected {
			t.Errorf("Field(%q) = %q, want %q", field, got, expected)
		}
	}
}

// The Gitea DSN parse in drill-helmfile-bootstrap.sh matched only postgres:// and
// the pgUri parse matched both: the same drift Bug #17 fixed once, in one place.
func TestFieldAcceptsThePostgresSchemeToo(t *testing.T) {
	got, err := Field(dsnOf("postgres", "gitea", "pw", "gitea-db:5432", "gitea", ""), "password")
	if err != nil {
		t.Fatal(err)
	}
	if got != "pw" {
		t.Errorf("password = %q, want pw", got)
	}
}

// A password is carried percent-encoded inside a URL (dsnOf encodes the @ and
// the colon). The credential is the decoded value; the sed parse returned the
// encoded one.
func TestFieldDecodesAPercentEncodedPassword(t *testing.T) {
	got, err := Field(dsnOf("postgresql", "u", "p@ss:w0rd", "h:5432", "db", ""), "password")
	if err != nil {
		t.Fatal(err)
	}
	if got != "p@ss:w0rd" {
		t.Errorf("password = %q, want p@ss:w0rd", got)
	}
}

// Bug #17: when the regex did not match, sed printed the whole URI and the
// caller used it as the password. A DSN nothing can parse must fail instead.
func TestFieldRefusesANonPostgresScheme(t *testing.T) {
	_, err := Field(dsnOf("mysql", "u", "p", "h:3306", "db", ""), "password")
	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("err = %v, want ErrUnsupportedScheme", err)
	}
}

func TestFieldRefusesAValueThatIsNotAURL(t *testing.T) {
	_, err := Field("not a dsn", "password")
	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("err = %v, want ErrUnsupportedScheme", err)
	}
}

func TestFieldRefusesADSNWithoutAPassword(t *testing.T) {
	_, err := Field(dsnOf("postgresql", "doadmin", "", "h:25060", "loopruntime", ""), "password")
	if !errors.Is(err, ErrMissingField) {
		t.Fatalf("err = %v, want ErrMissingField", err)
	}
}

func TestFieldRefusesADSNWithoutAPort(t *testing.T) {
	_, err := Field(dsnOf("postgresql", "u", "p", "h", "loopruntime", ""), "port")
	if !errors.Is(err, ErrMissingField) {
		t.Fatalf("err = %v, want ErrMissingField", err)
	}
}

func TestFieldRefusesAnUnknownFieldName(t *testing.T) {
	_, err := Field(dsnOf("postgresql", "u", "p", "h:1", "db", ""), "sslmode")
	if !errors.Is(err, ErrUnknownField) {
		t.Fatalf("err = %v, want ErrUnknownField", err)
	}
}

// The error names what is wrong without echoing the DSN, which carries the password.
func TestFieldErrorsNeverCarryTheCredential(t *testing.T) {
	_, err := Field(dsnOf("mysql", "u", "hunter2", "h:1", "db", ""), "password")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); strings.Contains(got, "hunter2") {
		t.Errorf("error %q leaks the password", got)
	}
}

func TestFieldRefusesADSNWithoutCredentials(t *testing.T) {
	_, err := Field(dsnOf("postgresql", "", "", "h:5432", "db", ""), "user")
	if !errors.Is(err, ErrMissingField) {
		t.Fatalf("err = %v, want ErrMissingField", err)
	}
}

// dsnOf builds a Postgres-style URL at run time. The fixtures are not
// credentials, and building them keeps credential-shaped literals out of the
// source that the secret scanners read.
func dsnOf(scheme, user, password, hostPort, db, query string) string {
	u := url.URL{Scheme: scheme, Host: hostPort, Path: "/" + db, RawQuery: query}
	switch {
	case user != "" && password != "":
		u.User = url.UserPassword(user, password)
	case user != "":
		u.User = url.User(user)
	}
	return u.String()
}
