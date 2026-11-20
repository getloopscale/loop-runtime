// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package archive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/shellrun"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func mkRepo(t *testing.T, parallelProjects []string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range parallelProjects {
		full := filepath.Join(root, dir, "Pulumi.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("name: t\nruntime: nodejs\n"), 0o600))
	}
	return root
}

// =============================================================================
// preflight
// =============================================================================

func TestPreflight_DryRunBypasses(t *testing.T) {
	require.NoError(t, preflight(Config{DryRun: true}, shellrun.NewMock()))
}

func TestPreflight_MissingPassphrase(t *testing.T) {
	err := preflight(Config{Env: mapEnv(map[string]string{})}, shellrun.NewMock())
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
	assert.Contains(t, te.Message, "PULUMI_CONFIG_PASSPHRASE")
}

func TestPreflight_MissingAWSCreds(t *testing.T) {
	err := preflight(Config{
		Env: mapEnv(map[string]string{"PULUMI_CONFIG_PASSPHRASE": "x"}),
	}, shellrun.NewMock())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AWS_ACCESS_KEY_ID")
}

func TestPreflight_ArchiveOnlyBypassesBucketCheck(t *testing.T) {
	require.NoError(t, preflight(Config{
		ArchiveOnly: true,
		Env: mapEnv(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": "x",
			"AWS_ACCESS_KEY_ID":        "y",
		}),
	}, shellrun.NewMock()))
}

func TestPreflight_BucketMissing(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 1, Stderr: "404"})
	err := preflight(Config{
		Env: mapEnv(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": "x",
			"AWS_ACCESS_KEY_ID":        "y",
		}),
	}, m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestPreflight_BucketWrongEncryption(t *testing.T) {
	m := shellrun.NewMock()
	m.SetResponse(`aws s3api head-bucket --bucket loopzero-archive`,
		&shellrun.Result{ExitCode: 0})
	m.SetResponse(`aws s3api get-bucket-encryption --bucket loopzero-archive`,
		&shellrun.Result{
			ExitCode: 0,
			Stdout:   `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}}`,
		})
	err := preflight(Config{
		Env: mapEnv(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": "x",
			"AWS_ACCESS_KEY_ID":        "y",
		}),
	}, m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing SSE-KMS")
}

func TestPreflight_BucketKMSPassesPreflight(t *testing.T) {
	m := shellrun.NewMock()
	m.SetResponse(`aws s3api head-bucket --bucket loopzero-archive`,
		&shellrun.Result{ExitCode: 0})
	m.SetResponse(`aws s3api get-bucket-encryption --bucket loopzero-archive`,
		&shellrun.Result{
			ExitCode: 0,
			Stdout:   `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"}}]}}`,
		})
	require.NoError(t, preflight(Config{
		Env: mapEnv(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": "x",
			"AWS_ACCESS_KEY_ID":        "y",
		}),
	}, m))
}

// =============================================================================
// extractSSEAlgo
// =============================================================================

func TestExtractSSEAlgo(t *testing.T) {
	body := `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"}}]}}`
	algo, err := extractSSEAlgo(body)
	require.NoError(t, err)
	assert.Equal(t, "aws:kms", algo)
}

func TestExtractSSEAlgo_EmptyRules(t *testing.T) {
	body := `{"ServerSideEncryptionConfiguration":{"Rules":[]}}`
	_, err := extractSSEAlgo(body)
	require.Error(t, err)
}

func TestExtractSSEAlgo_MalformedJSON(t *testing.T) {
	_, err := extractSSEAlgo("not json")
	require.Error(t, err)
}

// =============================================================================
// scanPlaintextSecrets
// =============================================================================

func TestScanPlaintextSecrets_Clean(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"deployment":{"resources":[{"id":"abc"}]}}`), 0o600))
	hits, err := scanPlaintextSecrets(p)
	require.NoError(t, err)
	assert.Equal(t, 0, hits)
}

func TestScanPlaintextSecrets_PlaintextDetected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"a":"my plaintext password here","b":"normal"}`), 0o600))
	hits, err := scanPlaintextSecrets(p)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
}

func TestScanPlaintextSecrets_CiphertextExcluded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	// "v1:..." prefix is excluded — Pulumi-encrypted block.
	require.NoError(t, os.WriteFile(p, []byte(`{"secret":"v1:abcdef ciphertext password value"}`), 0o600))
	hits, err := scanPlaintextSecrets(p)
	require.NoError(t, err)
	assert.Equal(t, 0, hits, "v1: prefix excludes the string from plaintext count")
}

func TestScanPlaintextSecrets_MissingFile(t *testing.T) {
	_, err := scanPlaintextSecrets("/tmp/zzz-no-archive.json")
	require.Error(t, err)
}

// =============================================================================
// hashFile
// =============================================================================

func TestHashFile_Stable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	require.NoError(t, os.WriteFile(p, []byte("hello"), 0o600))
	// SHA256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	got, err := hashFile(p)
	require.NoError(t, err)
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", got)
}

func TestHashFile_MissingFile(t *testing.T) {
	_, err := hashFile("/tmp/zzz-no-hash-file")
	require.Error(t, err)
}

// =============================================================================
// archivePrefix
// =============================================================================

func TestArchivePrefix(t *testing.T) {
	assert.Equal(t, "infra-pulumi-infra", archivePrefix("infra/pulumi-infra"))
	assert.Equal(t, "a-b-c", archivePrefix("a/b/c"))
}

// =============================================================================
// enumerateTargets
// =============================================================================

func TestEnumerateTargets_OnlyDirsWithPulumiYAML(t *testing.T) {
	root := mkRepo(t, []string{"infra/pulumi-infra"})
	require.NoError(t, os.MkdirAll(filepath.Join(root, "infra/pulumi-empty"), 0o755))
	got := enumerateTargets(root, []string{"infra/pulumi-infra", "infra/pulumi-empty"})
	assert.Equal(t, []string{"infra/pulumi-infra"}, got)
}

// =============================================================================
// listStacks / stackResourceCount
// =============================================================================

func TestListStacks_TwoStacks(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0, Stdout: `[{"name":"dev"},{"name":"prod"}]`})
	got, err := listStacks(nil, m, "/repo", "infra/pulumi-infra") //nolint:staticcheck
	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "prod"}, got)
}

func TestListStacks_EmptyStdout(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0, Stdout: ""})
	got, err := listStacks(nil, m, "/repo", "infra/pulumi-infra") //nolint:staticcheck
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestStackResourceCount_Two(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{
		ExitCode: 0,
		Stdout:   `{"resources":[{"id":"a"},{"id":"b"}]}`,
	})
	got, err := stackResourceCount(context.Background(), m, "/repo", "infra/pulumi-infra", "dev")
	require.NoError(t, err)
	assert.Equal(t, 2, got)
}

// A failed query is "unknown", never "zero live resources": zero is the one
// answer that lets the operator proceed to `pulumi stack rm`.
func TestStackResourceCount_CommandFailureIsAnError(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 1, Stderr: "unauthorized"})
	_, err := stackResourceCount(context.Background(), m, "/repo", "infra/pulumi-infra", "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
}

func TestStackResourceCount_MalformedOutputIsAnError(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0, Stdout: "not json"})
	_, err := stackResourceCount(context.Background(), m, "/repo", "infra/pulumi-infra", "dev")
	require.Error(t, err)
}

// =============================================================================
// Run — end-to-end with MockRunner
// =============================================================================

func TestRun_NoTargets(t *testing.T) {
	out, err := Run(Config{
		Root:             t.TempDir(),
		ParallelProjects: []string{"infra/missing"},
		Runner:           shellrun.NewMock(),
		DryRun:           true,
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Empty(t, out.Targets)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "nothing to do")
}

func TestRun_DryRunSkipsExportAndUpload(t *testing.T) {
	root := mkRepo(t, []string{"infra/pulumi-infra"})
	m := shellrun.NewMock()
	m.SetResponse(`pulumi stack ls --json`, &shellrun.Result{
		ExitCode: 0, Stdout: `[{"name":"dev"}]`,
	})
	m.SetResponse(`pulumi stack --stack dev --json`, &shellrun.Result{
		ExitCode: 0, Stdout: `{"resources":[]}`,
	})
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0})
	out, err := Run(Config{
		Root:             root,
		ParallelProjects: []string{"infra/pulumi-infra"},
		Runner:           m,
		DryRun:           true,
		Timestamp:        "20260520T120000Z",
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Empty(t, out.Archived, "DryRun must NOT write archive records")
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "DRY_RUN")
}

func TestRun_PreflightFailsOnMissingPassphrase(t *testing.T) {
	_, err := Run(Config{
		Env: mapEnv(map[string]string{}),
	})
	require.Error(t, err)
	var te *loopzeroerrors.LoopZeroError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "OPS_USAGE", te.Code)
}

func TestRun_LiveResourcesWarn(t *testing.T) {
	root := mkRepo(t, []string{"infra/pulumi-infra"})
	m := shellrun.NewMock()
	m.SetResponse(`pulumi stack ls --json`, &shellrun.Result{
		ExitCode: 0, Stdout: `[{"name":"dev"}]`,
	})
	m.SetResponse(`pulumi stack --stack dev --json`, &shellrun.Result{
		ExitCode: 0, Stdout: `{"resources":[{"id":"abc"}]}`,
	})
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0})
	out, err := Run(Config{
		Root:             root,
		ParallelProjects: []string{"infra/pulumi-infra"},
		Runner:           m,
		DryRun:           true,
	})
	require.NoError(t, err)
	assert.False(t, out.Pass, "live resources → Pass=false (operator gate)")
	assert.Equal(t, 1, out.LiveResourceCount)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "LIVE RESOURCES")
}

func TestRun_StackLsFailureBlocksPass(t *testing.T) {
	root := mkRepo(t, []string{"infra/pulumi-infra"})
	m := shellrun.NewMock()
	m.SetResponse(`pulumi stack ls --json`, &shellrun.Result{
		ExitCode: 1, Stderr: "auth",
	})
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0})
	out, err := Run(Config{
		Root:             root,
		ParallelProjects: []string{"infra/pulumi-infra"},
		Runner:           m,
		DryRun:           true,
	})
	require.NoError(t, err)
	assert.False(t, out.Pass, "a dir whose stacks cannot be listed was not archived — Pass must be false")
	assert.Equal(t, []string{"infra/pulumi-infra"}, out.UnlistedDirs)
	assert.Empty(t, out.Archived)
}

func TestRun_UnknownResourceCountBlocksPass(t *testing.T) {
	root := mkRepo(t, []string{"infra/pulumi-infra"})
	m := shellrun.NewMock()
	m.SetResponse(`pulumi stack ls --json`, &shellrun.Result{ExitCode: 0, Stdout: `[{"name":"dev"}]`})
	m.SetResponse(`pulumi stack --stack dev --json`, &shellrun.Result{ExitCode: 1, Stderr: "backend unreachable"})
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0})
	out, err := Run(Config{
		Root:             root,
		ParallelProjects: []string{"infra/pulumi-infra"},
		Runner:           m,
		DryRun:           true,
	})
	require.NoError(t, err)
	assert.False(t, out.Pass, "an unknown live-resource count must block the stack rm")
	assert.Equal(t, []string{"infra/pulumi-infra/dev"}, out.UnverifiedStacks)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "backend unreachable")
}

// liveExportRunner is a mock where preflight passes, one stack exists with no
// live resources, and `pulumi stack export` succeeds. The export file itself
// is written by the test.
func liveExportRunner() *shellrun.MockRunner {
	m := shellrun.NewMock()
	m.SetResponse(`aws s3api head-bucket --bucket loopzero-archive`, &shellrun.Result{ExitCode: 0})
	m.SetResponse(`aws s3api get-bucket-encryption --bucket loopzero-archive`, &shellrun.Result{
		ExitCode: 0,
		Stdout:   `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"}}]}}`,
	})
	m.SetResponse(`pulumi stack ls --json`, &shellrun.Result{ExitCode: 0, Stdout: `[{"name":"dev"}]`})
	m.SetResponse(`pulumi stack --stack dev --json`, &shellrun.Result{ExitCode: 0, Stdout: `{"resources":[]}`})
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0})
	return m
}

func s3Uploads(m *shellrun.MockRunner) int {
	n := 0
	for _, inv := range m.History() {
		if inv.Name == "aws" && len(inv.Args) > 1 && inv.Args[0] == "s3" && inv.Args[1] == "cp" {
			n++
		}
	}
	return n
}

func writeExport(t *testing.T, tmpDir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "infra-pulumi-infra-dev-20260520T120000Z.json"), []byte(body), 0o600))
}

func liveConfig(root, tmpDir string, m *shellrun.MockRunner) Config {
	return Config{
		Root:             root,
		ParallelProjects: []string{"infra/pulumi-infra"},
		Runner:           m,
		Env:              mapEnv(map[string]string{"PULUMI_CONFIG_PASSPHRASE": "p", "AWS_ACCESS_KEY_ID": "k"}),
		Timestamp:        "20260520T120000Z",
		TmpDir:           tmpDir,
	}
}

// The run told the operator to review plaintext candidates "before C5 upload"
// and then uploaded anyway. The archive must be held until the operator
// accepts the hits explicitly.
func TestRun_PlaintextHitsHoldTheUpload(t *testing.T) {
	root := mkRepo(t, []string{"infra/pulumi-infra"})
	tmpDir := t.TempDir()
	writeExport(t, tmpDir, `{"deployment":{"secrets":{"db":"password=hunter2"}}}`)
	m := liveExportRunner()

	out, err := Run(liveConfig(root, tmpDir, m))
	require.NoError(t, err)
	assert.Equal(t, 0, s3Uploads(m), "an archive with plaintext candidates must not be uploaded unreviewed")
	assert.False(t, out.Pass)
	assert.Empty(t, out.Archived)
	require.Len(t, out.Held, 1)
	assert.Equal(t, "dev", out.Held[0].Stack)
	assert.Equal(t, 1, out.Held[0].PlaintextHits)
}

func TestRun_PlaintextHitsUploadOnceAccepted(t *testing.T) {
	root := mkRepo(t, []string{"infra/pulumi-infra"})
	tmpDir := t.TempDir()
	writeExport(t, tmpDir, `{"deployment":{"secrets":{"db":"password=hunter2"}}}`)
	m := liveExportRunner()
	cfg := liveConfig(root, tmpDir, m)
	cfg.AcceptPlaintextHits = true

	out, err := Run(cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, s3Uploads(m))
	assert.True(t, out.Pass)
	require.Len(t, out.Archived, 1)
	assert.Equal(t, 1, out.Archived[0].PlaintextHits)
}

// =============================================================================
// Constants
// =============================================================================

func TestDefaults(t *testing.T) {
	assert.Equal(t, "loopzero-archive", DefaultArchiveBucket)
	assert.Equal(t, "3.140.0", PulumiVersionPinned)
	assert.Equal(t, []string{"infra/pulumi-infra", "infra/pulumi-platform"}, DefaultParallelProjects)
	assert.True(t, PlaintextSecretPattern.MatchString("api_key"))
	assert.True(t, PlaintextSecretPattern.MatchString("PASSWORD"))
	assert.False(t, PlaintextSecretPattern.MatchString("benign"))
}

// =============================================================================
// exportStack / uploadS3 — coverage uplift via direct unit tests
// =============================================================================

func TestExportStack_Success(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0})
	err := exportStack(nil, m, "/repo", "infra/pulumi-infra", "dev", "/tmp/x.json") //nolint:staticcheck
	require.NoError(t, err)
}

func TestExportStack_FailureSurfaces(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 1, Stderr: "stack not found"})
	err := exportStack(nil, m, "/repo", "infra/pulumi-infra", "dev", "/tmp/x.json") //nolint:staticcheck
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit 1")
}

func TestUploadS3_Success(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 0})
	err := uploadS3(nil, m, "/tmp/x.json", "s3://loopzero-archive/x", "abc", 3) //nolint:staticcheck
	require.NoError(t, err)
}

func TestUploadS3_FailureSurfaces(t *testing.T) {
	m := shellrun.NewMock()
	m.SetDefaultResponse(&shellrun.Result{ExitCode: 1, Stderr: "access denied"})
	err := uploadS3(nil, m, "/tmp/x.json", "s3://loopzero-archive/x", "abc", 3) //nolint:staticcheck
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit 1")
}

func TestCollectStrings_Malformed(t *testing.T) {
	var out []string
	err := collectStrings([]byte("not json"), &out)
	require.Error(t, err)
}

// =============================================================================
// Run — full live-export path (DryRun=false with mocked runner)
// =============================================================================

func TestRun_FullExportPath_HappyPath(t *testing.T) {
	root := mkRepo(t, []string{"infra/pulumi-infra"})
	tmpDir := t.TempDir()

	m := shellrun.NewMock()
	// preflight bucket checks
	m.SetResponse(`aws s3api head-bucket --bucket loopzero-archive`,
		&shellrun.Result{ExitCode: 0})
	m.SetResponse(`aws s3api get-bucket-encryption --bucket loopzero-archive`,
		&shellrun.Result{
			ExitCode: 0,
			Stdout:   `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"}}]}}`,
		})
	// pulumi stack ls
	m.SetResponse(`pulumi stack ls --json`,
		&shellrun.Result{ExitCode: 0, Stdout: `[{"name":"dev"}]`})
	// pulumi stack --stack dev --json
	m.SetResponse(`pulumi stack --stack dev --json`,
		&shellrun.Result{ExitCode: 0, Stdout: `{"resources":[]}`})
	// pulumi stack export: write an archive file to the path the
	// runner sees as --file argument (defaults to TmpDir/<prefix>-dev-TS.json)
	// We mock with a custom runner wrapper that fakes the file write.
	mFile := shellrun.NewMock()
	// Copy all SetResponse entries.
	mFile.SetResponse(`aws s3api head-bucket --bucket loopzero-archive`, &shellrun.Result{ExitCode: 0})
	mFile.SetResponse(`aws s3api get-bucket-encryption --bucket loopzero-archive`,
		&shellrun.Result{
			ExitCode: 0,
			Stdout:   `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"}}]}}`,
		})
	mFile.SetResponse(`pulumi stack ls --json`,
		&shellrun.Result{ExitCode: 0, Stdout: `[{"name":"dev"}]`})
	mFile.SetResponse(`pulumi stack --stack dev --json`,
		&shellrun.Result{ExitCode: 0, Stdout: `{"resources":[]}`})
	mFile.SetDefaultResponse(&shellrun.Result{ExitCode: 0})

	// Use a fake-archive helper: write the file before the runner is
	// invoked, then any later sha256 + scanPlaintextSecrets will work.
	exportedFile := filepath.Join(tmpDir,
		"infra-pulumi-infra-dev-20260520T120000Z.json")
	require.NoError(t, os.WriteFile(exportedFile,
		[]byte(`{"deployment":{"resources":[]}}`), 0o600))

	out, err := Run(Config{
		Root:             root,
		ParallelProjects: []string{"infra/pulumi-infra"},
		Runner:           mFile,
		Env: mapEnv(map[string]string{
			"PULUMI_CONFIG_PASSPHRASE": "p",
			"AWS_ACCESS_KEY_ID":        "k",
		}),
		Timestamp: "20260520T120000Z",
		TmpDir:    tmpDir,
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	require.Len(t, out.Archived, 1)
	assert.Equal(t, "dev", out.Archived[0].Stack)
	assert.Equal(t, "infra/pulumi-infra", out.Archived[0].Dir)
	assert.Contains(t, out.Archived[0].S3Key, "s3://loopzero-archive/pulumi-state/infra-pulumi-infra/dev/20260520T120000Z.json")
	assert.NotEmpty(t, out.Archived[0].SHA256)
}
