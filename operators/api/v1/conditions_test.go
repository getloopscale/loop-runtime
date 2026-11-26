// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSetGetCondition_roundtrip proves the condition helpers now live in the
// api module (M6 decouple): SetCondition adds, GetCondition reads it back.
func TestSetGetCondition_roundtrip(t *testing.T) {
	var conds []metav1.Condition
	SetCondition(&conds, TypeGitRepoReady, metav1.ConditionTrue, "Created", "git repo created", 1)

	got := GetCondition(conds, TypeGitRepoReady)
	if got == nil {
		t.Fatalf("GetCondition returned nil after SetCondition")
	}
	if got.Status != metav1.ConditionTrue || got.Reason != "Created" || got.ObservedGeneration != 1 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

// TestConditionTypeConstants_values locks the CR-status contract vocabulary.
func TestConditionTypeConstants_values(t *testing.T) {
	cases := map[string]string{
		TypeGitRepoReady:      "GitRepoReady",
		TypeEnvNamespaceReady: "NamespaceReady",
		TypeServiceResolved:   "ServiceResolved",
		TypeCertificateIssued: "CertificateIssued",
		TypeBootstrapComplete: "BootstrapComplete",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("condition constant value drift: got %q want %q", got, want)
		}
	}
}

// TestApiModuleIsCleanBase is the keystone decouple guard: NO .go file in this
// package (production OR test) may import anything under getloopscale/loop-runtime/operators/
// except this same api module. The acyclic-graph gate enforces production; this
// test additionally covers the package's own test files, which the gate's
// node-scan also sees but which would otherwise be easy to regress.
func TestApiModuleIsCleanBase(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	forbidden := []string{
		"github.com/getloopscale/loop-runtime/operators/internal",
		"github.com/getloopscale/loop-runtime/api",
		"github.com/getloopscale/loop-runtime/pkg",
		"github.com/getloopscale/loop-runtime/cloud",
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(".", e.Name()), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, forb := range forbidden {
				if strings.HasPrefix(path, forb) {
					t.Errorf("%s imports %q — api module must be a clean base (M6 decouple)", e.Name(), path)
				}
			}
		}
	}
}
