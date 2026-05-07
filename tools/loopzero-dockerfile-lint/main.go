// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// loopzero-dockerfile-lint: static analyzer for user-supplied Dockerfiles.
//
// Runs as a step in the LoopZero build pipeline (Argo Workflow) BEFORE BuildKit.
// Validates that each app's Dockerfile complies with LoopZero's hard rejection
// rules (security) and emits warnings for soft issues.
//
// Plan: docs/plans/build-mode-dispatch-and-dockerfile-security-plan.md
//
// Usage:
//
//	loopzero-dockerfile-lint --source /workspace --apps "apps/api:./apps/api/Dockerfile,apps/web:./apps/web/Dockerfile"
//
// Exit codes:
//
//	0 — all Dockerfiles passed (warnings allowed)
//	1 — at least one hard rejection rule fired
//	2 — usage error (bad args, file not readable)
//
// Output: structured JSON lines on stdout, one per finding (rule, severity,
// app, line, msg). Loki captures these for audit.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Severity classifies findings.
type Severity string

const (
	SeverityReject  Severity = "REJECT"  // hard fail, exit 1
	SeverityWarn    Severity = "WARN"    // log only, exit 0
	SeverityInfo    Severity = "INFO"    // diagnostic
)

// Finding is one structured event emitted to stdout.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	App      string   `json:"app"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Msg      string   `json:"msg"`
}

// allowedRegistryPrefixes lists FROM image prefixes that ARE allowed by
// default. Anything else fires REGISTRY_NOT_ALLOWED hard rejection.
//
// Pattern is prefix match: "docker.io/library/" matches "docker.io/library/node:20".
// Bare names ("node:20", "alpine:3.20") are interpreted as docker.io/library/<name>.
//
// To extend: tenant declares `build.allowed_registries: [...]` in loopzero.yaml.
// (NOT IMPLEMENTED IN THIS LINTER YET — opt-in extension via env var
// LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES, comma-separated, parsed by main()).
var defaultAllowedRegistryPrefixes = []string{
	"docker.io/library/",
	"docker.io/",
	"ghcr.io/",
	"registry.digitalocean.com/",
	"k3d-loopzero-registry:5000/",
	"public.ecr.aws/",
	"gcr.io/",
	"mcr.microsoft.com/",
	"quay.io/",
}

// reservedEnvKeyPrefixes are env/ARG keys that user Dockerfiles MUST NOT
// declare or override. They're reserved for LoopZero's runtime injection.
var reservedEnvKeyPrefixes = []string{
	"LOOPZERO_",
	"KUBERNETES_",
	"AWS_",
	"DO_",
}

// AppSpec is the (path, dockerfilePath) pair for one app being linted.
type AppSpec struct {
	Path           string // e.g., "apps/api"
	DockerfilePath string // e.g., "/workspace/apps/api/Dockerfile" (absolute) or "./Dockerfile" (relative to source)
}

func main() {
	source := flag.String("source", "/workspace", "Root directory of cloned source code")
	appsCSV := flag.String("apps", "", "Comma-separated list of '<path>:<dockerfile-path>' entries")
	jsonOnly := flag.Bool("json", false, "Suppress human-friendly summary (JSON only)")
	flag.Parse()

	if *appsCSV == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --apps is required (comma-separated '<path>:<dockerfile-path>')")
		os.Exit(2)
	}

	apps, err := parseAppsCSV(*appsCSV)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: parsing --apps: %v\n", err)
		os.Exit(2)
	}

	// Optional opt-in: tenant-allowed extra registries.
	extraAllowed := splitAndTrim(os.Getenv("LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES"))
	allowed := append([]string{}, defaultAllowedRegistryPrefixes...)
	allowed = append(allowed, extraAllowed...)

	totalRejects := 0
	totalWarns := 0

	for _, app := range apps {
		path := app.DockerfilePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(*source, path)
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			emit(Finding{
				Rule:     "FILE_NOT_READABLE",
				Severity: SeverityReject,
				App:      app.Path,
				File:     path,
				Line:     0,
				Msg:      fmt.Sprintf("cannot read Dockerfile: %v", readErr),
			})
			totalRejects++
			continue
		}

		findings := lintDockerfile(app.Path, path, string(content), allowed)
		for _, f := range findings {
			emit(f)
			switch f.Severity {
			case SeverityReject:
				totalRejects++
			case SeverityWarn:
				totalWarns++
			}
		}
	}

	if !*jsonOnly {
		fmt.Fprintf(os.Stderr, "\n[loopzero-dockerfile-lint] %d apps, %d hard rejections, %d warnings\n",
			len(apps), totalRejects, totalWarns)
	}

	if totalRejects > 0 {
		os.Exit(1)
	}
}

// emit writes a Finding as one JSON line to stdout. Loki captures these.
func emit(f Finding) {
	b, err := json.Marshal(f)
	if err != nil {
		// Should not happen for well-formed Finding.
		fmt.Fprintf(os.Stderr, "[loopzero-dockerfile-lint] marshal error: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

// parseAppsCSV parses "path1:dockerfile1,path2:dockerfile2" into AppSpecs.
// The colon SPLIT supports paths without colons (Linux). Windows paths with
// drive letters (C:\) would need different handling — out of scope.
func parseAppsCSV(csv string) ([]AppSpec, error) {
	if csv == "" {
		return nil, fmt.Errorf("empty apps list")
	}
	var apps []AppSpec
	for _, entry := range strings.Split(csv, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		idx := strings.Index(entry, ":")
		if idx < 0 {
			return nil, fmt.Errorf("invalid entry %q (expected <path>:<dockerfile-path>)", entry)
		}
		apps = append(apps, AppSpec{
			Path:           strings.TrimSpace(entry[:idx]),
			DockerfilePath: strings.TrimSpace(entry[idx+1:]),
		})
	}
	if len(apps) == 0 {
		return nil, fmt.Errorf("no apps after parsing")
	}
	return apps, nil
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// lintDockerfile applies all rules to the content of a single Dockerfile.
// Returns ordered findings (file order, not severity order).
func lintDockerfile(appName, file, content string, allowedRegistryPrefixes []string) []Finding {
	var findings []Finding
	lines := strings.Split(content, "\n")

	// Track whether we're in the "final" stage (last FROM block).
	// Simple heuristic: count FROM lines, last one is the final stage.
	totalFrom := 0
	for _, l := range lines {
		if isInstructionStart(l, "FROM") {
			totalFrom++
		}
	}
	currentFromIdx := 0
	currentUserInFinal := "" // tracked only in final stage

	for i, raw := range lines {
		line := stripCommentAndContinue(raw)
		lineNum := i + 1
		if line == "" {
			continue
		}

		// FROM checks (registry allow-list)
		if isInstructionStart(line, "FROM") {
			currentFromIdx++
			img := extractFromImage(line)
			if img != "" && !isAllowedRegistry(img, allowedRegistryPrefixes) {
				findings = append(findings, Finding{
					Rule:     "FROM_REGISTRY_NOT_ALLOWED",
					Severity: SeverityReject,
					App:      appName,
					File:     file,
					Line:     lineNum,
					Msg: fmt.Sprintf("FROM %q references a registry not in LoopZero's allow-list. "+
						"Allowed prefixes: %s. To extend, declare `build.allowed_registries: [...]` in loopzero.yaml "+
						"(opt-in, audit-logged).", img, strings.Join(allowedRegistryPrefixes, ", ")),
				})
			}
			continue
		}

		// USER tracking — only flag in the FINAL stage
		if isInstructionStart(line, "USER") && currentFromIdx == totalFrom {
			currentUserInFinal = strings.TrimSpace(strings.TrimPrefix(line, "USER"))
		}

		// RUN flag checks (--privileged, --security=insecure, --mount=type=bind)
		if isInstructionStart(line, "RUN") {
			if matched, flag := containsRiskyRunFlag(line); matched {
				findings = append(findings, Finding{
					Rule:     "RUN_FLAG_PROHIBITED",
					Severity: SeverityReject,
					App:      appName,
					File:     file,
					Line:     lineNum,
					Msg: fmt.Sprintf("RUN flag %q is prohibited. LoopZero's build sandbox does not permit "+
						"privileged operations, insecure security overrides, or absolute bind mounts.", flag),
				})
			}
			if matched, kind := containsBindMountEscapingContext(line); matched {
				findings = append(findings, Finding{
					Rule:     "BIND_MOUNT_ESCAPES_CONTEXT",
					Severity: SeverityReject,
					App:      appName,
					File:     file,
					Line:     lineNum,
					Msg: fmt.Sprintf("RUN --mount=type=bind src=%q is outside build context or absolute. "+
						"Use --mount=type=cache or --mount=type=secret instead.", kind),
				})
			}
		}

		// ARG / ENV reserved keys
		if isInstructionStart(line, "ARG") || isInstructionStart(line, "ENV") {
			if reserved, key := containsReservedEnvKey(line); reserved {
				findings = append(findings, Finding{
					Rule:     "RESERVED_ENV_KEY",
					Severity: SeverityReject,
					App:      appName,
					File:     file,
					Line:     lineNum,
					Msg: fmt.Sprintf("ARG/ENV key %q uses a reserved prefix (LOOPZERO_/KUBERNETES_/AWS_/DO_). "+
						"These names are reserved for LoopZero's runtime injection.", key),
				})
			}
		}

		// SECCOMP / AppArmor unconfined
		if containsSeccompUnconfined(line) {
			findings = append(findings, Finding{
				Rule:     "SECCOMP_UNCONFINED",
				Severity: SeverityReject,
				App:      appName,
				File:     file,
				Line:     lineNum,
				Msg:      "SECCOMP=unconfined or AppArmor=unconfined is prohibited.",
			})
		}

		// Soft warnings
		if isInstructionStart(line, "ADD") && containsRemoteURL(line) {
			findings = append(findings, Finding{
				Rule:     "ADD_REMOTE_URL",
				Severity: SeverityWarn,
				App:      appName,
				File:     file,
				Line:     lineNum,
				Msg:      "ADD <url> is less reproducible than COPY + RUN curl/wget. Consider COPY for local files only.",
			})
		}
		if isInstructionStart(line, "HEALTHCHECK") && !strings.Contains(line, " CMD") && !strings.Contains(line, "\tCMD") {
			findings = append(findings, Finding{
				Rule:     "HEALTHCHECK_NO_CMD",
				Severity: SeverityWarn,
				App:      appName,
				File:     file,
				Line:     lineNum,
				Msg:      "HEALTHCHECK without CMD — kubelet's readiness/liveness probes will be used instead.",
			})
		}
		if isInstructionStart(line, "RUN") && containsAptGetWithoutNoRecommends(line) {
			findings = append(findings, Finding{
				Rule:     "APT_NO_RECOMMENDS",
				Severity: SeverityWarn,
				App:      appName,
				File:     file,
				Line:     lineNum,
				Msg:      "apt-get install without --no-install-recommends will bloat the image.",
			})
		}
	}

	// USER 0 / root in final stage warning
	if currentUserInFinal == "0" || currentUserInFinal == "root" {
		findings = append(findings, Finding{
			Rule:     "USER_ROOT_FINAL_STAGE",
			Severity: SeverityWarn,
			App:      appName,
			File:     file,
			Line:     0,
			Msg: "Final stage runs as root (USER 0/root). PSA restricted will reject the pod " +
				"(runAsNonRoot=true is enforced). Add a non-root USER instruction.",
		})
	}

	return findings
}

// --- helpers ---

func isInstructionStart(line, instruction string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	upper := strings.ToUpper(t)
	if !strings.HasPrefix(upper, instruction) {
		return false
	}
	rest := upper[len(instruction):]
	// Must be followed by whitespace or end of line
	return rest == "" || rest[0] == ' ' || rest[0] == '\t'
}

func stripCommentAndContinue(line string) string {
	// Remove inline trailing comments (Dockerfile only honors line-leading # but be defensive).
	t := strings.TrimRight(line, "\\ \t\r\n")
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "#") {
		return ""
	}
	return t
}

// extractFromImage returns the image reference from a FROM line.
//
//	FROM image:tag        → "image:tag"
//	FROM image AS stage   → "image"
//	FROM --platform=x img → "img"
func extractFromImage(line string) string {
	t := strings.TrimSpace(line)
	t = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(t), "FROM"))
	// Use original-case after stripping prefix
	idxFrom := strings.Index(strings.ToUpper(line), "FROM")
	if idxFrom < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idxFrom+4:])
	// Split tokens, skip --flags
	fields := strings.Fields(rest)
	for _, f := range fields {
		if strings.HasPrefix(f, "--") {
			continue
		}
		// Stop at "AS" keyword
		if strings.EqualFold(f, "AS") {
			break
		}
		return f
	}
	return ""
}

func isAllowedRegistry(imageRef string, allowedPrefixes []string) bool {
	// Handle bare images: "node:20" or "alpine" → docker.io/library/<name>
	normalized := imageRef
	if !strings.Contains(imageRef, "/") {
		normalized = "docker.io/library/" + imageRef
	} else if !strings.Contains(strings.SplitN(imageRef, "/", 2)[0], ".") &&
		!strings.Contains(strings.SplitN(imageRef, "/", 2)[0], ":") {
		// "namespace/image:tag" without explicit registry → docker.io/<namespace>/<image>
		normalized = "docker.io/" + imageRef
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

var (
	riskyRunFlagRegex = regexp.MustCompile(`(?i)--security[=\s]+insecure|--privileged`)
	bindMountRegex    = regexp.MustCompile(`(?i)--mount[=\s]*type[=\s]*bind`)
)

func containsRiskyRunFlag(line string) (bool, string) {
	if m := riskyRunFlagRegex.FindString(line); m != "" {
		return true, strings.TrimSpace(m)
	}
	return false, ""
}

// containsBindMountEscapingContext returns (matched, source-spec).
// A bind mount is rejected unless its src is relative AND inside the build context.
// Conservative: any --mount=type=bind triggers REJECT unless src= is a relative
// path without ".." segments.
func containsBindMountEscapingContext(line string) (bool, string) {
	if !bindMountRegex.MatchString(line) {
		return false, ""
	}
	// Find src=...
	srcRe := regexp.MustCompile(`(?i)src=([^\s,]+)`)
	srcMatch := srcRe.FindStringSubmatch(line)
	if srcMatch == nil {
		// --mount=type=bind without src — defaults to context root, allowed
		return false, ""
	}
	src := strings.Trim(srcMatch[1], `"'`)
	if filepath.IsAbs(src) || strings.Contains(src, "..") {
		return true, src
	}
	return false, ""
}

func containsReservedEnvKey(line string) (bool, string) {
	t := strings.TrimSpace(line)
	upper := strings.ToUpper(t)
	for _, prefix := range []string{"ARG ", "ENV "} {
		if !strings.HasPrefix(upper, prefix) {
			continue
		}
		rest := strings.TrimSpace(t[len(prefix):])
		// Get key (everything before = or whitespace)
		key := rest
		for _, sep := range []string{"=", " ", "\t"} {
			if idx := strings.Index(key, sep); idx >= 0 {
				key = key[:idx]
			}
		}
		key = strings.TrimSpace(key)
		for _, p := range reservedEnvKeyPrefixes {
			if strings.HasPrefix(strings.ToUpper(key), p) {
				return true, key
			}
		}
	}
	return false, ""
}

var seccompUnconfinedRegex = regexp.MustCompile(`(?i)(seccomp[=\s]+unconfined|apparmor[=\s]+unconfined)`)

func containsSeccompUnconfined(line string) bool {
	return seccompUnconfinedRegex.MatchString(line)
}

var addRemoteURLRegex = regexp.MustCompile(`(?i)^\s*ADD\s+https?://`)

func containsRemoteURL(line string) bool {
	return addRemoteURLRegex.MatchString(line)
}

var aptGetWithoutNoRecommendsRegex = regexp.MustCompile(`(?i)apt(?:-get)?\s+install`)

func containsAptGetWithoutNoRecommends(line string) bool {
	if !aptGetWithoutNoRecommendsRegex.MatchString(line) {
		return false
	}
	if strings.Contains(strings.ToLower(line), "--no-install-recommends") {
		return false
	}
	return true
}
