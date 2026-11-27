// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewMetricsServer creates a separate HTTP server for Prometheus metrics.
// This keeps /metrics off the main API port so it is not publicly accessible.
//
// SECURITY: The /metrics endpoint has no authentication. In Kubernetes, expose
// via ClusterIP Service only (not NodePort/LoadBalancer). For additional
// protection, use a NetworkPolicy to restrict access to the monitoring
// namespace (see infra/helm E-11 NetworkPolicy definitions).
func NewMetricsServer(port string) *http.Server {
	if port == "" {
		port = "9090"
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// ValidateProductionEnv checks that all security-sensitive environment variables
// are set. Returns an error listing missing variables. Intended for use when
// LOOP_RUNTIME_ENV=production — in dev mode, fallback defaults are acceptable.
func ValidateProductionEnv(logger *slog.Logger, vars map[string]string) error {
	var missing []string
	for name, val := range vars {
		if val == "" {
			missing = append(missing, name)
			logger.Error("required environment variable not set", "var", name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %v", missing)
	}
	return nil
}
