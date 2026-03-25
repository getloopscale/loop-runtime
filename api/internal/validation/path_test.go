// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
