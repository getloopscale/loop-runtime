// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// registry-mirror copies all container images from a source OCI registry to a
// destination registry using the crane CLI tool.
//
// It lists all repositories and tags from the source, copies each image to the
// destination with the same repository path and tag, and verifies that digests
// match after copy. A final report summarizes total images, failures, and
// duration.
//
// Prerequisites:
//
//   - crane must be on PATH (https://github.com/google/go-containerregistry/tree/main/cmd/crane)
//   - Docker credentials configured for both registries (crane uses ~/.docker/config.json)
//
// Usage:
//
//	SOURCE_REGISTRY=registry.digitalocean.com/loopzero-system \
//	DEST_REGISTRY=registry.example.com/loopzero-platform \
//	go run infra/scripts/registry-mirror/main.go
//
// Exit codes:
//
//	0 — all images copied and verified successfully
//	1 — partial failure (some images failed to copy or verify)
//	2 — total failure (configuration error or no images copied)
package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Exit codes (see the package comment).
const (
	exitOK      = 0
	exitPartial = 1
	exitTotal   = 2
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(exitTotal)
	}
	if _, err := exec.LookPath("crane"); err != nil {
		slog.Error("crane not found on PATH — install from https://github.com/google/go-containerregistry/tree/main/cmd/crane", "error", err)
		os.Exit(exitTotal)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	code := mirror(ctx, cfg, execCrane{})
	cancel()
	os.Exit(code)
}

// craneClient is the four crane operations the mirror uses.
type craneClient interface {
	Catalog(ctx context.Context, registry string) ([]string, error)
	Ls(ctx context.Context, repo string) ([]string, error)
	Copy(ctx context.Context, src, dst string) error
	Digest(ctx context.Context, ref string) (string, error)
}

type imageRef struct {
	repo string
	tag  string
}

// mirror copies every image of the source registry and returns the exit code.
// A repository whose tags cannot be listed counts as a failure: its images
// were not mirrored, and reporting success would hide that.
func mirror(ctx context.Context, cfg config, c craneClient) int {
	start := time.Now()
	slog.Info("registry-mirror starting",
		"source", cfg.sourceRegistry, "dest", cfg.destRegistry,
		"dry_run", cfg.dryRun, "concurrency", cfg.concurrency)

	repos, err := c.Catalog(ctx, cfg.sourceRegistry)
	if err != nil {
		slog.Error("failed to list repositories", "registry", cfg.sourceRegistry, "error", err)
		return exitTotal
	}
	if len(repos) == 0 {
		slog.Info("no repositories found — nothing to mirror")
		printReport(0, 0, 0, time.Since(start))
		return exitOK
	}

	images, listFailures := listImages(ctx, cfg, c, repos)
	if cfg.dryRun {
		logPlannedCopies(cfg, images)
		printReport(len(images), listFailures, len(images), time.Since(start))
		return exitCode(len(images), listFailures)
	}

	succeeded, copyFailures := copyAll(ctx, cfg, c, images)
	failed := listFailures + copyFailures
	printReport(succeeded, failed, len(images), time.Since(start))
	return exitCode(succeeded, failed)
}

// listImages lists the tags of every repository. It returns the images found
// and how many repositories could not be listed.
func listImages(ctx context.Context, cfg config, c craneClient, repos []string) ([]imageRef, int) {
	var images []imageRef
	failures := 0
	for _, repo := range repos {
		fullRepo := cfg.sourceRegistry + "/" + repo
		tags, err := c.Ls(ctx, fullRepo)
		if err != nil {
			slog.Error("failed to list tags; the repository will not be mirrored", "repo", fullRepo, "error", err)
			failures++
			continue
		}
		for _, tag := range tags {
			images = append(images, imageRef{repo: repo, tag: tag})
		}
	}
	slog.Info("images to mirror", "count", len(images), "unlisted_repositories", failures)
	return images, failures
}

func logPlannedCopies(cfg config, images []imageRef) {
	for _, img := range images {
		src, dst := refs(cfg, img)
		slog.Info("would copy", "source", src, "dest", dst)
	}
}

// copyAll copies and verifies every image with bounded concurrency, returning
// how many succeeded and how many failed.
func copyAll(ctx context.Context, cfg config, c craneClient, images []imageRef) (int, int) {
	var (
		succeeded, failed atomic.Int64
		wg                sync.WaitGroup
		sem               = make(chan struct{}, cfg.concurrency)
	)
	for _, img := range images {
		wg.Add(1)
		go func(img imageRef) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := copyAndVerify(ctx, cfg, c, img); err != nil {
				slog.Error("image not mirrored", "image", img.repo+":"+img.tag, "error", err)
				failed.Add(1)
				return
			}
			succeeded.Add(1)
		}(img)
	}
	wg.Wait()
	return int(succeeded.Load()), int(failed.Load())
}

// copyAndVerify copies one image and checks the destination digest matches.
func copyAndVerify(ctx context.Context, cfg config, c craneClient, img imageRef) error {
	src, dst := refs(cfg, img)
	if err := c.Copy(ctx, src, dst); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	srcDigest, err := c.Digest(ctx, src)
	if err != nil {
		return fmt.Errorf("source digest of %s: %w", src, err)
	}
	dstDigest, err := c.Digest(ctx, dst)
	if err != nil {
		return fmt.Errorf("destination digest of %s: %w", dst, err)
	}
	if srcDigest != dstDigest {
		return fmt.Errorf("digest mismatch: %s is %s, %s is %s", src, srcDigest, dst, dstDigest)
	}
	slog.Info("image copied and verified", "image", img.repo+":"+img.tag, "digest", srcDigest)
	return nil
}

func refs(cfg config, img imageRef) (string, string) {
	suffix := "/" + img.repo + ":" + img.tag
	return cfg.sourceRegistry + suffix, cfg.destRegistry + suffix
}

// exitCode is 0 when nothing failed, 1 when some images were mirrored and
// some were not, and 2 when nothing was.
func exitCode(succeeded, failed int) int {
	switch {
	case failed == 0:
		return exitOK
	case succeeded > 0:
		return exitPartial
	default:
		return exitTotal
	}
}

type config struct {
	sourceRegistry string
	destRegistry   string
	dryRun         bool
	concurrency    int
}

func loadConfig() (config, error) {
	c := config{
		sourceRegistry: os.Getenv("SOURCE_REGISTRY"),
		destRegistry:   os.Getenv("DEST_REGISTRY"),
	}
	if c.sourceRegistry == "" {
		return c, fmt.Errorf("SOURCE_REGISTRY is required")
	}
	if c.destRegistry == "" {
		return c, fmt.Errorf("DEST_REGISTRY is required")
	}

	c.dryRun = os.Getenv("DRY_RUN") == "true"

	c.concurrency = 5
	if v := os.Getenv("CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("CONCURRENCY must be a positive integer: %w", err)
		}
		if n < 1 {
			return c, fmt.Errorf("CONCURRENCY must be >= 1, got %d", n)
		}
		c.concurrency = n
	}

	return c, nil
}

// printReport logs a summary of the mirror operation.
func printReport(succeeded, failed, total int, duration time.Duration) {
	slog.Info("mirror report",
		"total_images", total,
		"succeeded", succeeded,
		"failed", failed,
		"duration", duration.Round(time.Millisecond).String(),
	)
}

// Catalog lists all repositories in a registry.
// execCrane runs the crane CLI.
type execCrane struct{}

func (execCrane) Catalog(ctx context.Context, registry string) ([]string, error) {
	out, err := runCrane(ctx, "catalog", registry)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// Ls lists all tags for a repository.
func (execCrane) Ls(ctx context.Context, repo string) ([]string, error) {
	out, err := runCrane(ctx, "ls", repo)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// Copy copies an image from source to destination.
func (execCrane) Copy(ctx context.Context, src, dst string) error {
	_, err := runCrane(ctx, "copy", src, dst)
	return err
}

// Digest returns the digest of an image.
func (execCrane) Digest(ctx context.Context, ref string) (string, error) {
	out, err := runCrane(ctx, "digest", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// runCrane executes a crane subcommand and returns stdout.
func runCrane(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "crane", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("crane %s failed: %w\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}

	return stdout.String(), nil
}

// splitLines splits output into non-empty lines.
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
