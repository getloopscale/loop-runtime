// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The request and worker roles must exist BEFORE the migrations run: 000062 and
// 000068 grant to them only if they are already there (#396). And every consumer
// of the database on the local path must connect as them, or loopzero-api refuses to
// boot on a pool that can bypass RLS.

func setupSteps(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../../Taskfile.yml")
	if err != nil {
		t.Fatalf("read Taskfile.yml: %v", err)
	}
	var doc struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"` // strings and {task: …} maps, mixed
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse Taskfile.yml: %v", err)
	}
	var steps []string
	for _, c := range doc.Tasks["setup"].Cmds {
		if m, ok := c.(map[string]any); ok {
			if task, ok := m["task"].(string); ok {
				steps = append(steps, task)
			}
		}
	}
	if len(steps) == 0 {
		t.Fatal("task setup declares no task steps")
	}
	return steps
}

func indexOf(steps []string, name string) int {
	for i, s := range steps {
		if s == name {
			return i
		}
	}
	return -1
}

func TestSetupProvisionsTheRolesBeforeTheMigrations(t *testing.T) {
	steps := setupSteps(t)
	roles, migrate := indexOf(steps, "db:roles"), indexOf(steps, "db:migrate")
	if roles < 0 {
		t.Fatalf("task setup never runs db:roles; steps: %v", steps)
	}
	if migrate < 0 || roles > migrate {
		t.Errorf("db:roles (step %d) must run before db:migrate (step %d): the migrations grant to the roles only if they exist", roles, migrate)
	}
}

func TestTheTiltAPIConnectsThroughTheRoleSplit(t *testing.T) {
	raw, err := os.ReadFile("../../tilt/loopzero-api-values.yaml")
	if err != nil {
		t.Fatalf("read the Tilt values: %v", err)
	}
	var values struct {
		Secrets struct {
			DatabaseURL       string `yaml:"databaseUrl"`
			WorkerDatabaseURL string `yaml:"loopzeroWorkerDatabaseUrl"`
		} `yaml:"secrets"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("parse the Tilt values: %v", err)
	}
	if !strings.HasPrefix(values.Secrets.DatabaseURL, "postgres://loopzero_api:") {
		t.Errorf("Tilt's DATABASE_URL does not connect as loopzero_api: %q", values.Secrets.DatabaseURL)
	}
	if !strings.HasPrefix(values.Secrets.WorkerDatabaseURL, "postgres://loopzero_worker:") {
		t.Errorf("Tilt's LOOPZERO_WORKER_DATABASE_URL does not connect as loopzero_worker: %q", values.Secrets.WorkerDatabaseURL)
	}
}

// The secrets cluster is optional (#398), and when it exists nothing handed
// OpenBao's AppRole to loopzero-api: vault-bootstrap-dev.sh does the whole chain and
// had no caller. Setup wires it after networking, which is the step that makes
// the secrets cluster resolvable from core.
func TestSetupWiresOpenBaoAfterNetworking(t *testing.T) {
	steps := setupSteps(t)
	wire, net := indexOf(steps, "k3d:secrets:wire"), indexOf(steps, "networking:setup")
	if wire < 0 {
		t.Fatalf("task setup never runs k3d:secrets:wire; steps: %v", steps)
	}
	if net < 0 || wire < net {
		t.Errorf("k3d:secrets:wire (step %d) must run after networking:setup (step %d)", wire, net)
	}
}

// secretsWireBody returns the shell text of task k3d:secrets:wire.
func secretsWireBody(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../taskfiles/k3d.yml")
	if err != nil {
		t.Fatalf("read taskfiles/k3d.yml: %v", err)
	}
	var doc struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse taskfiles/k3d.yml: %v", err)
	}
	task, ok := doc.Tasks["k3d:secrets:wire"]
	if !ok {
		t.Fatal("taskfiles/k3d.yml declares no k3d:secrets:wire")
	}
	var body strings.Builder
	for _, c := range task.Cmds {
		if s, ok := c.(string); ok {
			body.WriteString(s)
		}
	}
	return body.String()
}

func TestTheOpenBaoWiringRunsTheBootstrapOnlyWhenTheClusterExists(t *testing.T) {
	text := secretsWireBody(t)
	for _, want := range []string{"loopzero-secrets", "SKIP", "infra/scripts/local/vault-bootstrap-dev.sh"} {
		if !strings.Contains(text, want) {
			t.Errorf("k3d:secrets:wire does not mention %q; it must skip without the cluster and run the bootstrap with it", want)
		}
	}
}

// The bootstrap issues a NEW AppRole role_id every run and writes only the secret_id.
// loopzero-api refuses to boot with transit on and no role_id, and the dev loop turns
// transit on — so the wiring that runs the bootstrap must sync the role_id right after
// it, not leave it to a seed that runs once the cleanroom is already over (#405).
func TestTheOpenBaoWiringSyncsTheRoleIDTheBootstrapIssued(t *testing.T) {
	text := secretsWireBody(t)

	boot := strings.Index(text, "vault-bootstrap-dev.sh")
	sync := strings.Index(text, "seed vault-role-id-sync")
	if sync < 0 {
		t.Fatalf("k3d:secrets:wire never runs `loopzero-ops seed vault-role-id-sync`; body:\n%s", text)
	}
	if boot < 0 || sync < boot {
		t.Errorf("the role-id sync (offset %d) must run after vault-bootstrap-dev.sh (offset %d)", sync, boot)
	}
	// The secrets bootstrap leaves kubectl on another cluster, and a seed inherits the
	// active context unless CORE_CONTEXT pins it — measured: the sync then reported the
	// AppRole Secret absent while it existed on core.
	if !strings.Contains(text, "CORE_CONTEXT={{.CORE_CONTEXT}}") {
		t.Errorf("the role-id sync must pin CORE_CONTEXT to the core context var; body:\n%s", text)
	}
}

// The dev loop turns the admin API on, and every /api/v1/admin/* route is gated by
// the admin service-auth secret alone (Authenticator.RequireAdminService). With the
// flag on and no secret, the admin routes answer 403 to everyone, so evict, pin and
// inspect cannot be exercised on the dev loop at all (#413).
func TestTheTiltAPIEnablesTheAdminRoutesItTurnsOn(t *testing.T) {
	raw, err := os.ReadFile("../../tilt/loopzero-api-values.yaml")
	if err != nil {
		t.Fatalf("read the Tilt values: %v", err)
	}
	var values struct {
		Config struct {
			LoopZeroAdminAPI string `yaml:"loopzeroAdminApi"`
		} `yaml:"config"`
		Secrets struct {
			AdminServiceAuthSecret string `yaml:"loopzeroAdminServiceAuthSecret"`
		} `yaml:"secrets"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("parse the Tilt values: %v", err)
	}
	if values.Config.LoopZeroAdminAPI != "true" {
		t.Skip("the Tilt values do not enable the admin API — https://github.com/getloopscale/loopzero/issues/413")
	}
	if values.Secrets.AdminServiceAuthSecret == "" {
		t.Error("loopzeroAdminApi is \"true\" but secrets.loopzeroAdminServiceAuthSecret is empty: every admin route answers 403")
	}
}
