// Package operatorboot holds what runs at the START of an operator binary,
// before the manager begins.
//
// It is small on purpose (B-061). The four binaries share it, so it has to be
// a package; and what it holds is the only thing that runs before the
// reconcile loop exists. The alternative — returning it to the reconcile
// package — is exactly the mixing that item undid.
package operatorboot

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"
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
	// Validated at the boundary, not trusted past it — an env var is an input
	// like any other. A malformed value would otherwise be logged verbatim and
	// then handed to net.Listen, and starting a debug server on an address
	// nobody meant is worse than not starting one.
	host, port, err := net.SplitHostPort(pprofAddr)
	if err != nil {
		slog.Error("pprof server NOT started: LOOPZERO_PPROF_ADDR is not a valid host:port", "error", err)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	//nolint:gosec // G706 false positive: host/port are the components
	// net.SplitHostPort ACCEPTED, so nothing unvalidated reaches the log line.
	// gosec's taint analysis propagates through any function's return values and
	// has no notion of validation — logging the raw env var was tried first, then
	// the parsed components; both flag identically.
	slog.Info("pprof server starting", "host", host, "port", port)
	// G114 mitigation: explicit Server with ReadHeaderTimeout so a
	// slowloris-style client cannot hold a connection idle in the
	// header-read phase. WriteTimeout deliberately ZERO because
	// `/debug/pprof/profile` runs a 30-60s CPU profile by design;
	// setting a finite WriteTimeout would truncate legitimate
	// profile collection.
	srv := &http.Server{
		Addr:              pprofAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("pprof server failed", "error", err)
		}
	}()
}
