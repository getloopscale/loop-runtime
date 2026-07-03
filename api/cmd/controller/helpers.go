// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/k8s"
	"github.com/getloopscale/loopzero/api/internal/leader"
	"github.com/getloopscale/loopzero/pkg/correlation"

	"k8s.io/klog/v2"
)

// healthPort is the HTTP port for /healthz and /metrics. Kept off 8080
// so it never collides with the API binary when both happen to run on
// the same node during a rolling update.
const healthPort = ":9091"

// cellID returns the cell to which this controller instance is scoped. It
// reads LOOPZERO_CELL_ID; if unset, falls back to the roadmap 03 default so a
// single-cell deployment works out of the box.
func cellID(_ config.Config) string {
	if v := os.Getenv("LOOPZERO_CELL_ID"); v != "" {
		return v
	}
	return db.CellDefault
}

// podName returns the holder identifier for the leader lease. Prefers
// HOSTNAME (which Kubernetes sets to the pod name) and falls back to
// "loopzero-controller" to remain usable outside Kubernetes.
func podName() string {
	if n := os.Getenv("HOSTNAME"); n != "" {
		return n
	}
	return "loopzero-controller"
}

// silenceKlog redirects the klog output from the embedded Kubernetes client
// libraries away from stderr so it does not break structured JSON logging.
func silenceKlog() {
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)
	_ = klogFlags.Set("logtostderr", "false")
	_ = klogFlags.Set("alsologtostderr", "false")
	_ = klogFlags.Set("stderrthreshold", "FATAL")
}

// appCRAdapter bridges k8s.ApplicationCRManager to build.ApplicationCREnsurer.
// Duplicated from api/cmd/main.go — the two binaries intentionally do not
// share a package here because cmd/main.go has HTTP-specific helpers the
// controller should never import.
type appCRAdapter struct {
	mgr *k8s.ApplicationCRManager
}

func (a *appCRAdapter) EnsureLoopZeroApplication(ctx context.Context, input build.ApplicationCRInput) error {
	// Audit gap M08 #10 / §6.C.1: propagate correlation_id from ctx into
	// the CR annotation. For controller-binary callers (post-build reconcile
	// loops) the ctx typically does NOT carry a correlation ID — those are
	// reconcile-internal flows. correlation.IDFromContext returns empty
	// string in that case, which LoopZeroApplicationInput interprets as "omit
	// the annotation" rather than "store empty value".
	return a.mgr.EnsureLoopZeroApplication(ctx, k8s.LoopZeroApplicationInput{
		TenantID:      input.TenantID,
		Project:       input.Project,
		Environment:   input.Environment,
		AppName:       input.AppName,
		ServiceName:   input.ServiceName,
		ServicePort:   input.ServicePort,
		CorrelationID: correlation.IDFromContext(ctx),
	})
}

// newHealthServer returns a minimal HTTP server exposing /healthz (liveness),
// /readyz (readiness — reports lease state), and /metrics (Prometheus).
// It listens on healthPort.
func newHealthServer(logger *slog.Logger, acquirer *leader.Acquirer, lease *leader.Lease) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Readiness reflects lease health: if we lost the lease, we are
		// not ready to handle work until re-elected.
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := acquirer.Validate(ctx, lease); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "fenced",
				"error":  err.Error(),
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"generation": lease.GenerationID,
			"holder":     lease.Holder,
			"name":       lease.Name,
			"cell_id":    lease.CellID,
		})
	})

	mux.Handle("/metrics", promhttp.Handler())

	return &http.Server{
		Addr:              healthPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
