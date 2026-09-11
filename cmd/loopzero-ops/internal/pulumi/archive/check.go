// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package archive exports Pulumi stack state for paralelas dirs to
// encrypted S3 (s3://loopzero-archive/pulumi-state/). Pre-flight verifies
// archive bucket has required SSE-KMS encryption (C4). Per-stack export
// goes through plaintext-secret regex scan (C9) before upload.
//
// Idempotent: re-running with same state + same timestamp produces a
// bit-identical archive set (timestamp filename suffix is the only
// variant). Idempotency test C11 covers this.
//
// Per condition C5: PULUMI_CONFIG_PASSPHRASE fetched at archive time
// from mgmt-platform-secrets, NOT colocated with archive.
// Per condition C17: pulumi version pinned 3.140.0.
//
// Companion to package orphanrm (archive then rm). Both implement
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 3 condition C14.
//
// Replaces infra/scripts/pulumi-archive.sh (~199 LOC) per Phase 7 of
// plan 2026-05-21-bash-to-go-migration-plan.
package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/shellrun"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// PulumiVersionPinned — bash PULUMI_VERSION_PINNED.
const PulumiVersionPinned = "3.140.0"

// DefaultArchiveBucket — S3 bucket holding the pulumi state archives.
const DefaultArchiveBucket = "loopzero-archive"

// DefaultParalelas — pulumi paralelas dirs subject to archive.
var DefaultParalelas = []string{
	"infra/pulumi-infra",
	"infra/pulumi-platform",
}

// PlaintextSecretPattern — C9 regex. Matches case-insensitive
// token/password/secret/api_key tokens.
var PlaintextSecretPattern = regexp.MustCompile(`(?i)(token|password|secret|api[_-]?key)`)

// CiphertextExcludePatterns are STRING VALUES we should NOT count as
// plaintext leakage. Bash version pattern-matched LINES (which included
// `"key": "value"` quotes); we walk extracted string VALUES here, so
// patterns omit the leading-quote.
//   - `ciphertext` — appears literally as a JSON key, captured by walkJSON
//   - `v1:` — Pulumi's encrypted-value prefix on string values
//   - `\u` — escaped unicode in encrypted blocks
var CiphertextExcludePatterns = []*regexp.Regexp{
	regexp.MustCompile(`ciphertext`),
	regexp.MustCompile(`^v1:`),
	regexp.MustCompile(`\\u`),
}

// Archived captures one successful (dir, stack) export.
type Archived struct {
	Dir           string
	Stack         string
	S3Key         string
	SHA256        string
	ResourceCount int
	PlaintextHits int // C9 warning count
}

// Config holds the archive inputs.
type Config struct {
	// Root is the repo root. Default: ".".
	Root string

	// Paralelas overrides DefaultParalelas.
	Paralelas []string

	// ArchiveBucket overrides DefaultArchiveBucket.
	ArchiveBucket string

	// Timestamp overrides the archive filename suffix
	// (default: time.Now().UTC().Format("20060102T150405Z")).
	Timestamp string

	// DryRun reports intended exports without writing to S3.
	DryRun bool

	// ArchiveOnly skips pre-flight bucket checks (C12 quarterly mode).
	ArchiveOnly bool

	// Runner is the shell runner (test injection via MockRunner).
	Runner shellrun.Runner

	// Env reads env vars; nil → os.Getenv.
	Env func(string) string

	// TmpDir overrides the temp dir for archive files. Default:
	// os.TempDir() + auto-generated subdir.
	TmpDir string
}

// Outcome captures the archive run result.
type Outcome struct {
	Pass              bool
	Targets           []string
	Archived          []Archived
	LiveResourceCount int
	Messages          []string
}

// preflight returns nil when env + bucket are OK to proceed. ArchiveOnly
// bypasses the bucket check (C12 quarterly mode).
func preflight(cfg Config, runner shellrun.Runner) error {
	env := cfg.Env
	if env == nil {
		env = os.Getenv
	}
	if cfg.DryRun {
		return nil // bash skips secret/AWS checks in dry-run
	}
	if env("PULUMI_CONFIG_PASSPHRASE") == "" {
		return &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: "pulumi-archive: PULUMI_CONFIG_PASSPHRASE env var required (C5)",
		}
	}
	if env("AWS_ACCESS_KEY_ID") == "" {
		return &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: "pulumi-archive: AWS_ACCESS_KEY_ID env var required",
		}
	}
	if cfg.ArchiveOnly {
		return nil // C12 mode trusts operator-provided bucket
	}
	bucket := cfg.ArchiveBucket
	if bucket == "" {
		bucket = DefaultArchiveBucket
	}
	// C4: bucket exists.
	ctx := context.Background()
	headRes, err := runner.Run(ctx, "aws", []string{"s3api", "head-bucket", "--bucket", bucket})
	if err != nil || headRes.ExitCode != 0 {
		return &loopzeroerrors.LoopZeroError{
			Code: "OPS_USAGE",
			Message: fmt.Sprintf("pulumi-archive: archive bucket s3://%s does not exist (C4)",
				bucket),
		}
	}
	// C4: SSE-KMS encryption required.
	encRes, err := runner.Run(ctx, "aws", []string{
		"s3api", "get-bucket-encryption", "--bucket", bucket,
	})
	if err != nil || encRes.ExitCode != 0 {
		return &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("pulumi-archive: bucket %s encryption query failed (C4 requires aws:kms)", bucket),
		}
	}
	algo, err := extractSSEAlgo(encRes.Stdout)
	if err != nil {
		return &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("pulumi-archive: parse encryption response: %v", err),
			Cause:   err,
		}
	}
	if algo != "aws:kms" {
		return &loopzeroerrors.LoopZeroError{
			Code: "OPS_USAGE",
			Message: fmt.Sprintf("pulumi-archive: bucket %s missing SSE-KMS (got %q; C4 requires aws:kms)",
				bucket, algo),
		}
	}
	return nil
}

// extractSSEAlgo parses the aws s3api get-bucket-encryption JSON.
func extractSSEAlgo(raw string) (string, error) {
	type encResp struct {
		ServerSideEncryptionConfiguration struct {
			Rules []struct {
				ApplyServerSideEncryptionByDefault struct {
					SSEAlgorithm string `json:"SSEAlgorithm"`
				} `json:"ApplyServerSideEncryptionByDefault"`
			} `json:"Rules"`
		} `json:"ServerSideEncryptionConfiguration"`
	}
	var r encResp
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return "", err
	}
	if len(r.ServerSideEncryptionConfiguration.Rules) == 0 {
		return "", fmt.Errorf("no encryption rules in response")
	}
	return r.ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm, nil
}

// enumerateTargets returns paralelas dirs that have a Pulumi.yaml.
func enumerateTargets(root string, paralelas []string) []string {
	var out []string
	for _, dir := range paralelas {
		pulumiYAML := filepath.Join(root, dir, "Pulumi.yaml")
		if _, err := os.Stat(pulumiYAML); err == nil {
			out = append(out, dir)
		}
	}
	return out
}

// stackEntry parses pulumi stack ls --json.
type stackEntry struct {
	Name string `json:"name"`
}

// listStacks invokes `pulumi stack ls --json` in dir.
func listStacks(ctx context.Context, runner shellrun.Runner, repoRoot, dir string) ([]string, error) {
	res, err := runner.Run(ctx, "pulumi", []string{"stack", "ls", "--json"},
		shellrun.WithCwd(filepath.Join(repoRoot, dir)))
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("pulumi stack ls (dir=%s): exit %d: %s", dir, res.ExitCode, res.Stderr)
	}
	stdout := strings.TrimSpace(res.Stdout)
	if stdout == "" {
		return nil, nil
	}
	var entries []stackEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		return nil, fmt.Errorf("parse pulumi stack ls json (dir=%s): %w", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Name != "" {
			out = append(out, e.Name)
		}
	}
	return out, nil
}

// stackResourceCount parses pulumi stack --stack X --json.
func stackResourceCount(ctx context.Context, runner shellrun.Runner, repoRoot, dir, stack string) int {
	res, err := runner.Run(ctx, "pulumi", []string{
		"stack", "--stack", stack, "--json",
	}, shellrun.WithCwd(filepath.Join(repoRoot, dir)))
	if err != nil || res.ExitCode != 0 {
		return 0 // bash || echo 0
	}
	type info struct {
		Resources []json.RawMessage `json:"resources"`
	}
	var inf info
	if err := json.Unmarshal([]byte(res.Stdout), &inf); err != nil {
		return 0
	}
	return len(inf.Resources)
}

// exportStack invokes `pulumi stack export --stack X --file path`.
func exportStack(ctx context.Context, runner shellrun.Runner, repoRoot, dir, stack, file string) error {
	res, err := runner.Run(ctx, "pulumi", []string{
		"stack", "export", "--stack", stack, "--file", file,
	}, shellrun.WithCwd(filepath.Join(repoRoot, dir)))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("pulumi stack export %s/%s: exit %d: %s", dir, stack, res.ExitCode, res.Stderr)
	}
	return nil
}

// uploadS3 invokes `aws s3 cp ... --acl private --sse aws:kms`.
func uploadS3(ctx context.Context, runner shellrun.Runner, localPath, s3Key, sha string, resourceCount int) error {
	res, err := runner.Run(ctx, "aws", []string{
		"s3", "cp", localPath, s3Key,
		"--acl", "private",
		"--sse", "aws:kms",
		"--metadata", fmt.Sprintf("snapshot-sha256=%s,resources=%d", sha, resourceCount),
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("s3 cp %s → %s: exit %d: %s", localPath, s3Key, res.ExitCode, res.Stderr)
	}
	return nil
}

// scanPlaintextSecrets reads the exported archive and counts plaintext-
// secret hits OUTSIDE Pulumi's ciphertext blocks (mirrors bash jq+grep).
func scanPlaintextSecrets(archivePath string) (int, error) {
	data, err := os.ReadFile(archivePath) //nolint:gosec // operator-controlled
	if err != nil {
		return 0, err
	}
	// JSON-walk: extract all string values, exclude lines matching any
	// of the ciphertext exclude patterns, count remaining matches.
	var allStrings []string
	if err := collectStrings(data, &allStrings); err != nil {
		return 0, err
	}
	count := 0
	for _, s := range allStrings {
		excluded := false
		for _, ex := range CiphertextExcludePatterns {
			if ex.MatchString(s) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		if PlaintextSecretPattern.MatchString(s) {
			count++
		}
	}
	return count, nil
}

// collectStrings traverses JSON and appends every string leaf to out.
func collectStrings(raw []byte, out *[]string) error {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	walkJSON(v, out)
	return nil
}

func walkJSON(v interface{}, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case map[string]interface{}:
		for _, val := range x {
			walkJSON(val, out)
		}
	case []interface{}:
		for _, item := range x {
			walkJSON(item, out)
		}
	}
}

// hashFile returns the hex SHA256 of the file.
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-controlled
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// archivePrefix mirrors bash `${dir//\//-}`.
func archivePrefix(dir string) string {
	return strings.ReplaceAll(dir, "/", "-")
}

// Run executes the archive flow.
func Run(cfg Config) (*Outcome, error) {
	runner := cfg.Runner
	if runner == nil {
		runner = shellrun.NewDefault()
	}
	if err := preflight(cfg, runner); err != nil {
		return nil, err
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	paralelas := cfg.Paralelas
	if paralelas == nil {
		paralelas = DefaultParalelas
	}
	bucket := cfg.ArchiveBucket
	if bucket == "" {
		bucket = DefaultArchiveBucket
	}
	ts := cfg.Timestamp
	if ts == "" {
		ts = time.Now().UTC().Format("20060102T150405Z")
	}

	out := &Outcome{}
	targets := enumerateTargets(root, paralelas)
	out.Targets = targets
	out.Messages = append(out.Messages,
		"[pulumi-archive] step 1: pre-flight",
		"[pulumi-archive] step 2: enumerating paralelas Pulumi dirs",
		fmt.Sprintf("[pulumi-archive]   %d dirs to archive: %s", len(targets), strings.Join(targets, " ")))
	if len(targets) == 0 {
		out.Pass = true
		out.Messages = append(out.Messages,
			"[pulumi-archive]   no paralelas dirs found — Phase 3 already cleaned",
			"[pulumi-archive] DONE — nothing to do")
		return out, nil
	}

	tmpDir := cfg.TmpDir
	if tmpDir == "" {
		var err error
		tmpDir, err = os.MkdirTemp("", "pulumi-archive-*")
		if err != nil {
			return nil, &loopzeroerrors.LoopZeroError{
				Code:    "OPS_USAGE",
				Message: fmt.Sprintf("pulumi-archive: mkdir tmp: %v", err),
				Cause:   err,
			}
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()
	}

	ctx := context.Background()
	out.Messages = append(out.Messages, "[pulumi-archive] step 3: per-dir archive")

	for _, dir := range targets {
		out.Messages = append(out.Messages, fmt.Sprintf("[pulumi-archive]   --- %s ---", dir))
		stacks, err := listStacks(ctx, runner, root, dir)
		if err != nil {
			out.Messages = append(out.Messages,
				fmt.Sprintf("[pulumi-archive]     WARN: pulumi stack ls failed (auth?); skipping %s", dir))
			continue
		}
		if len(stacks) == 0 {
			out.Messages = append(out.Messages,
				fmt.Sprintf("[pulumi-archive]     no stacks in %s", dir))
			continue
		}
		for _, stack := range stacks {
			out.Messages = append(out.Messages, fmt.Sprintf("[pulumi-archive]     stack: %s", stack))

			resourceCount := stackResourceCount(ctx, runner, root, dir, stack)
			if resourceCount > 0 {
				out.Messages = append(out.Messages,
					fmt.Sprintf("[pulumi-archive]     LIVE RESOURCES detected in %s/%s: %d resources",
						dir, stack, resourceCount),
					"[pulumi-archive]     Per condition C3, operator must verify against actual provider before T3.2 stack rm.")
				out.LiveResourceCount++
			}

			archivePath := filepath.Join(tmpDir,
				fmt.Sprintf("%s-%s-%s.json", archivePrefix(dir), stack, ts))
			if cfg.DryRun {
				out.Messages = append(out.Messages,
					fmt.Sprintf("[pulumi-archive]     DRY_RUN: would export to %s then upload to S3", archivePath))
				continue
			}

			if err := exportStack(ctx, runner, root, dir, stack, archivePath); err != nil {
				return nil, &loopzeroerrors.LoopZeroError{
					Code:    "OPS_DEPLOY_FAILURE",
					Message: fmt.Sprintf("pulumi-archive: %v", err),
					Cause:   err,
				}
			}
			hits, err := scanPlaintextSecrets(archivePath)
			if err != nil {
				return nil, &loopzeroerrors.LoopZeroError{
					Code:    "OPS_DEPLOY_FAILURE",
					Message: fmt.Sprintf("pulumi-archive: C9 scan %s: %v", archivePath, err),
					Cause:   err,
				}
			}
			if hits > 0 {
				out.Messages = append(out.Messages,
					fmt.Sprintf("[pulumi-archive]     WARN: %d plaintext token candidates in archive (C9 regex scan)", hits),
					fmt.Sprintf("[pulumi-archive]     Operator MUST review %s before C5 upload.", archivePath))
			}
			sha, err := hashFile(archivePath)
			if err != nil {
				return nil, &loopzeroerrors.LoopZeroError{
					Code:    "OPS_DEPLOY_FAILURE",
					Message: fmt.Sprintf("pulumi-archive: sha256 %s: %v", archivePath, err),
					Cause:   err,
				}
			}
			out.Messages = append(out.Messages,
				fmt.Sprintf("[pulumi-archive]     archive: %s sha256=%s (resources=%d)",
					filepath.Base(archivePath), sha, resourceCount))

			s3Key := fmt.Sprintf("s3://%s/pulumi-state/%s/%s/%s.json",
				bucket, archivePrefix(dir), stack, ts)
			if err := uploadS3(ctx, runner, archivePath, s3Key, sha, resourceCount); err != nil {
				return nil, &loopzeroerrors.LoopZeroError{
					Code:    "OPS_DEPLOY_FAILURE",
					Message: fmt.Sprintf("pulumi-archive: %v", err),
					Cause:   err,
				}
			}

			out.Archived = append(out.Archived, Archived{
				Dir:           dir,
				Stack:         stack,
				S3Key:         s3Key,
				SHA256:        sha,
				ResourceCount: resourceCount,
				PlaintextHits: hits,
			})
		}
	}

	out.Messages = append(out.Messages,
		"[pulumi-archive] step 4: summary",
		fmt.Sprintf("[pulumi-archive]   archived: %d", len(out.Archived)),
		fmt.Sprintf("[pulumi-archive]   live-resources detected (operator must verify): %d", out.LiveResourceCount))
	if out.LiveResourceCount > 0 {
		out.Pass = false
		out.Messages = append(out.Messages,
			fmt.Sprintf("[pulumi-archive] WARN: live resources in %d stack(s) — DO NOT proceed to T3.2 stack rm without operator verification",
				out.LiveResourceCount))
		return out, nil
	}
	out.Pass = true
	out.Messages = append(out.Messages, "[pulumi-archive] DONE")
	return out, nil
}
