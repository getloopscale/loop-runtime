// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package pulumirequiredsecrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// buildFixture creates the minimum repo layout to satisfy the invariant.
// Every required prod/staging/drill key is present either as a config entry
// or as a comment, and the SoT doc backticks each prod key.
func buildFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, root, prodStackPath,
		"config:\n"+
			"  # Required secrets (may be encrypted-placeholder or omitted):\n"+
			"  # spacesAccessKeyId — DO Spaces access\n"+
			"  # spacesSecretAccessKey — DO Spaces secret\n"+
			"  # digitaloceanToken — DO API\n"+
			"  # cfApiToken — Cloudflare Zone + Worker\n"+
			"  # giteaAdminPassword — Gitea admin\n"+
			"  # minioRootPassword — MinIO root\n",
	)
	writeFile(t, root, stagingStackPath,
		"config:\n"+
			"  # spacesAccessKeyId — DO Spaces\n"+
			"  # spacesSecretAccessKey — DO Spaces\n"+
			"  # digitaloceanToken — DO API\n"+
			"  # cfApiToken — Cloudflare\n",
	)
	writeFile(t, root, drillStackPath,
		"config:\n"+
			"  cfAccountId: some-account\n"+
			"  spacesAccessId: some-id\n"+
			"  spacesSecretKey: some-key\n",
	)
	writeFile(t, root, sotDocPath,
		"# Credentials Required\n\n"+
			"| Key | Purpose |\n"+
			"|---|---|\n"+
			"| `spacesAccessKeyId` | DO Spaces access |\n"+
			"| `spacesSecretAccessKey` | DO Spaces secret |\n"+
			"| `digitaloceanToken` | DO API |\n"+
			"| `cfApiToken` | Cloudflare |\n"+
			"| `giteaAdminPassword` | Gitea admin |\n"+
			"| `minioRootPassword` | MinIO |\n",
	)
	return root
}

func TestCheck_CanonicalPasses(t *testing.T) {
	root := buildFixture(t)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_ProdMissingKeyFails(t *testing.T) {
	root := buildFixture(t)
	// Rewrite prod stack without giteaAdminPassword.
	writeFile(t, root, prodStackPath,
		"config:\n"+
			"  # spacesAccessKeyId\n"+
			"  # spacesSecretAccessKey\n"+
			"  # digitaloceanToken\n"+
			"  # cfApiToken\n"+
			"  # minioRootPassword\n",
	)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when prod stack drops a key, got %v", err)
	}
}

func TestCheck_SotDocDropsKeyFails(t *testing.T) {
	root := buildFixture(t)
	// Rewrite SoT dropping the backtick around cfApiToken (still bare mention — insufficient).
	writeFile(t, root, sotDocPath,
		"# Credentials Required\n\n"+
			"| `spacesAccessKeyId` |\n"+
			"| `spacesSecretAccessKey` |\n"+
			"| `digitaloceanToken` |\n"+
			"| cfApiToken |\n"+ // <- bare, no backticks
			"| `giteaAdminPassword` |\n"+
			"| `minioRootPassword` |\n",
	)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when SoT drops backticks around a required key, got %v", err)
	}
}

func TestCheck_ProdStackMissingFails(t *testing.T) {
	root := t.TempDir()
	// Only SoT + staging present; no prod stack file.
	writeFile(t, root, stagingStackPath, "config:\n")
	writeFile(t, root, sotDocPath, "# CR\n\n`spacesAccessKeyId` `spacesSecretAccessKey` `digitaloceanToken` `cfApiToken` `giteaAdminPassword` `minioRootPassword`\n")
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when prod stack missing, got %v", err)
	}
}

func TestCheck_DrillStackTolerantWhenAbsent(t *testing.T) {
	root := buildFixture(t)
	// Delete drill stack — this MUST NOT fail (bash tolerates absence via `-f` guard).
	if err := os.Remove(filepath.Join(root, drillStackPath)); err != nil {
		t.Fatalf("remove drill: %v", err)
	}
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS when drill stack absent (tolerant), got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	abs := qtestutil.RootFile(t, prodStackPath)
	outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
	if checkErr != nil {
		t.Fatalf("real repo must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
	}
}
