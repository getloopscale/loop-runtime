// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// MinIO withdrew every binary channel of its community edition (#394): dl.min.io
// answers 410, the quay.io/minio repositories require authentication, Docker Hub
// minio/minio is gone, and github.com/minio/minio is archived as source-only.
// A reference to any of them works only on a node that cached the image before,
// so it passes on a warm cluster and fails on the first cold start — which is how
// it went unnoticed until a real cleanroom ran.
//
// The object store and its client are therefore built here, from pinned source
// commits (infra/images/minio, infra/images/clone-runner), and nothing may reach
// for the withdrawn channels again.

var withdrawnMinioChannel = regexp.MustCompile(
	`dl\.min\.io|quay\.io/minio/|(^|[\s"'=:])(docker\.io/)?minio/(mc|minio)[:@"'\s]`)

func TestNothingReachesForAWithdrawnMinioChannel(t *testing.T) {
	root := repoRoot(t)
	for _, dir := range []string{"charts", "infra/helm", "infra/helmfile", "infra/images", "infra/tests/e2e", "taskfiles"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			// A symlink points into a tree this walk already covers (loop-runtime-monitoring
			// links infra/helm/monitoring), so it is skipped rather than read twice.
			if err != nil || !d.Type().IsRegular() || strings.HasSuffix(path, ".md") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(body), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
					continue
				}
				if withdrawnMinioChannel.MatchString(line) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s:%d reaches for a withdrawn MinIO channel (#394): %s", rel, i+1, trimmed)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// The upstream minio/minio chart defaults both its server and its client image to
// quay.io/minio, which no longer serves them. Every environment that installs the
// release must replace both, or it falls back to the dead default silently.
//
// Rendered, not counted: this used to count `- name: image.repository` lines in the
// helmfile text, which says nothing about which branch an environment takes — and
// the branches grew a third (a declared platform registry, #435).
func TestEveryEnvironmentReplacesBothMinioChartImages(t *testing.T) {
	for _, env := range []string{"dev", "dev-public", "drill", "prod", "standalone", "standalone-multi", "standalone-public"} {
		t.Run(env, func(t *testing.T) {
			values := helmfileValues(t, "core.helmfile.yaml.gotmpl", env, "minio", helmfileCoreEnv...)
			for _, image := range []string{"image", "mcImage"} {
				repo, _ := dig(values, image, "repository").(string)
				tag, _ := dig(values, image, "tag").(string)
				if repo == "" || tag == "" {
					t.Errorf("%s: %s = %q:%q — the chart's withdrawn default would be used", env, image, repo, tag)
					continue
				}
				if withdrawnMinioChannel.MatchString(" " + repo + ":") {
					t.Errorf("%s: %s = %q reaches for a withdrawn MinIO channel (#394)", env, image, repo)
				}
			}
		})
	}
}

// A binary built from source is only as pinned as the commit it was built from. A
// tag can be moved; a commit cannot. Each image must name its commit and refuse to
// build if the checkout is anything else.
func TestMinioImagesBuildFromAVerifiedSourceCommit(t *testing.T) {
	commit := regexp.MustCompile(`ARG \w+_COMMIT=[0-9a-f]{40}\b`)
	verifiesCommit := regexp.MustCompile(`git (-C \S+ )?rev-parse HEAD\)?"? = "\$\{\w+_COMMIT\}"`)
	for _, dockerfile := range []string{"infra/images/minio/Dockerfile", "infra/images/clone-runner/Dockerfile"} {
		body, err := os.ReadFile(filepath.Join(repoRoot(t), dockerfile))
		if err != nil {
			t.Fatalf("read %s: %v", dockerfile, err)
		}
		text := string(body)
		if !commit.MatchString(text) {
			t.Errorf("%s pins no 40-hex source commit", dockerfile)
		}
		if !verifiesCommit.MatchString(text) {
			t.Errorf("%s does not verify the checked-out commit before building", dockerfile)
		}
	}
}

// The cache and clone steps run `sh -c` in loop-runtime-clone-runner, an Alpine image, so
// `sh` is BusyBox ash. The minio/mc image they used before was RHEL-based, where
// `sh` is bash, and a bash-only construct worked there and fails here with
// "syntax error: unexpected redirection" — measured on cache-save when the image
// changed (#394). Cache save is best-effort, so the failure would not even stop the
// build: the cache would silently never be written.
var bashOnlyConstruct = regexp.MustCompile(`<<<|\bread\s+-[a-zA-Z]*a|\[@\]|\[\[\s|\bdeclare\s+-a\b`)

func TestShellStepsInTheCloneRunnerUseOnlyPosixSh(t *testing.T) {
	rendered := renderChart(t, publishedChart(t, "loop-runtime-build-cwts"), "posix-sh", nil)
	var checked int
	for _, doc := range strings.Split(rendered, "\n---") {
		var cwt struct {
			Kind     string                `yaml:"kind"`
			Metadata struct{ Name string } `yaml:"metadata"`
			Spec     struct {
				Templates []struct {
					Name      string `yaml:"name"`
					Container *struct {
						Image   string   `yaml:"image"`
						Command []string `yaml:"command"`
					} `yaml:"container"`
				} `yaml:"templates"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &cwt); err != nil || cwt.Kind != "ClusterWorkflowTemplate" {
			continue
		}
		for _, tpl := range cwt.Spec.Templates {
			c := tpl.Container
			if c == nil || !strings.Contains(c.Image, "loop-runtime-clone-runner") || len(c.Command) < 3 || c.Command[0] != "sh" {
				continue
			}
			checked++
			for i, line := range strings.Split(c.Command[2], "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "#") {
					continue
				}
				if bashOnlyConstruct.MatchString(line) {
					t.Errorf("%s/%s line %d uses a bash-only construct under BusyBox sh: %s",
						cwt.Metadata.Name, tpl.Name, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no `sh -c` step running loop-runtime-clone-runner was found; the check measured nothing")
	}
}

// loop-runtime-minio is the server only: upstream's release image also carried mc, ours
// does not, and the client lives in loop-runtime-clone-runner (#394). A script that runs
// `mc` inside the MinIO server pod therefore fails — measured when
// provision-api-tokens.sh stopped a cold setup at "Ensuring MinIO buckets exist".
// The buckets are declared once, in infra/helm/minio/values.yaml, and created by
// the chart's own job; nothing reaches into the server pod for them.
func TestNoScriptRunsTheClientInsideTheMinioServerPod(t *testing.T) {
	root := repoRoot(t)
	execIntoMinio := regexp.MustCompile(`exec\s+"?\$\{?MINIO_POD`)
	for _, dir := range []string{"infra/scripts", "infra/helmfile/scripts", "taskfiles"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() || strings.HasSuffix(path, ".md") {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(body), "\n") {
				if execIntoMinio.MatchString(line) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s:%d runs a command inside the MinIO server pod, which carries no mc: %s", rel, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}
