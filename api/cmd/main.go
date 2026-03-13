// Package main is the entrypoint for the LoopZero API server.
// Sprint 0: skeleton only — validates configuration and exits.
package main

import (
	"fmt"
	"os"

	"github.com/loopzero/loopzero/pkg/logging"

	"github.com/loopzero/loopzero/api/internal/db"
)

func main() {
	logger := logging.NewLogger("api")

	cfg := db.GetDatabaseConfig()
	logger.Info("loopzero api starting",
		"host", cfg.Host,
		"port", cfg.Port,
		"database", cfg.Database,
	)

	fmt.Fprintf(os.Stderr, "loopzero api: server not implemented yet (Sprint 1)\n")
	os.Exit(0)
}
