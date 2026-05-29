// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"encoding/hex"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/source"
	"github.com/loopzero/loopzero/api/internal/sse"
	s3uploader "github.com/loopzero/loopzero/api/internal/s3"
)

// StoreDeps holds all store instances created from the database adapter.
//
// T1.1c2c + T1.2b (plan 2026-05-29-sqlc-migration v1.3): BuildStore +
// ReleaseStore field types use the db.* interfaces so the sqlc-backed
// wrappers (BuildStoreSqlc, ReleaseStoreSqlc) drop in transparently. The
// rest of StoreDeps already mixes concrete + interface types (EventStore,
// SecretStore, BootstrapStore are interfaces; DomainStore + CellStore
// are still concrete pending their T1.x/T2.x sqlc migrations).
type StoreDeps struct {
	BuildStore     db.BuildStore
	ReleaseStore   db.ReleaseStore
	EnvTargetStore *db.PostgresEnvironmentTargetStore
	EventStore     sse.EventStore
	PgEventStore   *sse.PostgresEventStore
	SecretStore    routes.SecretStore
	BootstrapStore routes.BootstrapStore
	DomainStore    *db.PostgresDomainStore
	CellStore      *db.PostgresCellStore
	SourceStore    source.SourceStore
	ObjUploader    routes.ObjectUploader
	Locker         *adapters.PoolEnvironmentLocker

	// SSE adapter
	BuildStoreReader sse.BuildStoreReader
}

// setupStores creates all data stores from the database adapter and config.
func setupStores(dbAdapter *db.PgxAdapter, cfg config.Config, logger *slog.Logger) (*StoreDeps, error) {
	// Sprint 2 stores
	// T1.2b (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper.
	// Satisfies db.BuildStore interface 1:1 — service field types
	// unchanged. State-machine SafeTransition + concurrent-deploy
	// rejection (Rule 6 + ARCH-002) preserved at store layer (D4).
	buildStore := db.NewBuildStoreSqlc(dbAdapter)
	// T1.1c2c (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper.
	// Satisfies db.ReleaseStore interface 1:1 — service field types
	// unchanged. State-machine SafeTransition runs at store layer (D4).
	releaseStore := db.NewReleaseStoreSqlc(dbAdapter)
	envTargetStore := db.NewPostgresEnvironmentTargetStore(dbAdapter, dbAdapter)
	pgEventStore := sse.NewPostgresEventStore(dbAdapter, dbAdapter)

	// Secret store — SEC-001: encryption at rest when key is configured.
	secretStore, err := setupSecretStore(dbAdapter, cfg.SecretEncryptionKey, logger)
	if err != nil {
		return nil, err
	}

	// Source store — Gitea source repo
	giteaSourceStore, err := source.NewGiteaSourceStore(source.GiteaSourceStoreConfig{
		BaseURL: cfg.GiteaURL,
		Token:   cfg.GiteaToken,
		OrgName: cfg.GiteaOrg, // B77: was hardcoded "loopzero"
	})
	if err != nil {
		return nil, err
	}

	// S3 uploader for source bundles — build cluster reads from Spaces.
	var objUploader routes.ObjectUploader
	if cfg.MinIOEndpoint != "" && cfg.MinIOAccessKey != "" && cfg.MinIOSecretKey != "" {
		objUploader = s3uploader.New(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey)
		logger.Info("s3 uploader enabled", "endpoint", cfg.MinIOEndpoint)
	}

	return &StoreDeps{
		BuildStore:       buildStore,
		ReleaseStore:     releaseStore,
		EnvTargetStore:   envTargetStore,
		EventStore:       pgEventStore,
		PgEventStore:     pgEventStore,
		SecretStore:      secretStore,
		BootstrapStore:   &db.PostgresBootstrapStore{DB: dbAdapter},
		DomainStore:      &db.PostgresDomainStore{DB: dbAdapter},
		CellStore:        &db.PostgresCellStore{DB: dbAdapter},
		SourceStore:      giteaSourceStore,
		ObjUploader:      objUploader,
		Locker:           &adapters.PoolEnvironmentLocker{Pool: dbAdapter.Pool},
		BuildStoreReader: &adapters.BuildStoreReaderAdapter{Store: buildStore},
	}, nil
}

// setupSecretStore creates the encrypted secret store.
func setupSecretStore(dbAdapter *db.PgxAdapter, encKeyHex string, logger *slog.Logger) (routes.SecretStore, error) {
	var encKey []byte
	if encKeyHex != "" {
		var err error
		encKey, err = hex.DecodeString(encKeyHex)
		if err != nil {
			return nil, err
		}
	}
	return db.NewPostgresSecretStore(dbAdapter, encKey)
}
