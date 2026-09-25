// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"time"
)

// Host is everything a seed reads from outside the cluster: the process
// environment, local tools (docker, helm, gh), HTTP and Postgres. It lives on
// Deps, so a test builds the Host it needs instead of swapping package-level
// variables — which made the seed tests share mutable state and forbade
// t.Parallel.
//
// Every field is optional: a nil field means the real implementation, so the
// composition root and most tests leave the Host zero.
type Host struct {
	// Getenv reads the process environment.
	Getenv func(string) string
	// DevHTTP talks to dev-cluster endpoints behind self-signed certificates
	// (Kratos admin, the cloud webhook). It never reaches a third party.
	DevHTTP *http.Client
	// StripeHTTP talks to the Stripe API, with certificate verification.
	StripeHTTP *http.Client
	// PGConnect opens a Postgres connection.
	PGConnect func(ctx context.Context, connString string) (pgConn, error)
	// DockerNetworkIP returns a container's IPv4 address inside a docker network.
	DockerNetworkIP func(network, container string) (string, error)
	// DockerGateway returns the IPv4 gateway of a docker network.
	DockerGateway func(network string) (string, error)
	// DockerContainerIP returns a container's first IP address.
	DockerContainerIP func(container string) (string, error)
	// HelmUpgrade installs or upgrades the broker chart.
	HelmUpgrade func(chartPath, apiURL string) error
	// GHAPI runs `gh api` with args, sending body (when non-nil) on stdin.
	GHAPI func(body []byte, args ...string) ([]byte, error)
	// GHAuthCheck reports whether gh is authenticated.
	GHAuthCheck func() error
}

// devHTTPDefault skips certificate verification because the dev cluster's
// certificates are self-signed; it is only used against dev endpoints.
var devHTTPDefault = &http.Client{
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // #nosec G402 -- self-signed dev certificates
	Timeout:   30 * time.Second,
}

// stripeHTTPDefault bounds every Stripe API call; http.DefaultClient has no
// timeout, so a stalled connection would hang the seed indefinitely.
var stripeHTTPDefault = &http.Client{Timeout: 30 * time.Second}

func (h Host) getenv(key string) string {
	if h.Getenv != nil {
		return h.Getenv(key)
	}
	return os.Getenv(key)
}

// envOr returns the environment value for key, or def when it is empty.
func (h Host) envOr(key, def string) string {
	if v := h.getenv(key); v != "" {
		return v
	}
	return def
}

func (h Host) devHTTP() *http.Client {
	if h.DevHTTP != nil {
		return h.DevHTTP
	}
	return devHTTPDefault
}

func (h Host) stripeHTTP() *http.Client {
	if h.StripeHTTP != nil {
		return h.StripeHTTP
	}
	return stripeHTTPDefault
}

func (h Host) pgConnect(ctx context.Context, connString string) (pgConn, error) {
	if h.PGConnect != nil {
		return h.PGConnect(ctx, connString)
	}
	return pgxConnect(ctx, connString)
}

func (h Host) dockerNetworkIP(network, container string) (string, error) {
	if h.DockerNetworkIP != nil {
		return h.DockerNetworkIP(network, container)
	}
	return defaultDockerNetworkInspect(network, container)
}

func (h Host) dockerGateway(network string) (string, error) {
	if h.DockerGateway != nil {
		return h.DockerGateway(network)
	}
	return defaultDockerNetworkGateway(network)
}

func (h Host) dockerContainerIP(container string) (string, error) {
	if h.DockerContainerIP != nil {
		return h.DockerContainerIP(container)
	}
	return defaultDockerInspect(container)
}

func (h Host) helmUpgrade(chartPath, apiURL string) error {
	if h.HelmUpgrade != nil {
		return h.HelmUpgrade(chartPath, apiURL)
	}
	return defaultHelmUpgrade(chartPath, apiURL)
}

func (h Host) ghAPI(body []byte, args ...string) ([]byte, error) {
	if h.GHAPI != nil {
		return h.GHAPI(body, args...)
	}
	return defaultGhAPI(body, args...)
}

func (h Host) ghAuthCheck() error {
	if h.GHAuthCheck != nil {
		return h.GHAuthCheck()
	}
	return defaultGhAuthCheck()
}
