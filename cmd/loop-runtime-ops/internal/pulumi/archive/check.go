// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package archive exports Pulumi stack state for parallel project dirs to
// encrypted S3 (s3://loop-runtime-archive/pulumi-state/). Pre-flight verifies
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

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/shellrun"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// PulumiVersionPinned — bash PULUMI_VERSION_PINNED.
const PulumiVersionPinned = "3.140.0"

// DefaultArchiveBucket — S3 bucket holding the pulumi state archives.
const DefaultArchiveBucket = "loop-runtime-archive"

// DefaultParallelProjects — pulumi parallel project dirs subject to archive.
var DefaultParallelProjects = []string{
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

	// ParallelProjects overrides DefaultParallelProjects.
	ParallelProjects []string

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

	// AcceptPlaintextHits uploads an archive even when the C9 scan found
	// plaintext-secret candidates. Without it such an archive is held for
	// operator review and the run does not pass.
	AcceptPlaintextHits bool
}

// Outcome captures the archive run result.
//
// Pass is true only when every stack of every target was listed, its live
// resource count was actually measured as zero, and its archive was uploaded.
// Anything the run could not establish keeps Pass false: this is the
// pre-flight for `pulumi stack rm`, so an unknown must never read as safe.
type Outcome struct {
	Pass              bool
	Targets           []string
	Archived          []Archived
	LiveResourceCount int
	// UnlistedDirs are target dirs whose stacks could not be listed.
	UnlistedDirs []string
	// UnverifiedStacks ("dir/stack") had no measurable resource count.
	UnverifiedStacks []string
	// Held are exports withheld from upload because the C9 scan found
	// plaintext-secret candidates and AcceptPlaintextHits was not set.
	Held     []Archived
	Messages []string
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
		return &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: "pulumi-archive: PULUMI_CONFIG_PASSPHRASE env var required (C5)",
		}
	}
	if env("AWS_ACCESS_KEY_ID") == "" {
		return &loopruntimeerrors.LoopRuntimeError{
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
		return &loopruntimeerrors.LoopRuntimeError{
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
		return &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("pulumi-archive: bucket %s encryption query failed (C4 requires aws:kms)", bucket),
		}
	}
	algo, err := extractSSEAlgo(encRes.Stdout)
	if err != nil {
		return &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: fmt.Sprintf("pulumi-archive: parse encryption response: %v", err),
			Cause:   err,
		}
	}
	if algo != "aws:kms" {
		return &loopruntimeerrors.LoopRuntimeError{
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

// enumerateTargets returns parallel project dirs that have a Pulumi.yaml.
func enumerateTargets(root string, parallelProjects []string) []string {
	var out []string
	for _, dir := range parallelProjects {
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

// stackResourceCount parses `pulumi stack --stack X --json`. A failed or
// unparseable query is an error, never zero: zero live resources is the answer
// that clears a stack for removal.
func stackResourceCount(ctx context.Context, runner shellrun.Runner, repoRoot, dir, stack string) (int, error) {
	res, err := runner.Run(ctx, "pulumi", []string{
		"stack", "--stack", stack, "--json",
	}, shellrun.WithCwd(filepath.Join(repoRoot, dir)))
	if err != nil {
		return 0, fmt.Errorf("pulumi stack --stack %s (dir=%s): %w", stack, dir, err)
	}
	if res.ExitCode != 0 {
		return 0, fmt.Errorf("pulumi stack --stack %s (dir=%s): exit %d: %s", stack, dir, res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	var inf struct {
		Resources []json.RawMessage `json:"resources"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &inf); err != nil {
		return 0, fmt.Errorf("parse pulumi stack --stack %s (dir=%s): %w", stack, dir, err)
	}
	return len(inf.Resources), nil
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
	parallelProjects := cfg.ParallelProjects
	if parallelProjects == nil {
		parallelProjects = DefaultParallelProjects
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
	targets := enumerateTargets(root, parallelProjects)
	out.Targets = targets
	out.Messages = append(out.Messages,
		"[pulumi-archive] step 1: pre-flight",
		"[pulumi-archive] step 2: enumerating the parallel Pulumi project dirs",
		fmt.Sprintf("[pulumi-archive]   %d dirs to archive: %s", len(targets), strings.Join(targets, " ")))
	if len(targets) == 0 {
		out.Pass = true
		out.Messages = append(out.Messages,
			"[pulumi-archive]   no parallel project dirs found — Phase 3 already cleaned",
			"[pulumi-archive] DONE — nothing to do")
		return out, nil
	}

	tmpDir := cfg.TmpDir
	if tmpDir == "" {
		var err error
		tmpDir, err = os.MkdirTemp("", "pulumi-archive-*")
		if err != nil {
			return nil, &loopruntimeerrors.LoopRuntimeError{
				Code:    "OPS_USAGE",
				Message: fmt.Sprintf("pulumi-archive: mkdir tmp: %v", err),
				Cause:   err,
			}
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				fmt.Fprintf(os.Stderr, "[pulumi-archive] WARN: could not remove temp dir %s: %v\n", tmpDir, err)
			}
		}()
	}

	a := archiver{ctx: context.Background(), cfg: cfg, runner: runner, root: root, bucket: bucket, ts: ts, tmpDir: tmpDir, out: out}
	out.Messages = append(out.Messages, "[pulumi-archive] step 3: per-dir archive")
	for _, dir := range targets {
		if err := a.archiveDir(dir); err != nil {
			return nil, err
		}
	}
	a.summarise()
	return out, nil
}

// archiver carries one run's inputs through the per-dir / per-stack steps.
type archiver struct {
	ctx    context.Context
	cfg    Config
	runner shellrun.Runner
	root   string
	bucket string
	ts     string
	tmpDir string
	out    *Outcome
}

func (a *archiver) say(format string, args ...any) {
	a.out.Messages = append(a.out.Messages, fmt.Sprintf("[pulumi-archive]"+format, args...))
}

func (a *archiver) archiveDir(dir string) error {
	a.say("   --- %s ---", dir)
	stacks, err := listStacks(a.ctx, a.runner, a.root, dir)
	if err != nil {
		a.out.UnlistedDirs = append(a.out.UnlistedDirs, dir)
		a.say("     ERROR: cannot list stacks in %s — nothing archived there: %v", dir, err)
		return nil
	}
	if len(stacks) == 0 {
		a.say("     no stacks in %s", dir)
		return nil
	}
	for _, stack := range stacks {
		if err := a.archiveStack(dir, stack); err != nil {
			return err
		}
	}
	return nil
}

func (a *archiver) archiveStack(dir, stack string) error {
	a.say("     stack: %s", stack)
	resourceCount, err := stackResourceCount(a.ctx, a.runner, a.root, dir, stack)
	switch {
	case err != nil:
		a.out.UnverifiedStacks = append(a.out.UnverifiedStacks, dir+"/"+stack)
		a.say("     ERROR: live-resource count unknown for %s/%s: %v", dir, stack, err)
	case resourceCount > 0:
		a.out.LiveResourceCount++
		a.say("     LIVE RESOURCES detected in %s/%s: %d resources", dir, stack, resourceCount)
		a.say("     Per condition C3, operator must verify against actual provider before T3.2 stack rm.")
	}

	archivePath := filepath.Join(a.tmpDir, fmt.Sprintf("%s-%s-%s.json", archivePrefix(dir), stack, a.ts))
	if a.cfg.DryRun {
		a.say("     DRY_RUN: would export to %s then upload to S3", archivePath)
		return nil
	}
	if err := exportStack(a.ctx, a.runner, a.root, dir, stack, archivePath); err != nil {
		return deployFailure(fmt.Sprintf("pulumi-archive: %v", err), err)
	}
	hits, err := scanPlaintextSecrets(archivePath)
	if err != nil {
		return deployFailure(fmt.Sprintf("pulumi-archive: C9 scan %s: %v", archivePath, err), err)
	}
	sha, err := hashFile(archivePath)
	if err != nil {
		return deployFailure(fmt.Sprintf("pulumi-archive: sha256 %s: %v", archivePath, err), err)
	}
	a.say("     archive: %s sha256=%s (resources=%d)", filepath.Base(archivePath), sha, resourceCount)

	s3Key := fmt.Sprintf("s3://%s/pulumi-state/%s/%s/%s.json", a.bucket, archivePrefix(dir), stack, a.ts)
	record := Archived{Dir: dir, Stack: stack, S3Key: s3Key, SHA256: sha, ResourceCount: resourceCount, PlaintextHits: hits}
	if hits > 0 && !a.cfg.AcceptPlaintextHits {
		a.out.Held = append(a.out.Held, record)
		a.say("     HELD: %d plaintext token candidates in archive (C9 regex scan) — not uploaded.", hits)
		a.say("     Review the export, then re-run with --accept-plaintext-hits to upload it.")
		return nil
	}
	if hits > 0 {
		a.say("     WARN: uploading with %d plaintext token candidates (accepted by operator)", hits)
	}
	if err := uploadS3(a.ctx, a.runner, archivePath, s3Key, sha, resourceCount); err != nil {
		return deployFailure(fmt.Sprintf("pulumi-archive: %v", err), err)
	}
	a.out.Archived = append(a.out.Archived, record)
	return nil
}

// summarise closes the run: Pass is true only when nothing is unknown, held
// or live.
func (a *archiver) summarise() {
	out := a.out
	a.say(" step 4: summary")
	a.say("   archived: %d", len(out.Archived))
	a.say("   held for plaintext review: %d", len(out.Held))
	a.say("   dirs not listed: %d; stacks with unknown resource count: %d", len(out.UnlistedDirs), len(out.UnverifiedStacks))
	a.say("   live-resources detected (operator must verify): %d", out.LiveResourceCount)
	blocked := out.LiveResourceCount > 0 || len(out.UnlistedDirs) > 0 || len(out.UnverifiedStacks) > 0 || len(out.Held) > 0
	if blocked {
		out.Pass = false
		a.say(" NOT CLEAR — DO NOT proceed to T3.2 stack rm until every listed item above is resolved")
		return
	}
	out.Pass = true
	a.say(" DONE")
}

func deployFailure(msg string, cause error) error {
	return &loopruntimeerrors.LoopRuntimeError{Code: "OPS_DEPLOY_FAILURE", Message: msg, Cause: cause}
}
