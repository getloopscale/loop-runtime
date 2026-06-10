// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// helper: run lint on inline Dockerfile content, return findings + exit
type lintResult struct {
	findings []Finding
	rejects  int
	warns    int
}

func runLint(t *testing.T, dockerfile string) lintResult {
	t.Helper()
	findings := lintDockerfile("test-app", "Dockerfile", dockerfile, defaultAllowedRegistryPrefixes)
	res := lintResult{findings: findings}
	for _, f := range findings {
		switch f.Severity {
		case SeverityReject:
			res.rejects++
		case SeverityWarn:
			res.warns++
		}
	}
	return res
}

// ---- Hard rejection rules ----

func TestRejects_FromUnauthorizedRegistry(t *testing.T) {
	res := runLint(t, "FROM evil-registry.com/malicious:latest\nCMD echo hi\n")
	if res.rejects == 0 {
		t.Fatalf("expected REJECT, got %d findings: %+v", len(res.findings), res.findings)
	}
	if res.findings[0].Rule != "FROM_REGISTRY_NOT_ALLOWED" {
		t.Errorf("rule = %s, want FROM_REGISTRY_NOT_ALLOWED", res.findings[0].Rule)
	}
}

func TestAccepts_DockerHubLibrary(t *testing.T) {
	res := runLint(t, "FROM node:20-alpine\nCMD echo hi\n")
	if res.rejects > 0 {
		t.Fatalf("docker.io/library/* must be allowed, got rejections: %+v", res.findings)
	}
}

// T2.3 of deep-review-remediation-plan (closes find_sec_003 HIGH).
// `docker.io/<org>/*` (e.g., `docker.io/bitnami/redis:7`, `docker.io/attacker/malware`)
// is INTENTIONALLY rejected by default. Only `docker.io/library/` (Docker
// Official Images) is in the default allow-list. Non-library Docker Hub
// images require explicit opt-in via LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES.
func TestRejects_DockerIoOrgImage_BareName(t *testing.T) {
	res := runLint(t, "FROM bitnami/redis:7\nCMD echo hi\n")
	if res.rejects == 0 {
		t.Fatalf("docker.io/<org>/* (bare name without library/) MUST be rejected, got accepted: %+v", res.findings)
	}
}

func TestRejects_DockerIoExplicitAttackerImage(t *testing.T) {
	// The attacker scenario from find_sec_003: an explicit docker.io reference
	// to a non-library namespace — previously allowed by the broad docker.io/
	// prefix, now correctly rejected.
	res := runLint(t, "FROM docker.io/attacker/malware:latest\nCMD echo hi\n")
	if res.rejects == 0 {
		t.Fatalf("docker.io/attacker/malware MUST be rejected (find_sec_003 attacker scenario), got accepted: %+v", res.findings)
	}
}

func TestAccepts_GHCR(t *testing.T) {
	res := runLint(t, "FROM ghcr.io/foo/bar:v1\nCMD echo hi\n")
	if res.rejects > 0 {
		t.Fatalf("ghcr.io/* must be allowed, got rejections: %+v", res.findings)
	}
}

func TestAccepts_DOCR(t *testing.T) {
	res := runLint(t, "FROM registry.digitalocean.com/loopzero/x:v1\nCMD echo hi\n")
	if res.rejects > 0 {
		t.Fatalf("DOCR must be allowed, got rejections: %+v", res.findings)
	}
}

func TestAccepts_K3dRegistry(t *testing.T) {
	res := runLint(t, "FROM k3d-loopzero-registry:5000/foo:latest\nCMD echo hi\n")
	if res.rejects > 0 {
		t.Fatalf("k3d-loopzero-registry must be allowed, got rejections: %+v", res.findings)
	}
}

func TestAccepts_FromWithPlatformAndAS(t *testing.T) {
	res := runLint(t, "FROM --platform=linux/amd64 node:20-alpine AS builder\nCMD echo hi\n")
	if res.rejects > 0 {
		t.Fatalf("--platform + AS should not break parsing, got rejections: %+v", res.findings)
	}
}

func TestRejects_RunPrivileged(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nRUN --privileged echo hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "RUN_FLAG_PROHIBITED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected RUN_FLAG_PROHIBITED, got: %+v", res.findings)
	}
}

func TestRejects_RunSecurityInsecure(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nRUN --security=insecure apk add foo\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "RUN_FLAG_PROHIBITED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected RUN_FLAG_PROHIBITED for --security=insecure, got: %+v", res.findings)
	}
}

func TestRejects_BindMountAbsolute(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nRUN --mount=type=bind,src=/etc/passwd,dst=/leak cat /leak\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "BIND_MOUNT_ESCAPES_CONTEXT" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected BIND_MOUNT_ESCAPES_CONTEXT, got: %+v", res.findings)
	}
}

func TestRejects_BindMountWithDotDot(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nRUN --mount=type=bind,src=../outside-context,dst=/x ls /x\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "BIND_MOUNT_ESCAPES_CONTEXT" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected BIND_MOUNT_ESCAPES_CONTEXT for ../, got: %+v", res.findings)
	}
}

func TestAccepts_BindMountRelativeSafe(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nRUN --mount=type=bind,src=./vendor,dst=/v cp -r /v /app\n")
	for _, f := range res.findings {
		if f.Rule == "BIND_MOUNT_ESCAPES_CONTEXT" {
			t.Fatalf("relative bind src should be allowed, got REJECT: %+v", f)
		}
	}
}

func TestAccepts_CacheMount(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nRUN --mount=type=cache,target=/var/cache/apt apt update\n")
	for _, f := range res.findings {
		if f.Rule == "BIND_MOUNT_ESCAPES_CONTEXT" || f.Rule == "RUN_FLAG_PROHIBITED" {
			t.Fatalf("cache mount should be allowed, got REJECT: %+v", f)
		}
	}
}

func TestRejects_ReservedEnvKey_LOOPZERO(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nENV LOOPZERO_TENANT_ID=fake\nCMD echo hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "RESERVED_ENV_KEY" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected RESERVED_ENV_KEY for LOOPZERO_*, got: %+v", res.findings)
	}
}

func TestRejects_ReservedArgKey_KUBERNETES(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nARG KUBERNETES_SERVICE_HOST=fake\nCMD echo hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "RESERVED_ENV_KEY" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected RESERVED_ENV_KEY for KUBERNETES_*, got: %+v", res.findings)
	}
}

func TestRejects_ReservedEnvKey_AWS(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nENV AWS_SECRET_ACCESS_KEY=stolen\nCMD echo hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "RESERVED_ENV_KEY" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected RESERVED_ENV_KEY for AWS_*, got: %+v", res.findings)
	}
}

func TestRejects_SeccompUnconfined(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nLABEL io.kubernetes.cri.seccomp=unconfined\nCMD hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "SECCOMP_UNCONFINED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SECCOMP_UNCONFINED, got: %+v", res.findings)
	}
}

// ---- Soft warnings ----

func TestWarns_UserRootInFinalStage(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nUSER root\nCMD hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "USER_ROOT_FINAL_STAGE" && f.Severity == SeverityWarn {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected USER_ROOT_FINAL_STAGE warn, got: %+v", res.findings)
	}
}

func TestWarns_UserZeroInFinalStage(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nUSER 0\nCMD hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "USER_ROOT_FINAL_STAGE" && f.Severity == SeverityWarn {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected USER_ROOT_FINAL_STAGE warn for USER 0, got: %+v", res.findings)
	}
}

func TestNoWarn_NonRootUser(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nUSER 1000\nCMD hi\n")
	for _, f := range res.findings {
		if f.Rule == "USER_ROOT_FINAL_STAGE" {
			t.Fatalf("non-root USER should not warn, got: %+v", f)
		}
	}
}

func TestWarns_AddRemoteURL(t *testing.T) {
	res := runLint(t, "FROM alpine:3.20\nADD https://example.com/x.tar.gz /tmp/\nCMD hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "ADD_REMOTE_URL" && f.Severity == SeverityWarn {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ADD_REMOTE_URL warn, got: %+v", res.findings)
	}
}

func TestWarns_AptWithoutNoRecommends(t *testing.T) {
	res := runLint(t, "FROM ubuntu:24.04\nRUN apt-get update && apt-get install -y curl\nCMD hi\n")
	found := false
	for _, f := range res.findings {
		if f.Rule == "APT_NO_RECOMMENDS" && f.Severity == SeverityWarn {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected APT_NO_RECOMMENDS warn, got: %+v", res.findings)
	}
}

func TestNoWarn_AptWithNoRecommends(t *testing.T) {
	res := runLint(t, "FROM ubuntu:24.04\nRUN apt-get install -y --no-install-recommends curl\nCMD hi\n")
	for _, f := range res.findings {
		if f.Rule == "APT_NO_RECOMMENDS" {
			t.Fatalf("--no-install-recommends should suppress warn, got: %+v", f)
		}
	}
}

// ---- Multi-stage detection ----

func TestUserRootOnlyFlagsFinalStage(t *testing.T) {
	// Only the LAST FROM is the final stage; USER root in build stage is OK.
	df := `FROM alpine:3.20 AS build
USER root
RUN echo build-time

FROM alpine:3.20
USER 1000
CMD hi
`
	res := runLint(t, df)
	for _, f := range res.findings {
		if f.Rule == "USER_ROOT_FINAL_STAGE" {
			t.Fatalf("intermediate stage USER root should not warn, got: %+v", f)
		}
	}
}

// ---- Happy path ----

func TestCleanDockerfilePassesAll(t *testing.T) {
	df := `FROM node:20-alpine AS deps
WORKDIR /app
COPY package*.json ./
RUN npm install --omit=dev

FROM node:20-alpine
WORKDIR /app
USER 1000
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NODE_ENV=production
EXPOSE 3001
CMD ["node", "src/index.js"]
`
	res := runLint(t, df)
	if res.rejects != 0 {
		t.Fatalf("clean Dockerfile must have 0 rejects, got %d: %+v", res.rejects, res.findings)
	}
}

// ---- Binary integration test (CLI exit codes + JSON output) ----

func TestBinary_ExitsZeroOnClean(t *testing.T) {
	bin := buildBinary(t)
	source := t.TempDir()
	dfPath := filepath.Join(source, "Dockerfile")
	must(t, os.WriteFile(dfPath, []byte("FROM node:20-alpine\nUSER 1000\nCMD hi\n"), 0o644))

	cmd := exec.Command(bin, "--source", source, "--apps", ".:Dockerfile")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0, got error: %v\nout: %s", err, out)
	}
}

func TestBinary_ExitsOneOnReject(t *testing.T) {
	bin := buildBinary(t)
	source := t.TempDir()
	dfPath := filepath.Join(source, "Dockerfile")
	must(t, os.WriteFile(dfPath, []byte("FROM evil.com/x:latest\nCMD hi\n"), 0o644))

	cmd := exec.Command(bin, "--source", source, "--apps", ".:Dockerfile")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit 1 on reject, got success\nout: %s", out)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}
	if !strings.Contains(string(out), "FROM_REGISTRY_NOT_ALLOWED") {
		t.Errorf("expected JSON output with FROM_REGISTRY_NOT_ALLOWED, got: %s", out)
	}
}

func TestBinary_ExitsTwoOnUsageError(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin) // no --apps
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit 2 without --apps, got success\nout: %s", out)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.ExitCode())
	}
}

func TestBinary_OptInExtraRegistry(t *testing.T) {
	bin := buildBinary(t)
	source := t.TempDir()
	dfPath := filepath.Join(source, "Dockerfile")
	must(t, os.WriteFile(dfPath, []byte("FROM custom-registry.example.com/img:latest\nUSER 1000\nCMD hi\n"), 0o644))

	cmd := exec.Command(bin, "--source", source, "--apps", ".:Dockerfile")
	cmd.Env = append(os.Environ(),
		"LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES=custom-registry.example.com/")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 with opt-in registry, got error: %v\nout: %s", err, out)
	}
}

// ---- helpers ----

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binPath := filepath.Join(dir, "loopzero-dockerfile-lint")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\nout: %s", err, out)
	}
	return binPath
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
