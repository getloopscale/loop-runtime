package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// PostgresProjectStore creates projects in PostgreSQL.
// It implements routes.ProjectStore (DIP).
type PostgresProjectStore struct {
	DB ExecQueryExecutor
}

// GenerateProjectID generates a project ID in the format "proj-{8 hex chars}".
func GenerateProjectID() string {
	b := make([]byte, 4) // 4 bytes = 8 hex chars
	_, _ = rand.Read(b)
	return "proj-" + hex.EncodeToString(b)
}

// CreateProject inserts a new project into the database.
// Returns routes.ErrProjectExists if the (tenant_id, name) unique constraint is violated.
func (s *PostgresProjectStore) CreateProject(ctx context.Context, input routes.CreateProjectInput) (routes.ProjectResult, error) {
	id := GenerateProjectID()

	_, err := s.DB.Exec(ctx,
		"INSERT INTO projects (id, tenant_id, name, created_by) VALUES ($1, $2, $3, $4)",
		id, input.TenantID, input.Name, input.CreatedBy,
	)
	if err != nil {
		var uv *UniqueViolationError
		if errors.As(err, &uv) {
			return routes.ProjectResult{}, routes.ErrProjectExists
		}
		return routes.ProjectResult{}, fmt.Errorf("project store: insert failed: %w", err)
	}

	return routes.ProjectResult{
		ID:     id,
		Name:   input.Name,
		TeamID: input.TenantID,
	}, nil
}
