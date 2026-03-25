// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// PostgresSecretStore queries PostgreSQL for secret metadata.
// It NEVER returns secret values — only names and context.
type PostgresSecretStore struct {
	DB QueryExecutor
}

// ListNames returns metadata (name, environment, app) for all secrets
// matching the given tenant, project, and environment.
// Results are ordered alphabetically by name.
func (s *PostgresSecretStore) ListNames(ctx context.Context, tenantID, projectID, env string) ([]routes.SecretMetadata, error) {
	rows, err := s.DB.Query(ctx,
		"SELECT name, environment, app FROM secrets WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 ORDER BY name",
		tenantID, projectID, env,
	)
	if err != nil {
		return nil, fmt.Errorf("secret_store: list names: %w", err)
	}
	defer rows.Close()

	var result []routes.SecretMetadata
	for rows.Next() {
		var m routes.SecretMetadata
		if err := rows.Scan(&m.Name, &m.Environment, &m.App); err != nil {
			return nil, fmt.Errorf("secret_store: scan row: %w", err)
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("secret_store: iterate rows: %w", err)
	}

	// Return empty slice instead of nil to avoid null in JSON responses.
	if result == nil {
		result = []routes.SecretMetadata{}
	}

	return result, nil
}
