package unit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readMigration003(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	migrationPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "internal", "db", "migrations", "000003_deploy_support.up.sql")
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("migration file not found: %v", err)
	}
	return string(data)
}

func TestMigration003PartialUniqueIndex(t *testing.T) {
	sql := readMigration003(t)

	t.Run("creates partial unique index on builds", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE UNIQUE INDEX") {
			t.Fatal("expected CREATE UNIQUE INDEX statement")
		}
	})

	t.Run("index name is idx_one_active_build_per_env", func(t *testing.T) {
		if !strings.Contains(sql, "idx_one_active_build_per_env") {
			t.Fatal("expected index name idx_one_active_build_per_env")
		}
	})

	t.Run("index targets builds table", func(t *testing.T) {
		if !strings.Contains(sql, "ON builds") {
			t.Fatal("expected index ON builds table")
		}
	})

	t.Run("index covers tenant_id project_id environment", func(t *testing.T) {
		if !strings.Contains(sql, "tenant_id") || !strings.Contains(sql, "project_id") || !strings.Contains(sql, "environment") {
			t.Fatal("expected index to cover (tenant_id, project_id, environment)")
		}
	})

	t.Run("index WHERE clause filters QUEUED and RUNNING", func(t *testing.T) {
		upper := strings.ToUpper(sql)
		if !strings.Contains(upper, "WHERE") {
			t.Fatal("expected WHERE clause for partial index")
		}
		if !strings.Contains(upper, "'QUEUED'") {
			t.Fatal("expected WHERE clause to include 'QUEUED'")
		}
		if !strings.Contains(upper, "'RUNNING'") {
			t.Fatal("expected WHERE clause to include 'RUNNING'")
		}
	})
}

func TestMigration003BuildAppsTable(t *testing.T) {
	sql := readMigration003(t)

	t.Run("creates build_apps table", func(t *testing.T) {
		if !strings.Contains(sql, "CREATE TABLE") || !strings.Contains(sql, "build_apps") {
			t.Fatal("expected CREATE TABLE build_apps")
		}
	})

	t.Run("build_apps has id UUID primary key", func(t *testing.T) {
		// Should have id column with UUID type and PRIMARY KEY
		upper := strings.ToUpper(sql)
		if !strings.Contains(upper, "UUID") {
			t.Fatal("expected UUID type for id column")
		}
		if !strings.Contains(upper, "GEN_RANDOM_UUID()") {
			t.Fatal("expected DEFAULT gen_random_uuid() for id column")
		}
	})

	t.Run("build_apps has build_id foreign key to builds", func(t *testing.T) {
		if !strings.Contains(sql, "build_id") {
			t.Fatal("expected build_id column")
		}
		if !strings.Contains(sql, "REFERENCES builds") {
			t.Fatal("expected REFERENCES builds for build_id")
		}
	})

	t.Run("build_apps has required columns", func(t *testing.T) {
		requiredColumns := []string{"app_name", "status", "duration_ms", "error", "image_digest", "url", "created_at", "updated_at"}
		for _, col := range requiredColumns {
			if !strings.Contains(sql, col) {
				t.Fatalf("expected column %s in build_apps table", col)
			}
		}
	})

	t.Run("build_apps has default status PENDING", func(t *testing.T) {
		if !strings.Contains(sql, "'PENDING'") {
			t.Fatal("expected DEFAULT 'PENDING' for status column")
		}
	})

	t.Run("build_apps has unique constraint on build_id and app_name", func(t *testing.T) {
		// Look for UNIQUE(build_id, app_name) with flexible whitespace
		found := false
		lines := strings.Split(sql, "\n")
		for _, line := range lines {
			upper := strings.ToUpper(strings.TrimSpace(line))
			if strings.Contains(upper, "UNIQUE") && strings.Contains(upper, "BUILD_ID") && strings.Contains(upper, "APP_NAME") {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected UNIQUE(build_id, app_name) constraint in build_apps table")
		}
	})
}

func TestMigration003ReleasesAppsColumn(t *testing.T) {
	sql := readMigration003(t)

	t.Run("adds apps JSONB column to releases", func(t *testing.T) {
		upper := strings.ToUpper(sql)
		if !strings.Contains(upper, "ALTER TABLE") {
			t.Fatal("expected ALTER TABLE statement to add column to releases")
		}
		if !strings.Contains(sql, "releases") {
			t.Fatal("expected ALTER TABLE on releases table")
		}
		if !strings.Contains(upper, "JSONB") {
			t.Fatal("expected JSONB type for apps column")
		}
		if !strings.Contains(sql, "apps") {
			t.Fatal("expected apps column name")
		}
	})
}

func TestMigration003Idempotency(t *testing.T) {
	sql := readMigration003(t)

	t.Run("CREATE TABLE uses IF NOT EXISTS", func(t *testing.T) {
		createTableCount := strings.Count(sql, "CREATE TABLE")
		createTableIFNE := strings.Count(sql, "CREATE TABLE IF NOT EXISTS")
		if createTableCount != createTableIFNE {
			t.Fatalf("expected all %d CREATE TABLE to use IF NOT EXISTS, only %d do", createTableCount, createTableIFNE)
		}
	})

	t.Run("CREATE INDEX uses IF NOT EXISTS", func(t *testing.T) {
		// Count all CREATE INDEX variants (including CREATE UNIQUE INDEX)
		createIndexCount := 0
		lines := strings.Split(sql, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(strings.ToUpper(line))
			if strings.HasPrefix(trimmed, "CREATE UNIQUE INDEX") || strings.HasPrefix(trimmed, "CREATE INDEX") {
				createIndexCount++
			}
		}
		ifNotExistsCount := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(strings.ToUpper(line))
			if (strings.HasPrefix(trimmed, "CREATE UNIQUE INDEX") || strings.HasPrefix(trimmed, "CREATE INDEX")) && strings.Contains(trimmed, "IF NOT EXISTS") {
				ifNotExistsCount++
			}
		}
		if createIndexCount != ifNotExistsCount {
			t.Fatalf("expected all %d CREATE INDEX to use IF NOT EXISTS, only %d do", createIndexCount, ifNotExistsCount)
		}
	})

	t.Run("ALTER TABLE uses IF NOT EXISTS for ADD COLUMN", func(t *testing.T) {
		// Ensure ALTER TABLE ADD COLUMN is idempotent
		upper := strings.ToUpper(sql)
		if strings.Contains(upper, "ADD COLUMN") {
			// Should handle idempotency — either via IF NOT EXISTS or DO $$ block
			hasIfNotExists := strings.Contains(upper, "ADD COLUMN IF NOT EXISTS")
			hasDollarBlock := strings.Contains(sql, "DO $$")
			if !hasIfNotExists && !hasDollarBlock {
				t.Fatal("ALTER TABLE ADD COLUMN must use IF NOT EXISTS or be wrapped in DO $$ block for idempotency")
			}
		}
	})
}

func TestMigration003ForwardOnly(t *testing.T) {
	sql := readMigration003(t)

	t.Run("no DROP statements", func(t *testing.T) {
		upper := strings.ToUpper(sql)
		if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DROP COLUMN") || strings.Contains(upper, "DROP INDEX") {
			t.Fatal("migration must not contain DROP statements (forward-only)")
		}
	})
}

func TestMigration003NoDownMigration(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	migrationDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "internal", "db", "migrations")

	t.Run("no down migration file exists", func(t *testing.T) {
		downPath := filepath.Join(migrationDir, "000003_deploy_support.down.sql")
		if _, err := os.Stat(downPath); err == nil {
			t.Fatal("down migration file must not exist (forward-only policy)")
		}
	})
}
