package unit

import (
	"testing"

	"github.com/loopzero/loopzero/api/internal/source"
)

func TestValidateLoopZeroYAML_ValidConfig(t *testing.T) {
	// Arrange
	content := `
name: my-project
apps:
  api:
    type: server
    port: 8080
  worker:
    type: worker
`

	// Act
	cfg, appNames, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Name != "my-project" {
		t.Fatalf("expected name 'my-project', got '%s'", cfg.Name)
	}
	if len(appNames) != 2 {
		t.Fatalf("expected 2 app names, got %d", len(appNames))
	}
}

func TestValidateLoopZeroYAML_ProjectFieldAsName(t *testing.T) {
	// Arrange: uses "project" field instead of "name"
	content := `
project: my-project
apps:
  api:
    type: server
    port: 3000
`

	// Act
	cfg, _, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Name != "my-project" {
		t.Fatalf("expected name 'my-project' from project field, got '%s'", cfg.Name)
	}
}

func TestValidateLoopZeroYAML_EmptyContent(t *testing.T) {
	// Arrange
	content := ""

	// Act
	_, _, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
	valErr, ok := err.(*source.ValidationError)
	if !ok {
		t.Fatalf("expected *source.ValidationError, got %T", err)
	}
	if valErr.Field != "loopzero_yaml" {
		t.Fatalf("expected field 'loopzero_yaml', got '%s'", valErr.Field)
	}
}

func TestValidateLoopZeroYAML_InvalidYAMLSyntax(t *testing.T) {
	// Arrange
	content := `
name: my-project
apps:
  api:
    port: [invalid
`

	// Act
	_, _, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	valErr, ok := err.(*source.ValidationError)
	if !ok {
		t.Fatalf("expected *source.ValidationError, got %T", err)
	}
	if valErr.Field != "loopzero_yaml" {
		t.Fatalf("expected field 'loopzero_yaml', got '%s'", valErr.Field)
	}
}

func TestValidateLoopZeroYAML_MissingName(t *testing.T) {
	// Arrange: no name or project field
	content := `
apps:
  api:
    type: server
    port: 8080
`

	// Act
	_, _, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	valErr, ok := err.(*source.ValidationError)
	if !ok {
		t.Fatalf("expected *source.ValidationError, got %T", err)
	}
	if valErr.Field != "name" {
		t.Fatalf("expected field 'name', got '%s'", valErr.Field)
	}
}

func TestValidateLoopZeroYAML_NoApps(t *testing.T) {
	// Arrange: name present but no apps
	content := `
name: my-project
apps: {}
`

	// Act
	_, _, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty apps, got nil")
	}
	valErr, ok := err.(*source.ValidationError)
	if !ok {
		t.Fatalf("expected *source.ValidationError, got %T", err)
	}
	if valErr.Field != "apps" {
		t.Fatalf("expected field 'apps', got '%s'", valErr.Field)
	}
}

func TestValidateLoopZeroYAML_ServerWithoutPort(t *testing.T) {
	// Arrange: server type requires a port
	content := `
name: my-project
apps:
  api:
    type: server
`

	// Act
	_, _, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err == nil {
		t.Fatal("expected error for server without port, got nil")
	}
	valErr, ok := err.(*source.ValidationError)
	if !ok {
		t.Fatalf("expected *source.ValidationError, got %T", err)
	}
	if valErr.Field != "apps.api.port" {
		t.Fatalf("expected field 'apps.api.port', got '%s'", valErr.Field)
	}
}

func TestValidateLoopZeroYAML_DefaultTypeWithoutPort(t *testing.T) {
	// Arrange: no type specified (defaults to server), no port
	content := `
name: my-project
apps:
  api: {}
`

	// Act
	_, _, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err == nil {
		t.Fatal("expected error for default type without port, got nil")
	}
	valErr, ok := err.(*source.ValidationError)
	if !ok {
		t.Fatalf("expected *source.ValidationError, got %T", err)
	}
	// When type is unspecified, the error field is apps.<name> (not apps.<name>.port)
	if valErr.Field != "apps.api" {
		t.Fatalf("expected field 'apps.api', got '%s'", valErr.Field)
	}
}

func TestValidateLoopZeroYAML_InvalidAppType(t *testing.T) {
	// Arrange
	content := `
name: my-project
apps:
  api:
    type: microservice
    port: 8080
`

	// Act
	_, _, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid app type, got nil")
	}
	valErr, ok := err.(*source.ValidationError)
	if !ok {
		t.Fatalf("expected *source.ValidationError, got %T", err)
	}
	if valErr.Field != "apps.api.type" {
		t.Fatalf("expected field 'apps.api.type', got '%s'", valErr.Field)
	}
}

func TestValidateLoopZeroYAML_WorkerWithoutPort(t *testing.T) {
	// Arrange: worker type does not require a port
	content := `
name: my-project
apps:
  processor:
    type: worker
`

	// Act
	cfg, appNames, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err != nil {
		t.Fatalf("expected no error for worker without port, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(appNames) != 1 {
		t.Fatalf("expected 1 app name, got %d", len(appNames))
	}
}

func TestValidateLoopZeroYAML_FrontendWithoutPort(t *testing.T) {
	// Arrange: frontend type does not require a port
	content := `
name: my-project
apps:
  web:
    type: frontend
`

	// Act
	cfg, appNames, err := source.ValidateLoopZeroYAML(content)

	// Assert
	if err != nil {
		t.Fatalf("expected no error for frontend without port, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(appNames) != 1 {
		t.Fatalf("expected 1 app name, got %d", len(appNames))
	}
}
