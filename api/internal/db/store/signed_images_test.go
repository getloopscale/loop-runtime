// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"

	"github.com/jackc/pgx/v5"
)

func TestAppAttestationStoreSqlc_FindSignedImages_RejectsEmptyInputs(t *testing.T) {
	// Validation fires before s.gen(tx); a zero store is safe.
	s := &AppAttestationStoreSqlc{}
	for _, in := range [][3]string{{"", "p", "c"}, {"t", "", "c"}, {"t", "p", ""}} {
		_, err := s.FindSignedImages(context.Background(), nil, in[0], in[1], in[2])
		if !errors.Is(err, loopzeroerrors.ErrMissingField) {
			t.Errorf("FindSignedImages(%q, %q, %q) error = %v, want ErrMissingField", in[0], in[1], in[2], err)
		}
	}
}

// TestAppAttestationStoreSqlc_FindSignedImages_RealPG runs the query against a
// Postgres loaded with schema.gen.sql (LOOPZERO_E2E_DATABASE_URL). It seeds
// inside a transaction it rolls back, so it leaves no rows behind.
func TestAppAttestationStoreSqlc_FindSignedImages_RealPG(t *testing.T) {
	dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPZERO_E2E_DATABASE_URL not set: real-PG query test skipped")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	base := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	type seed struct {
		tenant, project, commit, app, digest string
		signed                               bool
		at                                   time.Duration
	}
	seeds := []seed{
		{"tn-a", "web-shop", "c1", "api", "sha256:01", true, 0},
		{"tn-a", "web-shop", "c1", "api", "sha256:02", true, time.Hour}, // newest for api
		{"tn-a", "web-shop", "c1", "web", "sha256:03", false, 0},        // never signed
		{"tn-a", "web-shop", "c2", "api", "sha256:04", true, 2 * time.Hour},
		{"tn-a", "other", "c1", "api", "sha256:05", true, 2 * time.Hour},
		{"tn-b", "web-shop", "c1", "api", "sha256:06", true, 2 * time.Hour},
	}
	for i, sd := range seeds {
		buildID := "bld-signed-images-" + string(rune('a'+i))
		releaseID := "rel-signed-images-" + string(rune('a'+i))
		if _, err := tx.Exec(ctx, `INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by, commit_sha, cell_id)
			VALUES ($1, $2, $3, 'production', 'SUCCEEDED', 'corr', 'user', $4, 'cell-0')`,
			buildID, sd.tenant, sd.project, sd.commit); err != nil {
			t.Fatalf("seed build %d: %v", i, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, cell_id)
			VALUES ($1, $2, $3, $4, 'production', 'SUPERSEDED', 'corr', 'user', 'cell-0')`,
			releaseID, buildID, sd.tenant, sd.project); err != nil {
			t.Fatalf("seed release %d: %v", i, err)
		}
		var signature *string
		var signedAt *time.Time
		if sd.signed {
			sig := "sig-" + sd.digest
			at := base.Add(sd.at)
			signature, signedAt = &sig, &at
		}
		if _, err := tx.Exec(ctx, `INSERT INTO release_app_attestations (id, release_id, tenant_id, app_name, image_digest, image_signature, signed_at, sbom_format, sbom_component_count, cell_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'cyclonedx-json', 3, 'cell-0', $8)`,
			"raa-signed-images-"+string(rune('a'+i)), releaseID, sd.tenant, sd.app, sd.digest, signature, signedAt, base.Add(sd.at)); err != nil {
			t.Fatalf("seed attestation %d: %v", i, err)
		}
	}

	images, err := NewAppAttestationStoreSqlc(nil).FindSignedImages(ctx, &db.PgxTxAdapter{Tx: tx}, "tn-a", "web-shop", "c1")
	if err != nil {
		t.Fatalf("FindSignedImages: %v", err)
	}

	want := db.ReusedImage{Digest: "sha256:02", Signature: "sig-sha256:02", SignedAt: base.Add(time.Hour),
		SBOMFormat: "cyclonedx-json", SBOMComponentCount: 3}
	if len(images) != 1 || images["api"] != want {
		t.Errorf("FindSignedImages = %+v, want only api = %+v", images, want)
	}
}
