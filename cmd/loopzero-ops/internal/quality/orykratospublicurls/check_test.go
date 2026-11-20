package orykratospublicurls

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestCheck_HelmfileAbsentIsSkipped(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	out, err := Check(Config{RepoRoot: writeHelmfileConfig(t)})

	if err != nil || !out.Skipped || out.SkipReason == "" {
		t.Fatalf("without helmfile the gate skips and says why; out=%+v err=%v", out, err)
	}
}

func writeHelmfileConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "infra", "helmfile")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core.helmfile.yaml.gotmpl"), []byte("# stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// A render failure must not be read as "nothing to check": a broken chart,
// missing values or an unset required variable used to make every env skip,
// and the gate then reported SKIP with exit 0 — indistinguishable from
// helmfile not being installed.
func TestCheck_RenderFailureFails(t *testing.T) {
	root := writeHelmfileConfig(t)
	failing := func(_, env string) ([]byte, error) {
		return nil, errors.New("required env var `LOOPZERO_DB_PASSWORD` is not set")
	}

	out, err := Check(Config{RepoRoot: root, Render: failing})

	if err != nil || out.Verdict() != verdict.Fail || out.Pass || out.Skipped {
		t.Fatalf("render failure must fail the gate; out=%+v err=%v", out, err)
	}
	if !strings.Contains(strings.Join(out.Failures, "\n"), "LOOPZERO_DB_PASSWORD") {
		t.Errorf("the failure must carry the render error; got %v", out.Failures)
	}
}

func TestCheck_EmptyRenderFails(t *testing.T) {
	root := writeHelmfileConfig(t)
	empty := func(_, _ string) ([]byte, error) { return nil, nil }

	if out, err := Check(Config{RepoRoot: root, Render: empty}); err != nil || out.Verdict() != verdict.Fail {
		t.Fatalf("an empty render must fail the gate; err=%v", err)
	}
}

func TestCheck_GoldenRenderPasses(t *testing.T) {
	root := writeHelmfileConfig(t)
	render := func(_, env string) ([]byte, error) {
		if env == "dev" {
			return []byte("ui_url: http://localhost:4455/login\n"), nil
		}
		return []byte(strings.Join([]string{
			`base_url: "https://auth.getloopscale.com/"`,
			"enabled: true",
			"allow_credentials: true",
			"domain: getloopscale.com",
			"allowed_return_urls:",
			"ui_url: https://app.getloopscale.com/register",
			"ui_url: https://app.getloopscale.com/login",
		}, "\n")), nil
	}

	out, err := Check(Config{RepoRoot: root, Render: render})

	if err != nil || !out.Pass {
		t.Fatalf("golden render must pass; out=%+v err=%v", out, err)
	}
}

func TestCheck_MissingHelmfileConfigIsAnError(t *testing.T) {
	render := func(_, _ string) ([]byte, error) { return nil, nil }

	if _, err := Check(Config{RepoRoot: t.TempDir(), Render: render}); err == nil {
		t.Fatalf("a missing helmfile config is a broken setup, not a pass; err=%v", err)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if out.Skipped {
		// Only one skip is left: helmfile not installed on this machine.
		return
	}
	if err != nil {
		t.Fatalf("real repo: %v; failures: %v", err, out.Failures)
	}
}
