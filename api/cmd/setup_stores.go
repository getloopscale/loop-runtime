// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/adapters"
	"github.com/getloopscale/loopzero/api/internal/buildplane/source"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/objectstore"
	sourceroutes "github.com/getloopscale/loopzero/api/internal/routes/source"
	"github.com/getloopscale/loopzero/api/internal/secrets/secretshttp"
	"github.com/getloopscale/loopzero/api/internal/sse"
	"github.com/getloopscale/loopzero/pkg/vault"
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
	BuildStore       db.BuildStore
	ReleaseStore     db.ReleaseStore
	EnvTargetStore   *store.PostgresEnvironmentTargetStore
	PreviewSlotStore *store.PostgresPreviewSlotStore // 2026-06-07 D2 — single-slot preview
	EventStore       sse.EventStore
	PgEventStore     *store.PostgresEventStore
	SecretStore      secretshttp.SecretStore
	// PgSecretStore is the same store as SecretStore, typed concretely for the
	// two consumers that need more than the route port: the deploy-time
	// resyncer (ListForResync) and the ESO broker (ReadValue).
	PgSecretStore  *store.PostgresSecretStore
	BootstrapStore ports.BootstrapStore
	DomainStore    *store.PostgresDomainStore
	CellStore      *store.PostgresCellStore
	SourceStore    source.SourceStore
	ObjUploader    sourceroutes.ObjectUploader
	Locker         *adapters.PoolEnvironmentLocker

	// SSE adapter
	BuildStoreReader sse.BuildStoreReader
}

// setupStores creates all data stores from the database adapter and config.
//
// T2.5 wire-up (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2): the
// transit parameter is threaded into setupSecretStore so the
// PostgresSecretStore boots with vault-backed encrypt/decrypt dispatch.
// nil-safe: when transit is nil (CryptoMode != "transit"), the store
// falls back to the legacy AES path. The fail-loud contract lives in
// the caller (main.go): if config demanded transit and constructor
// failed, the process exits BEFORE setupStores ever runs.
func setupStores(dbAdapter *db.PgxAdapter, cfg config.Config, transit vault.Transit, logger *slog.Logger) (*StoreDeps, error) {
	// Sprint 2 stores
	// T1.2b (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper.
	// Satisfies db.BuildStore interface 1:1 — service field types
	// unchanged. State-machine SafeTransition + concurrent-deploy
	// rejection (Rule 6 + ARCH-002) preserved at store layer (D4).
	buildStore := store.NewBuildStoreSqlc(dbAdapter)
	// T1.3.b of plan 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1:
	// EventStore must be constructed BEFORE releaseStore so AppendSideEffect
	// can emit signal.changed events via canonical SSE infrastructure.
	pgEventStore := store.NewPostgresEventStore(dbAdapter, dbAdapter)
	// T1.1c2c (plan 2026-05-29-sqlc-migration v1.3): sqlc-backed wrapper.
	// Satisfies db.ReleaseStore interface 1:1 — service field types
	// unchanged. State-machine SafeTransition runs at store layer (D4).
	// T1.3.b v1.1: 2nd param eventStore required non-nil (EC-1 absorption);
	// panics on nil to surface mis-wired startups loud + fast.
	releaseStore := store.NewReleaseStoreSqlc(dbAdapter, pgEventStore)
	envTargetStore := store.NewPostgresEnvironmentTargetStore(dbAdapter, dbAdapter)
	// 2026-06-07 D2 — single-slot preview store (Netlify-style).
	// Phase 1 of plan 2026-06-07-preview-as-category-single-slot-plan.md.
	previewSlotStore := store.NewPostgresPreviewSlotStore(dbAdapter, dbAdapter)

	// Secret store — SEC-001: encryption at rest when key is configured.
	// T2.5 wire-up (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2):
	// transit is non-nil only when cfg.CryptoMode()=="transit"; routes
	// the store to NewPostgresSecretStoreWithVault for dual-path
	// (vault:v1: prefix → transit decrypt; else → AES legacy).
	secretStore, err := setupSecretStore(dbAdapter, cfg.SecretEncryptionKey, transit, logger)
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
	var objUploader sourceroutes.ObjectUploader
	if cfg.MinIOEndpoint != "" && cfg.MinIOAccessKey != "" && cfg.MinIOSecretKey != "" {
		objStore, err := objectstore.NewS3Store(cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey)
		if err != nil {
			return nil, fmt.Errorf("object store: %w", err)
		}
		objUploader = objStore
		logger.Info("s3 uploader enabled", "endpoint", cfg.MinIOEndpoint)
	}

	return &StoreDeps{
		BuildStore:       buildStore,
		ReleaseStore:     releaseStore,
		EnvTargetStore:   envTargetStore,
		PreviewSlotStore: previewSlotStore,
		EventStore:       pgEventStore,
		PgEventStore:     pgEventStore,
		SecretStore:      secretStore,
		PgSecretStore:    secretStore,
		BootstrapStore:   &store.PostgresBootstrapStore{DB: dbAdapter},
		DomainStore:      &store.PostgresDomainStore{DB: dbAdapter},
		CellStore:        &store.PostgresCellStore{DB: dbAdapter},
		SourceStore:      giteaSourceStore,
		ObjUploader:      objUploader,
		Locker:           &adapters.PoolEnvironmentLocker{Pool: dbAdapter.Pool},
		BuildStoreReader: &adapters.BuildStoreReaderAdapter{Store: buildStore},
	}, nil
}

// setupSecretStore creates the encrypted secret store.
//
// T2.5 wire-up (plan 2026-06-10-j22-openbao-transit-eso-sync v1.2): when
// transit is non-nil, the store boots via NewPostgresSecretStoreWithVault
// so writes dispatch through the transit client's Encrypt (storing vault:v1:...
// prefixed bytes in encrypted_value) and reads dispatch via prefix
// detection in decryptFromStore (vault:v1: → transit decrypt; else →
// AES legacy). The legacy AES key is still passed through so existing
// pre-transit rows remain readable during the migration window
// (per the dual-read design in secret_wrapper.go decryptFromStore).
//
// nil-safe: transit==nil falls back to the legacy single-path AES store.
// Caller (setupStores) decides which path to take based on
// cfg.CryptoMode(); fail-loud refusal already fired upstream in
// config.Validate() so reaching this function with the wrong combination
// is a programming error, not a runtime error.
func setupSecretStore(dbAdapter db.DBTX, encKeyHex string, transit vault.Transit, logger *slog.Logger) (*store.PostgresSecretStore, error) {
	var encKey []byte
	if encKeyHex != "" {
		var err error
		encKey, err = hex.DecodeString(encKeyHex)
		if err != nil {
			return nil, err
		}
	}
	if transit != nil {
		// `transit` goes in directly: `vault.Transit` satisfies `store.VaultTransitClient`
		// structurally, and neither package knows the other. The bridge that used to sit
		// here only renamed methods (B-043, bullet 4).
		store, err := store.NewPostgresSecretStoreWithVault(dbAdapter, transit, encKey)
		if err != nil {
			return nil, err
		}
		logger.Info("secret store wired with openbao transit dispatch",
			"legacy_aes_key_present", len(encKey) > 0)
		return store, nil
	}
	// T10.3 (B-162): this store has no write path — encryptForStore refuses
	// without a Transit client. Say so at boot rather than at the first
	// `loopzero secret set`, which is where the operator would otherwise find out.
	logger.Warn("secret store wired without openbao transit — secret writes will be refused (T10.3); legacy AES rows remain readable",
		"legacy_aes_key_present", len(encKey) > 0)
	return store.NewPostgresSecretStore(dbAdapter, encKey)
}
