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
//	DEST_REGISTRY=harbor.dev.loopzero.example/loopzero-platform \
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

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	slog.Info("registry-mirror starting",
		"source", cfg.sourceRegistry,
		"dest", cfg.destRegistry,
		"dry_run", cfg.dryRun,
		"concurrency", cfg.concurrency,
	)

	// Verify crane is available.
	if _, err := exec.LookPath("crane"); err != nil {
		slog.Error("crane not found on PATH — install from https://github.com/google/go-containerregistry/tree/main/cmd/crane", "error", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	start := time.Now()

	// Step 1: List all repositories in the source registry.
	slog.Info("step 1/4: listing repositories", "registry", cfg.sourceRegistry)
	repos, err := craneCatalog(ctx, cfg.sourceRegistry)
	if err != nil {
		slog.Error("failed to list repositories", "registry", cfg.sourceRegistry, "error", err)
		os.Exit(2)
	}
	slog.Info("repositories found", "count", len(repos))

	if len(repos) == 0 {
		slog.Info("no repositories found — nothing to mirror")
		printReport(0, 0, 0, time.Since(start))
		os.Exit(0)
	}

	// Step 2: For each repository, list all tags.
	slog.Info("step 2/4: listing tags for all repositories")
	type imageRef struct {
		repo string
		tag  string
	}
	var images []imageRef

	for _, repo := range repos {
		fullRepo := cfg.sourceRegistry + "/" + repo
		tags, err := craneLs(ctx, fullRepo)
		if err != nil {
			slog.Error("failed to list tags", "repo", fullRepo, "error", err)
			continue
		}
		slog.Info("tags found", "repo", repo, "count", len(tags))
		for _, tag := range tags {
			images = append(images, imageRef{repo: repo, tag: tag})
		}
	}

	slog.Info("total images to mirror", "count", len(images))

	if len(images) == 0 {
		slog.Info("no images found — nothing to mirror")
		printReport(0, 0, 0, time.Since(start))
		os.Exit(0)
	}

	if cfg.dryRun {
		slog.Info("dry run — listing images that would be copied")
		for _, img := range images {
			src := cfg.sourceRegistry + "/" + img.repo + ":" + img.tag
			dst := cfg.destRegistry + "/" + img.repo + ":" + img.tag
			slog.Info("would copy", "source", src, "dest", dst)
		}
		printReport(len(images), 0, 0, time.Since(start))
		os.Exit(0)
	}

	// Step 3: Copy each image with concurrency control.
	slog.Info("step 3/4: copying images", "concurrency", cfg.concurrency)

	var (
		successCount atomic.Int64
		failCount    atomic.Int64
		wg           sync.WaitGroup
		sem          = make(chan struct{}, cfg.concurrency)
	)

	for _, img := range images {
		wg.Add(1)
		go func(img imageRef) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			src := cfg.sourceRegistry + "/" + img.repo + ":" + img.tag
			dst := cfg.destRegistry + "/" + img.repo + ":" + img.tag

			slog.Info("copying image", "source", src, "dest", dst)

			if err := craneCopy(ctx, src, dst); err != nil {
				slog.Error("failed to copy image", "source", src, "dest", dst, "error", err)
				failCount.Add(1)
				return
			}

			// Step 4 (per-image): Verify digest matches.
			srcDigest, err := craneDigest(ctx, src)
			if err != nil {
				slog.Error("failed to get source digest", "image", src, "error", err)
				failCount.Add(1)
				return
			}

			dstDigest, err := craneDigest(ctx, dst)
			if err != nil {
				slog.Error("failed to get destination digest", "image", dst, "error", err)
				failCount.Add(1)
				return
			}

			if srcDigest != dstDigest {
				slog.Error("digest mismatch",
					"source", src, "source_digest", srcDigest,
					"dest", dst, "dest_digest", dstDigest,
				)
				failCount.Add(1)
				return
			}

			slog.Info("image copied and verified", "image", img.repo+":"+img.tag, "digest", srcDigest)
			successCount.Add(1)
		}(img)
	}

	wg.Wait()

	duration := time.Since(start)
	succeeded := int(successCount.Load())
	failed := int(failCount.Load())

	slog.Info("step 4/4: mirror complete")
	printReport(succeeded, failed, len(images), duration)

	if failed > 0 && succeeded > 0 {
		os.Exit(1) // partial failure
	}
	if failed > 0 && succeeded == 0 {
		os.Exit(2) // total failure
	}
}

// config holds environment-driven configuration.
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

// craneCatalog lists all repositories in a registry.
func craneCatalog(ctx context.Context, registry string) ([]string, error) {
	out, err := runCrane(ctx, "catalog", registry)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// craneLs lists all tags for a repository.
func craneLs(ctx context.Context, repo string) ([]string, error) {
	out, err := runCrane(ctx, "ls", repo)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// craneCopy copies an image from source to destination.
func craneCopy(ctx context.Context, src, dst string) error {
	_, err := runCrane(ctx, "copy", src, dst)
	return err
}

// craneDigest returns the digest of an image.
func craneDigest(ctx context.Context, ref string) (string, error) {
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
