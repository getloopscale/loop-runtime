package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration002(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "internal", "db", "migrations", "000002_auth_tenants.up.sql")
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration002Tables(t *testing.T) {
	sql := readMigration002(t)

	t.Run("creates users table", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS users") {
			t.Fatal("expected CREATE TABLE IF NOT EXISTS users")
		}
	})

	t.Run("creates tenants table", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS tenants") {
			t.Fatal("expected CREATE TABLE IF NOT EXISTS tenants")
		}
	})

	t.Run("creates tenant_members table", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS tenant_members") {
			t.Fatal("expected CREATE TABLE IF NOT EXISTS tenant_members")
		}
	})

	t.Run("creates projects table", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS projects") {
			t.Fatal("expected CREATE TABLE IF NOT EXISTS projects")
		}
	})
}

func TestMigration002Constraints(t *testing.T) {
	sql := readMigration002(t)

	t.Run("tenant_members has composite primary key", func(t *testing.T) {
		if !strings.Contains(sql, "PRIMARY KEY (tenant_id, user_id)") {
			t.Fatal("expected PRIMARY KEY (tenant_id, user_id)")
		}
	})

	t.Run("projects has unique constraint on (tenant_id, name)", func(t *testing.T) {
		if !strings.Contains(sql, "UNIQUE") || !strings.Contains(sql, "tenant_id") {
			t.Fatal("expected UNIQUE constraint involving tenant_id")
		}
	})

	t.Run("tenants has foreign key to users (owner)", func(t *testing.T) {
		if !strings.Contains(sql, "REFERENCES users") {
			t.Fatal("expected REFERENCES users in tenants table")
		}
	})

	t.Run("tenant_members has foreign keys to tenants and users", func(t *testing.T) {
		// Should have at least 2 REFERENCES (one to tenants, one to users)
		count := strings.Count(sql, "REFERENCES")
		if count < 3 { // tenants→users, tenant_members→tenants, tenant_members→users
			t.Fatalf("expected at least 3 REFERENCES, got %d", count)
		}
	})
}

func TestMigration002Idempotency(t *testing.T) {
	sql := readMigration002(t)

	t.Run("all CREATE TABLE use IF NOT EXISTS", func(t *testing.T) {
		createTableCount := strings.Count(sql, "CREATE TABLE")
		createTableIFNE := strings.Count(sql, "CREATE TABLE IF NOT EXISTS")
		if createTableCount != createTableIFNE {
			t.Fatalf("expected all %d CREATE TABLE to use IF NOT EXISTS, only %d do", createTableCount, createTableIFNE)
		}
	})

	t.Run("all CREATE INDEX use IF NOT EXISTS", func(t *testing.T) {
		createIndexCount := strings.Count(sql, "CREATE INDEX")
		createIndexIFNE := strings.Count(sql, "CREATE INDEX IF NOT EXISTS")
		if createIndexCount != createIndexIFNE {
			t.Fatalf("expected all %d CREATE INDEX to use IF NOT EXISTS, only %d do", createIndexCount, createIndexIFNE)
		}
	})
}

func TestMigration002ForwardOnly(t *testing.T) {
	sql := readMigration002(t)

	t.Run("no DROP statements", func(t *testing.T) {
		upper := strings.ToUpper(sql)
		if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DROP COLUMN") || strings.Contains(upper, "DROP INDEX") {
			t.Fatal("migration must not contain DROP statements (forward-only)")
		}
	})
}
