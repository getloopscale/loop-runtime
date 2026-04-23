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
type StoreDeps struct {
	BuildStore     *db.PostgresBuildStore
	ReleaseStore   *db.PostgresReleaseStore
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
	buildStore := db.NewPostgresBuildStore(dbAdapter)
	releaseStore := db.NewPostgresReleaseStore(dbAdapter)
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
		OrgName: "loopzero",
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
