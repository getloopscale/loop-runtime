// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package constants

import "time"

// --- Requeue Delays ---
// Controllers use RequeueAfter to poll for completion of async operations.
// These durations define how often the controller retries after a non-error wait.

const (
	// RequeueDeletionWait is the delay between checks when waiting for child
	// resources to be deleted (Tenant → Environments, Application → ExposedRoutes).
	RequeueDeletionWait = 5 * time.Second

	// RequeueNamespaceDeletionWait is the delay between checks when waiting
	// for a namespace to be fully deleted on the runtime cluster.
	RequeueNamespaceDeletionWait = 5 * time.Second

	// RequeueProbeInterval is the interval between ExposedRoute reachability probes
	// when the route is not yet stable (pending, cert-issued, or recovering from failure).
	RequeueProbeInterval = 30 * time.Second

	// RequeueProbeStableInterval is the interval between probes when the route
	// is already Reachable and stable. Reduces reconcile load: 200 routes at 30s
	// = 400 reconciles/min vs 200 routes at 5min = 40 reconciles/min.
	RequeueProbeStableInterval = 5 * time.Minute

	// RequeueRepoBindingInterval is the periodic recheck interval for GitRepositoryBinding.
	RequeueRepoBindingInterval = 5 * time.Minute

	// RequeueRepoBindingOnError is the retry delay for GitRepositoryBinding after an error.
	RequeueRepoBindingOnError = 30 * time.Second

	// RequeueSecretNotFound is the retry delay when a referenced secret is not yet available.
	RequeueSecretNotFound = 30 * time.Second

	// RequeuePeriodicCheck is the default periodic recheck interval for ClusterCredential connectivity.
	RequeuePeriodicCheck = 5 * time.Minute

	// RequeuePlatformBootstrapDelay is the delay between PlatformBootstrap retry attempts.
	RequeuePlatformBootstrapDelay = 5 * time.Second
)

// --- HTTP Client Timeouts ---
// Every HTTP client created in operator code MUST use one of these constants.
// No inline time.Duration for HTTP timeouts.

const (
	// HTTPTimeoutDefault is the default timeout for HTTP calls to internal services
	// (Gitea, ArgoCD, API). Used by shared httpClient in cmd/main.go.
	HTTPTimeoutDefault = 30 * time.Second

	// HTTPTimeoutExternal is the timeout for calls to external APIs (DigitalOcean).
	HTTPTimeoutExternal = 10 * time.Second

	// HTTPTimeoutMinIO is the timeout for MinIO Admin API calls.
	// Slightly shorter than default because MinIO is expected to be local.
	HTTPTimeoutMinIO = 15 * time.Second

	// ProbeTimeout is the HTTP client timeout for reachability probes.
	ProbeTimeout = 5 * time.Second

	// ReachabilityTimeout is the timeout for repository reachability checks.
	ReachabilityTimeout = 10 * time.Second

	// ConnectivityTimeout is the timeout for cluster connectivity checks (healthz).
	ConnectivityTimeout = 5 * time.Second
)

// --- Retry Config ---

const (
	// DefaultMaxRetries is the fallback for PlatformBootstrap when spec.maxRetries is unset.
	DefaultMaxRetries = 10
)

// --- DO Registry Credential Expiry ---

const (
	// DORegistryTokenExpirySeconds is the TTL for DigitalOcean registry tokens (7 days).
	DORegistryTokenExpirySeconds = 604800

	// DORegistryTokenRefreshBuffer is the minimum remaining validity before refreshing
	// a cached DO registry token. Tokens are refreshed when less than 1 day remains
	// out of the 7-day TTL, meaning we call the DO API ~once per 6 days per tenant
	// instead of on every reconcile.
	DORegistryTokenRefreshBuffer = 24 * time.Hour
)

// --- External API URLs ---
// URLs for external provider APIs. These are compile-time constants because
// the API endpoint is dictated by the provider, not by our deployment.

const (
	// DORegistryCredentialsURL is the DigitalOcean API endpoint for generating
	// Docker registry credentials. The expiry_seconds parameter is appended dynamically.
	DORegistryCredentialsURL = "https://api.digitalocean.com/v2/registry/docker-credentials"
)
