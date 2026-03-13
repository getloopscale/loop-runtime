// Package main is the entrypoint for the LoopZero API server.
package main

import (
	"os"

	"github.com/loopzero/loopzero/pkg/logging"

	"github.com/loopzero/loopzero/api/internal/server"
)

func main() {
	logger := logging.NewLogger("api")

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	cfg := server.Config{
		Port:             port,
		Logger:           logger,
		DB:               nil, // TODO: wire pgx connection pool
		TokenValidator:   nil, // TODO: wire GitHub token validator
		MemberStore:      nil, // TODO: wire DB membership store
		ProjectStore:     nil, // TODO: wire DB project store
		TenantCRMgr:      nil, // TODO: wire Kubernetes Tenant CR manager
		IdempotencyStore: nil, // TODO: wire DB idempotency store
	}

	if err := server.Run(cfg); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
