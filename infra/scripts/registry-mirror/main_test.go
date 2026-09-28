// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeCrane answers the four crane calls from maps. A missing entry is an
// error, so a test states every call it expects to succeed.
type fakeCrane struct {
	repos      []string
	catalogErr error
	tags       map[string][]string // full repo -> tags; absent -> listing error
	copyFails  map[string]bool     // source ref -> copy error
	digests    map[string]string   // ref -> digest; absent -> digest error

	mu     sync.Mutex
	copied []string
}

func (f *fakeCrane) Catalog(context.Context, string) ([]string, error) {
	return f.repos, f.catalogErr
}

func (f *fakeCrane) Ls(_ context.Context, repo string) ([]string, error) {
	tags, ok := f.tags[repo]
	if !ok {
		return nil, errors.New("unauthorized: " + repo)
	}
	return tags, nil
}

func (f *fakeCrane) Copy(_ context.Context, src, _ string) error {
	if f.copyFails[src] {
		return errors.New("copy refused: " + src)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.copied = append(f.copied, src)
	return nil
}

func (f *fakeCrane) Digest(_ context.Context, ref string) (string, error) {
	d, ok := f.digests[ref]
	if !ok {
		return "", errors.New("no digest: " + ref)
	}
	return d, nil
}

var mirrorCfg = config{sourceRegistry: "src.example", destRegistry: "dst.example", concurrency: 2}

func healthyCrane() *fakeCrane {
	return &fakeCrane{
		repos: []string{"api", "web"},
		tags:  map[string][]string{"src.example/api": {"v1"}, "src.example/web": {"v2"}},
		digests: map[string]string{
			"src.example/api:v1": "sha256:a", "dst.example/api:v1": "sha256:a",
			"src.example/web:v2": "sha256:w", "dst.example/web:v2": "sha256:w",
		},
	}
}

func TestMirror_ExitsZeroWhenEveryImageIsCopiedAndVerified(t *testing.T) {
	if got := mirror(context.Background(), mirrorCfg, healthyCrane()); got != exitOK {
		t.Fatalf("exit = %d, want %d", got, exitOK)
	}
}

// A repository whose tags cannot be listed is a repository that was not
// mirrored. It used to be skipped without counting, so the run exited 0.
func TestMirror_ARepositoryThatCannotBeListedIsAPartialFailure(t *testing.T) {
	c := healthyCrane()
	delete(c.tags, "src.example/web")

	got := mirror(context.Background(), mirrorCfg, c)

	if got != exitPartial {
		t.Fatalf("exit = %d, want %d (partial failure)", got, exitPartial)
	}
}

// When no repository can be listed nothing was mirrored; that used to read as
// "nothing to mirror" and exit 0.
func TestMirror_NoListableRepositoryIsATotalFailure(t *testing.T) {
	c := healthyCrane()
	c.tags = map[string][]string{}

	if got := mirror(context.Background(), mirrorCfg, c); got != exitTotal {
		t.Fatalf("exit = %d, want %d (total failure)", got, exitTotal)
	}
}

func TestMirror_AnEmptySourceRegistryHasNothingToMirror(t *testing.T) {
	c := healthyCrane()
	c.repos = nil

	if got := mirror(context.Background(), mirrorCfg, c); got != exitOK {
		t.Fatalf("exit = %d, want %d", got, exitOK)
	}
}

func TestMirror_ACatalogFailureIsATotalFailure(t *testing.T) {
	c := healthyCrane()
	c.catalogErr = errors.New("registry down")

	if got := mirror(context.Background(), mirrorCfg, c); got != exitTotal {
		t.Fatalf("exit = %d, want %d", got, exitTotal)
	}
}

func TestMirror_ADigestMismatchCountsAsAFailure(t *testing.T) {
	c := healthyCrane()
	c.digests["dst.example/web:v2"] = "sha256:other"

	if got := mirror(context.Background(), mirrorCfg, c); got != exitPartial {
		t.Fatalf("exit = %d, want %d", got, exitPartial)
	}
}

func TestMirror_EveryCopyFailingIsATotalFailure(t *testing.T) {
	c := healthyCrane()
	c.copyFails = map[string]bool{"src.example/api:v1": true, "src.example/web:v2": true}

	if got := mirror(context.Background(), mirrorCfg, c); got != exitTotal {
		t.Fatalf("exit = %d, want %d", got, exitTotal)
	}
}

// A dry run copies nothing, but it still reports a repository it could not
// list: the plan it prints is incomplete.
func TestMirror_DryRunCopiesNothingAndReportsListingFailures(t *testing.T) {
	c := healthyCrane()
	delete(c.tags, "src.example/web")
	cfg := mirrorCfg
	cfg.dryRun = true

	got := mirror(context.Background(), cfg, c)

	if len(c.copied) != 0 {
		t.Fatalf("dry run copied %v", c.copied)
	}
	if got != exitPartial {
		t.Fatalf("exit = %d, want %d", got, exitPartial)
	}
}

func TestSplitLines_DropsBlankLinesAndSurroundingSpace(t *testing.T) {
	got := splitLines("\n a \n\n b\n")
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("got %q", got)
	}
}
