// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dockerfilemirror

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeImages materializes subdir→Dockerfile-content under a temp images dir.
func writeImages(t *testing.T, dockerfiles map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "images")
	for sub, content := range dockerfiles {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

func TestCheck_MirrorGcr_Pass(t *testing.T) {
	dir := writeImages(t, map[string]string{
		"clone-runner": "FROM mirror.gcr.io/library/alpine:3.20\nRUN echo hi\n",
	})
	out, err := Check(Config{ImagesDir: dir})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass || out.OK != 1 || out.Total != 1 {
		t.Fatalf("mirror.gcr.io FROM must PASS: %+v", out)
	}
}

func TestCheck_PlainDockerHub_Fails(t *testing.T) {
	dir := writeImages(t, map[string]string{
		"bad": "FROM alpine:3.20\n",
	})
	out, _ := Check(Config{ImagesDir: dir})
	if out.Pass || len(out.Violations) != 1 {
		t.Fatalf("plain alpine: (Docker Hub default) must FAIL: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "alpine") {
		t.Fatalf("violation must name the bad FROM: %v", out.Violations)
	}
}

func TestCheck_DOCR_Pass(t *testing.T) {
	dir := writeImages(t, map[string]string{
		"docr": "FROM registry.digitalocean.com/loop-runtime-system/base:latest\n",
	})
	out, _ := Check(Config{ImagesDir: dir})
	if !out.Pass {
		t.Fatalf("DOCR-prefixed FROM must PASS: %+v", out)
	}
}

func TestCheck_FullPathRegistry_Pass(t *testing.T) {
	dir := writeImages(t, map[string]string{
		"quay": "FROM quay.io/prometheus/busybox:latest\n",
	})
	out, _ := Check(Config{ImagesDir: dir})
	if !out.Pass {
		t.Fatalf("full-path (>=2 slashes) registry ref must PASS: %+v", out)
	}
}

func TestCheck_MultiStageMixed_FailsOnBad(t *testing.T) {
	dir := writeImages(t, map[string]string{
		"multi": "FROM mirror.gcr.io/library/golang:1.25 AS build\nRUN go build\nFROM node:20 AS run\n",
	})
	out, _ := Check(Config{ImagesDir: dir})
	if out.Pass || out.Total != 2 || out.OK != 1 || len(out.Violations) != 1 {
		t.Fatalf("multi-stage: 1 good + 1 bad → 1 violation: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "node") {
		t.Fatalf("violation must be the node:20 stage: %v", out.Violations)
	}
}

func TestCheck_NoDockerfiles_Errors(t *testing.T) {
	dir := writeImages(t, map[string]string{})
	_, err := Check(Config{ImagesDir: dir})
	if err == nil {
		t.Fatal("no FROM directives must error (faithful to bash exit 1)")
	}
}
