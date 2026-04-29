// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shared

import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
)

// PprofEnabled checks LOOPZERO_PPROF_ENABLED. Default: false (safe for production).
// Set LOOPZERO_PPROF_ENABLED=true explicitly to enable (dev/debug only).
func PprofEnabled() bool {
	v := os.Getenv("LOOPZERO_PPROF_ENABLED")
	return v == "true" || v == "1"
}

// StartPprofServer starts the pprof HTTP server on the given address.
// Uses a dedicated ServeMux (never http.DefaultServeMux). Runs in a goroutine.
func StartPprofServer() {
	pprofAddr := os.Getenv("LOOPZERO_PPROF_ADDR")
	if pprofAddr == "" {
		pprofAddr = ":6061"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	slog.Info("pprof server starting", "addr", pprofAddr)
	go func() {
		if err := http.ListenAndServe(pprofAddr, mux); err != nil {
			slog.Error("pprof server failed", "error", err)
		}
	}()
}
