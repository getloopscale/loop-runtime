// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The sandbox service of staging (getloopscale/loop-sandbox) runs on the host's Docker,
// started by infra/staging/sandbox.sh from `task staging:platform`, so that the nightly
// rebuild and every deploy bring it up with the rest of the platform.

// fakeSandboxDocker stands in for docker. It answers the two lookups of the VPC's
// network, records every call in $FAKE_STATE/calls and, for `compose`, copies the env
// file it was given to $FAKE_STATE/env.
const fakeSandboxDocker = `#!/usr/bin/env bash
set -euo pipefail
echo "$*" >>"$FAKE_STATE/calls"
case "$1 ${2:-}" in
  "network ls")
    case "$*" in *"label=aws-emulator.vpc-id=vpc-core"*) echo net-core ;; esac ;;
  "network inspect") echo 10.40.0.1 ;;
  compose*)
    prev=""
    for arg in "$@"; do
      if [ "$prev" = "--env-file" ]; then cp "$arg" "$FAKE_STATE/env"; fi
      prev="$arg"
    done ;;
esac
exit 0
`

const fakeSandboxYq = `#!/usr/bin/env bash
case "$1" in
  *loop-sandbox*) echo sha-abc1234 ;;
  *registry*) echo ghcr.io/getloopscale/ ;;
esac
`

type sandboxHost struct {
	t     *testing.T
	bin   string
	state string
}

func newSandboxHost(t *testing.T) sandboxHost {
	t.Helper()
	h := sandboxHost{t: t, bin: t.TempDir(), state: t.TempDir()}
	for name, body := range map[string]string{
		"docker": fakeSandboxDocker,
		"yq":     fakeSandboxYq,
		"curl":   "#!/usr/bin/env bash\necho \"$*\" >>\"$FAKE_STATE/curl\"\nexit 0\n",
		"sleep":  "#!/usr/bin/env bash\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(h.bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (h sandboxHost) run(env []string, args ...string) (stdout, stderr string, err error) {
	h.t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot(), "infra", "staging", "sandbox.sh")}, args...)...)
	cmd.Env = append(append(os.Environ(), "PATH="+h.bin+":"+os.Getenv("PATH"), "FAKE_STATE="+h.state,
		"SANDBOX_STATE_DIR="+filepath.Join(h.state, "sandbox")), env...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

func (h sandboxHost) file(name string) string {
	h.t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.state, name))
	if err != nil {
		h.t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

func TestSandboxAddressIsTheGatewayOfTheCoreVPC(t *testing.T) {
	h := newSandboxHost(t)

	out, stderr, err := h.run([]string{"LOOP_RUNTIME_VPC_ID=vpc-core"}, "address")

	if err != nil {
		t.Fatalf("sandbox.sh address: %v\n%s", err, stderr)
	}
	if got := strings.TrimSpace(out); got != "http://10.40.0.1:8090" {
		t.Errorf("address = %q, want http://10.40.0.1:8090: the console's pods reach the host at the VPC's gateway", got)
	}
}

func TestSandboxAddressFailsWhileTheVPCDoesNotExist(t *testing.T) {
	h := newSandboxHost(t)

	_, stderr, err := h.run([]string{"LOOP_RUNTIME_VPC_ID=vpc-other"}, "address")

	if err == nil || !strings.Contains(stderr, "vpc-other") {
		t.Errorf("err = %v, stderr = %q; want a failure that names the VPC", err, stderr)
	}
}

func TestSandboxUpRunsThePinnedImageBehindTheConsolesKey(t *testing.T) {
	h := newSandboxHost(t)

	_, stderr, err := h.run([]string{"LOOP_RUNTIME_VPC_ID=vpc-core", "SANDBOX_GATEWAY_PUBKEY=cHVibGlj"}, "up")

	if err != nil {
		t.Fatalf("sandbox.sh up: %v\n%s", err, stderr)
	}
	env := h.file("env")
	for _, want := range []string{
		"SANDBOX_IMAGE=ghcr.io/getloopscale/loop-sandbox:sha-abc1234",
		"SANDBOX_GATEWAY_PUBKEY=cHVibGlj",
		"SANDBOX_BIND=10.40.0.1:8090",
	} {
		if !strings.Contains(env, want+"\n") {
			t.Errorf("the compose env file lacks %q:\n%s", want, redactSandboxEnv(env))
		}
	}
	if !regexp.MustCompile(`(?m)^SANDBOX_DB_PASSWORD=[0-9a-f]{48}$`).MatchString(env) ||
		!regexp.MustCompile(`(?m)^SANDBOX_SECRETS_KEY=[0-9a-f]{64}$`).MatchString(env) {
		t.Errorf("the compose env file lacks a generated database password or secrets key:\n%s", redactSandboxEnv(env))
	}
	if calls := h.file("calls"); !strings.Contains(calls, "up -d --wait") {
		t.Errorf("docker compose was not asked to start the service:\n%s", calls)
	}
	if probes := h.file("curl"); !strings.Contains(probes, "http://10.40.0.1:8090/v1/readyz") {
		t.Errorf("the API's readiness was not checked at its address:\n%s", probes)
	}
}

func TestSandboxUpKeepsItsDatabasePasswordAcrossRuns(t *testing.T) {
	// The password is set when the database volume is first initialised: a new one on
	// the next deploy would lock the API out of its own database.
	h := newSandboxHost(t)
	env := []string{"LOOP_RUNTIME_VPC_ID=vpc-core", "SANDBOX_GATEWAY_PUBKEY=cHVibGlj"}
	password := regexp.MustCompile(`(?m)^SANDBOX_DB_PASSWORD=.*$`)

	if _, stderr, err := h.run(env, "up"); err != nil {
		t.Fatalf("first up: %v\n%s", err, stderr)
	}
	first := password.FindString(h.file("env"))
	if _, stderr, err := h.run(env, "up"); err != nil {
		t.Fatalf("second up: %v\n%s", err, stderr)
	}

	if second := password.FindString(h.file("env")); first == "" || first != second {
		t.Error("the database password changed between two runs of up")
	}
}

func TestSandboxUpRefusesToStartWithoutTheConsolesKey(t *testing.T) {
	h := newSandboxHost(t)

	_, stderr, err := h.run([]string{"LOOP_RUNTIME_VPC_ID=vpc-core"}, "up")

	if err == nil || !strings.Contains(stderr, "SANDBOX_GATEWAY_PUBKEY") {
		t.Errorf("err = %v, stderr = %q; want a failure that names SANDBOX_GATEWAY_PUBKEY", err, stderr)
	}
	if _, statErr := os.Stat(filepath.Join(h.state, "calls")); statErr == nil {
		if strings.Contains(h.file("calls"), "compose") {
			t.Error("the service was started without a key to trust")
		}
	}
}

func redactSandboxEnv(env string) string {
	return regexp.MustCompile(`(?m)^(SANDBOX_DB_PASSWORD|SANDBOX_SECRETS_KEY)=.*$`).ReplaceAllString(env, "$1=<redacted>")
}

// The service has to be part of what a rebuild brings up and takes down: installed by
// hand, it would be gone after the nightly rebuild.
func TestStagingPlatformStartsTheSandboxBeforeTheConsoleThatCallsIt(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	taskfile := string(raw)
	up := strings.Index(taskfile, "infra/staging/sandbox.sh up")
	coreSync := strings.Index(taskfile, "-f infra/helmfile/core.helmfile.yaml.gotmpl -e staging")
	if up < 0 {
		t.Fatal("taskfiles/staging.yml never runs infra/staging/sandbox.sh up")
	}
	if coreSync < 0 || up > coreSync {
		t.Error("the sandbox service starts after the core sync: loop-console would be installed with an address nothing answers at")
	}
	if !strings.Contains(taskfile, "LOOP_RUNTIME_SANDBOX_API_URL") {
		t.Error("staging:platform does not export LOOP_RUNTIME_SANDBOX_API_URL, which the console's values read")
	}
	down := taskfile[strings.Index(taskfile, "\n  down:"):]
	if !strings.Contains(down, "infra/staging/sandbox.sh down") {
		t.Error("staging:down leaves the sandbox service and its sandboxes behind")
	}
}

func TestTheStagingConsoleIsPointedAtTheSandbox(t *testing.T) {
	values, err := os.ReadFile(filepath.Join(repoRoot(), "infra/helmfile/values/staging/loop-console.yaml.gotmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`sandboxApiUrl:\s*\{\{\s*env "LOOP_RUNTIME_SANDBOX_API_URL"`).Match(values) {
		t.Error("values/staging/loop-console.yaml.gotmpl does not set config.sandboxApiUrl from LOOP_RUNTIME_SANDBOX_API_URL")
	}
	environment, err := os.ReadFile(filepath.Join(repoRoot(), "infra/helmfile/environments/staging.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s+loop-sandbox: "sha-[0-9a-f]{7}"`).Match(environment) {
		t.Error("environments/staging.yaml pins no loop-sandbox image (platformImages.tags.loop-sandbox: sha-<7 hex>)")
	}
}

func TestStagingValidateSelectsTheSandboxJourney(t *testing.T) {
	script, _ := validateScript(t)
	journeys := regexp.MustCompile(`(?m)^JOURNEYS="\$\{VALIDATE_JOURNEYS:-([^}]*)\}"`).FindStringSubmatch(script)
	if journeys == nil {
		t.Fatal("infra/staging/validate.sh declares no default JOURNEYS")
	}
	selected := false
	for _, label := range strings.Split(journeys[1], "||") {
		if strings.TrimSpace(label) == "sandbox" {
			selected = true
		}
	}
	if !selected {
		t.Errorf("JOURNEYS is %q: it does not select sandbox, so no deploy proves a sandbox can be created", journeys[1])
	}
	spec, err := os.ReadFile(filepath.Join(repoRoot(), "infra/tests/livetest/dogfood/journey_sandbox_live_test.go"))
	if err != nil {
		t.Fatalf("no spec for the sandbox journey: %v", err)
	}
	if !regexp.MustCompile(`Label\([^)]*"sandbox"`).Match(spec) {
		t.Error("journey_sandbox_live_test.go carries no sandbox label: the filter would select nothing of it")
	}
	if regexp.MustCompile(`\bSkip\(`).Match(spec) {
		t.Error("the sandbox journey calls Skip: a step that cannot run must fail, not pass as skipped")
	}
}

// Once the signing key exists, loop-console signs every call it forwards to
// loop-runtime-api as well. An API that trusts no key then answers 401 to all of them,
// which took the whole console down on staging.
func TestTheStagingAPITrustsTheKeyTheConsoleSignsWith(t *testing.T) {
	values, err := os.ReadFile(filepath.Join(repoRoot(), "infra/helmfile/values/staging/loop-runtime-api.yaml.gotmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`loopRuntimeServiceAuthEd25519PublicCurrent:\s*\{\{\s*requiredEnv "LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_KEY"`).Match(values) {
		t.Error("values/staging/loop-runtime-api.yaml.gotmpl does not set config.loopRuntimeServiceAuthEd25519PublicCurrent from LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_KEY")
	}
	taskfile, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(taskfile), "export LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_KEY") {
		t.Error("staging:platform does not export LOOP_RUNTIME_SERVICE_AUTH_ED25519_PUBLIC_KEY for the core sync")
	}
}
