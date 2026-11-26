// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

// startPprofServer serves the pprof endpoints on cfg.PprofAddr when
// cfg.PprofEnabled, on a dedicated mux so nothing registered on
// DefaultServeMux is exposed. Reachable by port-forward, never by ingress.
func startPprofServer(cfg config.Config, logger *slog.Logger) {
	if !cfg.PprofEnabled {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	// ReadHeaderTimeout blocks slowloris (G114). WriteTimeout stays zero on
	// purpose: /debug/pprof/profile runs 30-60s CPU profiles.
	srv := &http.Server{Addr: cfg.PprofAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		logger.Info("pprof server starting", "addr", cfg.PprofAddr)
		if err := srv.ListenAndServe(); err != nil {
			logger.Error("pprof server failed", "error", err)
		}
	}()
}
