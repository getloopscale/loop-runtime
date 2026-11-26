// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cellrouting

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewClientWithRetry wraps client.New with exponential backoff retry.
// Handles transient connectivity issues during cluster startup ordering
// (ARCH_OP_003). Config parsing is not retried — only the client connection.
func NewClientWithRetry(log logr.Logger, cfg *rest.Config, opts client.Options, clusterName string) (client.Client, error) {
	const maxAttempts = 3
	backoff := 2 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		c, err := client.New(cfg, opts)
		if err == nil {
			return c, nil
		}
		lastErr = err
		if attempt < maxAttempts {
			log.V(0).Info("retrying cluster client creation",
				"cluster", clusterName,
				"attempt", attempt,
				"nextRetryIn", backoff.String(),
				"error", err,
			)
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}

// CreateClusterClient reads a kubeconfig path and creates a K8s client with retry.
func CreateClusterClient(log logr.Logger, kubeconfigPath string, scheme *runtime.Scheme, clusterName string) (client.Client, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("unable to create %s cluster config from %s: %w", clusterName, kubeconfigPath, err)
	}
	c, err := NewClientWithRetry(log, cfg, client.Options{Scheme: scheme}, clusterName)
	if err != nil {
		return nil, fmt.Errorf("unable to create %s cluster client after retries: %w", clusterName, err)
	}
	log.Info(clusterName+" cluster client created", "kubeconfig", kubeconfigPath)
	return c, nil
}
