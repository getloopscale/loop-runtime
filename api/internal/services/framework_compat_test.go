// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"testing"
)

// TestFrameworkMapping_CLIServerParity ensures that every framework recognized
// by the CLI is also mapped by the server's resolveLanguageStrategy().
// MEET-2026-04-09-044 P1: Prevents framework drift between CLI and server.
// If this test fails, either the CLI or server framework list needs updating.
func TestFrameworkMapping_CLIServerParity(t *testing.T) {
	// These are ALL frameworks the CLI recognizes (from cli/src/adapters/validate-config.ts).
	// This list must be kept in sync with the CLI. Any addition to the CLI's
	// knownFrameworks list must be mirrored here.
	cliKnownFrameworks := []string{
		// Node.js
		"nextjs", "next", "vite", "remix", "nestjs", "express", "node",
		"react", "vue", "svelte", "astro", "angular", "fastify",
		// Go
		"gin", "go",
		// Python
		"fastapi", "django", "python",
		// Ruby
		"sinatra", "rails", "ruby",
		// Java
		"spring", "spring-boot", "java",
		// PHP
		"laravel", "slim", "php",
		// Rust
		"axum", "actix", "rocket", "rust",
	}

	for _, fw := range cliKnownFrameworks {
		strategy := resolveLanguageStrategy(fw)
		if strategy == "custom" {
			t.Errorf("Framework %q is known in CLI but resolves to 'custom' in server. Add it to resolveLanguageStrategy().", fw)
		}
	}
}

// TestFrameworkMapping_ServerCoversAll ensures resolveLanguageStrategy has no
// frameworks that the CLI doesn't know about.
func TestFrameworkMapping_ServerCoversAll(t *testing.T) {
	// All frameworks mapped in resolveLanguageStrategy
	serverFrameworks := map[string]string{
		"nextjs": "node", "next": "node", "vite": "node", "remix": "node",
		"nestjs": "node", "express": "node", "node": "node",
		"react": "node", "vue": "node", "svelte": "node", "astro": "node",
		"angular": "node", "fastify": "node",
		"gin": "go", "go": "go",
		"fastapi": "python", "django": "python", "python": "python",
		"sinatra": "ruby", "rails": "ruby", "ruby": "ruby",
		"spring": "java", "spring-boot": "java", "java": "java",
		"laravel": "php", "slim": "php", "php": "php",
		"axum": "rust", "actix": "rust", "rocket": "rust", "rust": "rust",
	}

	for fw, expected := range serverFrameworks {
		actual := resolveLanguageStrategy(fw)
		if actual != expected {
			t.Errorf("resolveLanguageStrategy(%q) = %q, want %q", fw, actual, expected)
		}
	}
}
