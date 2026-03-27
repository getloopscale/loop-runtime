// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

var envKeyRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// LoopZeroYAML represents the parsed loopzero.yaml configuration.
type LoopZeroYAML struct {
	Version int                   `yaml:"version,omitempty"` // v2: schema version (current: 1)
	Name    string                `yaml:"name"`
	Project string                `yaml:"project"`
	Apps    map[string]AppConfig  `yaml:"apps"`
	Ignore  []string              `yaml:"ignore,omitempty"` // v2: glob patterns of non-deployable dirs
}

// AppConfig represents a single app definition in loopzero.yaml.
type AppConfig struct {
	Type       string            `yaml:"type"`
	Port       int               `yaml:"port"`
	Path       string            `yaml:"path"`
	Framework  string            `yaml:"framework"`
	Build      string            `yaml:"build,omitempty"`      // v2: "dockerfile", "buildpack", or "custom"
	Dockerfile string            `yaml:"dockerfile,omitempty"` // v2: Dockerfile path relative to app path
	Render     string            `yaml:"render,omitempty"`     // v2: "static" or "server" (MEET-009)
	Env        map[string]string `yaml:"env,omitempty"`        // v2: build-time environment variables
	Secrets    []string          `yaml:"secrets,omitempty"`    // secret names declared for this app
}

// ValidationError holds field-specific validation errors for loopzero.yaml.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateLoopZeroYAML parses and validates a loopzero.yaml content string.
// Returns the parsed config and a list of app names, or a validation error.
func ValidateLoopZeroYAML(content string) (*LoopZeroYAML, []string, error) {
	if content == "" {
		return nil, nil, &ValidationError{
			Field:   "loopzero_yaml",
			Message: "loopzero.yaml is required",
		}
	}

	var cfg LoopZeroYAML
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, nil, &ValidationError{
			Field:   "loopzero_yaml",
			Message: fmt.Sprintf("invalid YAML: %s", err.Error()),
		}
	}

	// Accept both "name" and "project" fields (CLI generates "project:")
	if cfg.Name == "" && cfg.Project != "" {
		cfg.Name = cfg.Project
	}
	if cfg.Name == "" {
		return nil, nil, &ValidationError{
			Field:   "name",
			Message: "name is required in loopzero.yaml",
		}
	}

	if len(cfg.Apps) == 0 {
		return nil, nil, &ValidationError{
			Field:   "apps",
			Message: "No apps defined in loopzero.yaml",
		}
	}

	// Validate version if present (v2 schema — Sprint 10)
	if cfg.Version != 0 && cfg.Version != 1 && cfg.Version != 2 {
		return nil, nil, &ValidationError{
			Field:   "version",
			Message: fmt.Sprintf("unsupported version %d (current: 2)", cfg.Version),
		}
	}

	// Validate each app
	for appName, app := range cfg.Apps {
		appType := app.Type

		// If type is "server" or unspecified (defaults to server), port is required
		switch appType {
		case "worker", "frontend":
			// No port required for worker or frontend types
		case "server", "":
			if app.Port <= 0 {
				if appType == "" {
					return nil, nil, &ValidationError{
						Field:   fmt.Sprintf("apps.%s", appName),
						Message: fmt.Sprintf("app '%s' must have a port (number > 0) or specify type as 'worker' or 'frontend'", appName),
					}
				}
				return nil, nil, &ValidationError{
					Field:   fmt.Sprintf("apps.%s.port", appName),
					Message: fmt.Sprintf("app '%s' with type 'server' must have a port (number > 0)", appName),
				}
			}
		default:
			return nil, nil, &ValidationError{
				Field:   fmt.Sprintf("apps.%s.type", appName),
				Message: fmt.Sprintf("app '%s' has invalid type '%s'; must be 'server', 'worker', or 'frontend'", appName, appType),
			}
		}

		// v2: Validate build strategy
		if app.Build != "" {
			switch app.Build {
			case "dockerfile", "buildpack", "custom":
				// valid
			default:
				return nil, nil, &ValidationError{
					Field:   fmt.Sprintf("apps.%s.build", appName),
					Message: fmt.Sprintf("app '%s' has invalid build strategy '%s'; must be 'dockerfile', 'buildpack', or 'custom'", appName, app.Build),
				}
			}
		}

		// v2: Validate env keys match ^[A-Z_][A-Z0-9_]*$
		for key := range app.Env {
			if !envKeyRegex.MatchString(key) {
				return nil, nil, &ValidationError{
					Field:   fmt.Sprintf("apps.%s.env.%s", appName, key),
					Message: fmt.Sprintf("env key '%s' must match ^[A-Z_][A-Z0-9_]*$ (uppercase with underscores)", key),
				}
			}
		}
	}

	appNames := make([]string, 0, len(cfg.Apps))
	for name := range cfg.Apps {
		appNames = append(appNames, name)
	}

	return &cfg, appNames, nil
}
