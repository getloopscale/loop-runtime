// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import "fmt"

// AppUnit is a fully resolved app — all fields required, ready for build plan generation.
// This is the normalization boundary: before AppUnit, data comes from parsing and heuristics.
// After AppUnit, everything is deterministic.
type AppUnit struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`                        // "server", "worker", "frontend"
	Path       string            `json:"path"`
	Framework  string            `json:"framework"`
	Port       int               `json:"port"`
	Build      string            `json:"build"`                       // "dockerfile", "buildpack", "custom"
	Dockerfile string            `json:"dockerfile,omitempty"`        // resolved Dockerfile path (relative to app path)
	Rendering  string            `json:"rendering,omitempty"`         // "static" or "server" (MEET-009)
	Env        map[string]string `json:"env,omitempty"`               // build-time environment variables
	Secrets    []string          `json:"secrets,omitempty"`
}

// MEET-2026-03-29-016: Remix removed — SSR-first framework, classified as server.
var frontendFrameworks = map[string]bool{
	"nextjs": true, "vite": true,
	"react": true, "vue": true, "svelte": true, "astro": true, "angular": true,
}

// NormalizeApp resolves a raw AppSpec into a fully populated AppUnit.
// Returns error if the combination is invalid (e.g., server without port).
func NormalizeApp(name string, raw AppSpec) (AppUnit, error) {
	// Resolve type
	appType := raw.Type
	if appType == "" {
		if frontendFrameworks[raw.Framework] {
			appType = "frontend"
		} else {
			appType = "server"
		}
	}
	if appType != "server" && appType != "worker" && appType != "frontend" {
		return AppUnit{}, fmt.Errorf("app '%s': invalid type '%s'", name, appType)
	}

	// Resolve build strategy
	build := raw.Build
	if build == "" {
		if raw.Framework == "custom" || raw.Framework == "" || raw.Framework == "unknown" {
			build = "dockerfile"
		} else {
			build = "buildpack"
		}
	}

	// Resolve dockerfile
	dockerfile := ""
	if build == "dockerfile" {
		dockerfile = raw.Dockerfile
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}
	}

	// Resolve port
	port := raw.Port
	if appType == "server" && port <= 0 {
		port = 8080
	}
	if appType == "frontend" && port <= 0 {
		port = 80 // default for frontend servers (nginx, static file server)
	}
	if appType == "worker" {
		port = 0
	}

	// Resolve path
	path := raw.Path
	if path == "" {
		path = "."
	}

	// Resolve framework
	framework := raw.Framework
	if framework == "" {
		framework = "custom"
	}

	env := raw.Env
	if env == nil {
		env = map[string]string{}
	}

	secrets := raw.Secrets
	if secrets == nil {
		secrets = []string{}
	}

	// Resolve rendering mode (MEET-2026-03-26-009, MEET-2026-03-29-016).
	rendering := raw.Rendering
	if rendering != "static" && rendering != "server" {
		rendering = "server"
	}
	// Non-frontend types are always "server" (container delivery).
	if appType != "frontend" {
		rendering = "server"
	}
	// MEET-2026-03-29-016: Frontend apps MUST use static delivery (zero pods, CDN).
	// No server-rendered frontends allowed. This is the authoritative enforcement point.
	if appType == "frontend" && rendering != "static" {
		return AppUnit{}, fmt.Errorf("app '%s': frontend apps must use static delivery (render: static). LoopZero serves frontends from CDN — server-side rendering is not supported. Add 'render: static' to your loopzero.yaml", name)
	}

	return AppUnit{
		Name:       name,
		Type:       appType,
		Path:       path,
		Framework:  framework,
		Port:       port,
		Build:      build,
		Dockerfile: dockerfile,
		Rendering:  rendering,
		Env:        env,
		Secrets:    secrets,
	}, nil
}
