// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package helmrender

import (
	"errors"
	"reflect"
	"testing"
)

func TestTemplateArgs(t *testing.T) {
	got := TemplateArgs("probe", "charts/x", []string{"a=1", "b=2"})
	want := []string{"template", "probe", "charts/x", "--set", "a=1", "--set", "b=2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := TemplateArgs("", "charts/x", nil); !reflect.DeepEqual(got, []string{"template", "charts/x"}) {
		t.Fatalf("an empty release must be omitted, got %v", got)
	}
}

func TestRender_HelmAbsentIsErrHelmUnavailable(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := (Helm{}).Render(t.TempDir(), nil); !errors.Is(err, ErrHelmUnavailable) {
		t.Fatalf("want ErrHelmUnavailable, got %v", err)
	}
}
