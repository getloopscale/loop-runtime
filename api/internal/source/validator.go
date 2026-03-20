package source

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// LoopZeroYAML represents the parsed loopzero.yaml configuration.
type LoopZeroYAML struct {
	Name    string                `yaml:"name"`
	Project string                `yaml:"project"`
	Apps    map[string]AppConfig  `yaml:"apps"`
}

// AppConfig represents a single app definition in loopzero.yaml.
type AppConfig struct {
	Type      string `yaml:"type"`
	Port      int    `yaml:"port"`
	Path      string `yaml:"path"`
	Framework string `yaml:"framework"`
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
	}

	appNames := make([]string, 0, len(cfg.Apps))
	for name := range cfg.Apps {
		appNames = append(appNames, name)
	}

	return &cfg, appNames, nil
}
