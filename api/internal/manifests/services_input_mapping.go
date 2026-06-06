// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Plan B v3.1 T-B.4 (2026-06-05) — services.json → RenderInput mapping.
//
// Adapts loopzerokit-emitted `.loopzero/services.json` to the existing `RenderInput`
// shape consumed by the production K8s emitter (api/internal/manifests/
// renderer.go). Per ADR D1 + Sonnet R-1: ADAPT existing `manifests.Render()`,
// do NOT create a new emitter. This file provides:
//
//   1. The runtime → LanguageStrategy compat table (EC-B3-1) so the new
//      schema's flat `runtime: "python" | "node"` lifts into existing
//      strategy strings during the transition window per ADR D7.
//   2. `mapServicesToRenderInput()` — pure mapping function.
//   3. `RenderFromServicesManifest()` — orchestrator that wraps existing
//      `Render()` (no logic duplication).
//
// EC-B3-9 — preserves `services[]` array order from services.json
// (loopzerokit emits topologically sorted via Kahn's algorithm).
//
// EC-B3-13 — emits structured logs via slog at INFO (function bound +
// service count) and DEBUG (per-service mapping detail).

package manifests

import (
	"fmt"
	"log/slog"
	"strings"
)

// ServiceEntry mirrors loopzerokit's `ManifestServiceEntry` interface — kept
// local to avoid an import cycle through `internal/source`. Validators
// produce instances with the same field shape; this struct simply
// re-declares the wire contract at the mapping boundary.
//
// Plan v1.2 T2.2: `Type` enum added (server | worker | frontend). Defaults
// to ServiceTypeServer when empty (Go-side fallback).
type ServiceEntry struct {
	Name          string
	Runtime       string
	Type          string
	Port          int
	Proxy         string
	Dev           string
	Build         string
	Start         string
	OpenAPI       string
	Healthcheck   string
	CORS          bool
	Env           map[string]string
	DependsOn     []string
	PassSetCookie bool

	// Rendering bundles frontend-specific rendering options. EC-9 (v1.1):
	// when Type=ServiceTypeFrontend, Rendering MUST be populated or the
	// mapping returns an error.
	Rendering *FrontendRendering
}

// FrontendRendering describes how a frontend service is delivered. v4.1 ships
// the `static` path only — SSR / hybrid are reserved for later versions.
type FrontendRendering struct {
	Mode      string // "static" (v4.1) — required when Type=frontend
	AssetsDir string // optional override; defaults to "dist" downstream
}

// Service type enum values mirror services.schema.json.
const (
	ServiceTypeServer   = "server"
	ServiceTypeWorker   = "worker"
	ServiceTypeFrontend = "frontend"
)

// resolveServiceType applies the Go-side fallback to ServiceTypeServer when
// the wire field is empty (JSON Schema 7 does not apply `default`
// automatically).
func resolveServiceType(t string) string {
	if t == "" {
		return ServiceTypeServer
	}
	return t
}

// ServicesManifest mirrors loopzerokit's `ServicesManifest` interface.
type ServicesManifest struct {
	Version  int
	Services []ServiceEntry
}

// runtimeToStrategy maps services.json `runtime` field values to the
// existing LanguageStrategy strings consumed by argo/languages.go and
// the manifests emitter. Acts as a compat layer during the transition
// window per ADR D7. Future v3.x: pure runtime-based dispatch once
// legacy framework fallback is removed.
//
// EC-B3-1 (v3.1) — explicit mapping table (not heuristic).
var runtimeToStrategy = map[string]string{
	"python": "python-fastapi", // Wave 2 canonical Python strategy
	"node":   "node-hono",      // Wave 2 canonical Node strategy
}

// mapRuntimeToStrategy resolves a services.json runtime value to the
// LanguageStrategy string. Returns an error for unknown runtimes.
func mapRuntimeToStrategy(runtime string) (string, error) {
	strategy, ok := runtimeToStrategy[strings.ToLower(runtime)]
	if !ok {
		return "", fmt.Errorf("unknown runtime %q (expected one of: python, node)", runtime)
	}
	return strategy, nil
}

// mapServicesToRenderInput translates a `ServicesManifest` (the platform-
// neutral shape emitted by loopzerokit) into a `RenderInput` (the existing
// K8s emitter input). The caller supplies the deploy-context fields
// (TenantID, ProjectID, BaseDomain, etc.) via `opts` since they don't
// live inside services.json.
//
// EC-B3-9 — array order from services.json is preserved (loopzerokit
// guarantees Kahn topological order).
func mapServicesToRenderInput(svc ServicesManifest, opts RenderOpts) (RenderInput, error) {
	slog.Info("manifests.mapServicesToRenderInput: start",
		"version", svc.Version,
		"service_count", len(svc.Services))

	apps := make([]AppManifestInput, 0, len(svc.Services))
	for _, s := range svc.Services {
		strategy, err := mapRuntimeToStrategy(s.Runtime)
		if err != nil {
			return RenderInput{}, fmt.Errorf("service %q: %w", s.Name, err)
		}

		// Plan v1.2 T2.2 — propagate `type`; default to server when absent.
		// EC-9 (v1.1): frontend services without rendering metadata are
		// rejected with an actionable error so consumers can't silently
		// fall through to the server emit path.
		svcType := resolveServiceType(s.Type)
		if svcType == ServiceTypeFrontend && s.Rendering == nil {
			return RenderInput{}, fmt.Errorf(
				"service %q: frontend services require `rendering` metadata (v4.1: static path only)",
				s.Name,
			)
		}

		slog.Debug("manifests.mapServicesToRenderInput: service",
			"name", s.Name,
			"runtime", s.Runtime,
			"strategy", strategy,
			"port", s.Port,
			"type", svcType)

		apps = append(apps, AppManifestInput{
			Name:     s.Name,
			Type:     svcType,
			Port:     s.Port,
			ImageRef: opts.ImageRefFor(s.Name),
			Secrets:  nil, // Reserved for future services.json `secrets` field
			Env:      s.Env,
			Replicas: opts.DefaultReplicas,
		})
	}

	return RenderInput{
		TenantID:             opts.TenantID,
		ProjectID:            opts.ProjectID,
		Environment:          opts.Environment,
		BuildID:              opts.BuildID,
		ReleaseID:            opts.ReleaseID,
		BaseDomain:           opts.BaseDomain,
		S3Endpoint:           opts.S3Endpoint,
		S3BucketName:         opts.S3BucketName,
		Apps:                 apps,
		ClusterIssuerName:    opts.ClusterIssuerName,
		GatewayNamespace:     opts.GatewayNamespace,
		DefaultCPURequest:    opts.DefaultCPURequest,
		DefaultMemoryRequest: opts.DefaultMemoryRequest,
		DefaultCPULimit:      opts.DefaultCPULimit,
		DefaultMemoryLimit:   opts.DefaultMemoryLimit,
		Plan:                 opts.Plan,
		PrometheusAddress:    opts.PrometheusAddress,
		ProgressiveDelivery:  opts.ProgressiveDelivery,
	}, nil
}

// RenderOpts carries deploy-context fields that don't live inside the
// platform-neutral services.json payload — LoopZeroCloud-internal concerns
// (tenant, project, image refs, cluster config). Provides a single
// argument bag for `RenderFromServicesManifest()`.
type RenderOpts struct {
	TenantID     string
	ProjectID    string
	Environment  string
	BuildID      string
	ReleaseID    string
	BaseDomain   string
	S3Endpoint   string
	S3BucketName string

	// imageRefByName allows the caller to provide per-service image refs
	// (one per built image from the build plane). When set, takes
	// precedence over ImageRefDefault.
	ImageRefByName  map[string]string
	ImageRefDefault string

	// Replica/HPA defaults
	DefaultReplicas int

	// Cluster + render defaults — mirror the optional fields on RenderInput.
	ClusterIssuerName    string
	GatewayNamespace     string
	DefaultCPURequest    string
	DefaultMemoryRequest string
	DefaultCPULimit      string
	DefaultMemoryLimit   string

	// Canary / Rollout config
	Plan                string
	PrometheusAddress   string
	ProgressiveDelivery bool
}

// ImageRefFor returns the image ref for a given service name. Falls
// back to ImageRefDefault when no per-service ref is registered.
func (o RenderOpts) ImageRefFor(serviceName string) string {
	if ref, ok := o.ImageRefByName[serviceName]; ok {
		return ref
	}
	return o.ImageRefDefault
}

// RenderFromServicesManifest is the public entry point for the new
// services.json → K8s manifests path. Wraps existing `Render()` after
// mapping the platform-neutral payload into `RenderInput`. Per ADR D1:
// emission logic (Deployment, Service, IngressRoute, Rollout) is
// UNCHANGED — only the input shape is bridged.
func RenderFromServicesManifest(svc ServicesManifest, opts RenderOpts) (string, error) {
	input, err := mapServicesToRenderInput(svc, opts)
	if err != nil {
		return "", fmt.Errorf("services.json → RenderInput: %w", err)
	}
	return Render(input), nil
}

// RenderFromServicesManifestInput is the same bridge as
// RenderFromServicesManifest but stops at the `RenderInput` boundary so
// integration tests can inspect the structured value without invoking the
// emitter. Plan v1.2 T2.4.
func RenderFromServicesManifestInput(svc ServicesManifest, opts RenderOpts) (RenderInput, error) {
	return mapServicesToRenderInput(svc, opts)
}
