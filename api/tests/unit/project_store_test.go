package unit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

func TestPostgresProjectStoreImplementsInterface(t *testing.T) {
	// Compile-time check: PostgresProjectStore implements routes.ProjectStore.
	var _ routes.ProjectStore = (*db.PostgresProjectStore)(nil)
}

func TestGenerateProjectID(t *testing.T) {
	t.Run("ID has proj- prefix and 8 hex chars", func(t *testing.T) {
		id := db.GenerateProjectID()
		if !strings.HasPrefix(id, "proj-") {
			t.Errorf("expected proj- prefix, got %q", id)
		}
		hexPart := strings.TrimPrefix(id, "proj-")
		if len(hexPart) != 8 {
			t.Errorf("expected 8 hex chars after prefix, got %d chars: %q", len(hexPart), hexPart)
		}
		for _, c := range hexPart {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Errorf("expected hex char, got %c in %q", c, id)
			}
		}
	})

	t.Run("IDs are unique", func(t *testing.T) {
		seen := make(map[string]bool)
		for range 100 {
			id := db.GenerateProjectID()
			if seen[id] {
				t.Fatalf("duplicate ID generated: %s", id)
			}
			seen[id] = true
		}
	})
}

func TestPostgresProjectStoreCreateProjectValidation(t *testing.T) {
	t.Run("passes correct input to query executor", func(t *testing.T) {
		// Use a fake that captures the SQL and args
		fake := &fakeQueryExecForInsert{
			shouldConflict: false,
		}
		store := &db.PostgresProjectStore{DB: fake}

		result, err := store.CreateProject(context.Background(), routes.CreateProjectInput{
			TenantID:  "acme",
			Name:      "my-saas",
			CreatedBy: "github-lucas",
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !strings.HasPrefix(result.ID, "proj-") {
			t.Errorf("expected proj- prefix, got %q", result.ID)
		}
		if result.Name != "my-saas" {
			t.Errorf("expected Name=my-saas, got %q", result.Name)
		}
		if result.TeamID != "acme" {
			t.Errorf("expected TeamID=acme, got %q", result.TeamID)
		}
	})

	t.Run("unique constraint violation returns ErrProjectExists", func(t *testing.T) {
		fake := &fakeQueryExecForInsert{
			shouldConflict: true,
		}
		store := &db.PostgresProjectStore{DB: fake}

		_, err := store.CreateProject(context.Background(), routes.CreateProjectInput{
			TenantID:  "acme",
			Name:      "my-saas",
			CreatedBy: "github-lucas",
		})

		if !errors.Is(err, routes.ErrProjectExists) {
			t.Fatalf("expected ErrProjectExists, got %v", err)
		}
	})
}

// fakeQueryExecForInsert implements db.DBTX for testing CreateProject.
type fakeQueryExecForInsert struct {
	shouldConflict bool
}

func (f *fakeQueryExecForInsert) Exec(ctx context.Context, sql string, args ...any) (db.ExecResult, error) {
	if f.shouldConflict {
		return db.ExecResult{}, &db.UniqueViolationError{Constraint: "projects_tenant_id_name_key"}
	}
	return db.ExecResult{RowsAffected: 1}, nil
}

func (f *fakeQueryExecForInsert) Query(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	return &emptyRows{}, nil
}

func (f *fakeQueryExecForInsert) QueryRow(ctx context.Context, sql string, args ...any) db.Row {
	return &fakeRow{}
}
