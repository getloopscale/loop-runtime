// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// infra/helm/data-plane-service runs one data-plane service (memory, RAG, lens) with its
// own Postgres, so dev-public's data plane has a versioned definition instead of compose
// directories that no longer exist (#446). One chart, installed once per service.

type k8sObject struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec map[string]any `yaml:"spec"`
}

type envVar struct {
	Name      string         `yaml:"name"`
	Value     string         `yaml:"value"`
	ValueFrom map[string]any `yaml:"valueFrom"`
}

type container struct {
	Name            string         `yaml:"name"`
	Image           string         `yaml:"image"`
	Command         []string       `yaml:"command"`
	Env             []envVar       `yaml:"env"`
	ReadinessProbe  map[string]any `yaml:"readinessProbe"`
	LivenessProbe   map[string]any `yaml:"livenessProbe"`
	SecurityContext map[string]any `yaml:"securityContext"`
	Ports           []struct {
		ContainerPort int `yaml:"containerPort"`
	} `yaml:"ports"`
}

type workload struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Template struct {
			Spec struct {
				SecurityContext map[string]any `yaml:"securityContext"`
				InitContainers  []container    `yaml:"initContainers"`
				Containers      []container    `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
		VolumeClaimTemplates []map[string]any `yaml:"volumeClaimTemplates"`
	} `yaml:"spec"`
}

const dataPlaneValues = `
image:
  repository: registry.example/loopzero-memory
  tag: "0123456789abcdef0123456789abcdef01234567"
port: 8080
healthPath: /v1/health
nodePort: 30181
existingSecret: loopzero-memory
env:
  HOST: "0.0.0.0"
  LOOPZEROMEM_EMBEDDER: openai
secretEnv:
  - name: OPENAI_API_KEY
    key: openai-api-key
  - name: THEMORY_ADMIN_BOOTSTRAP_TOKEN
    key: admin-bootstrap-token
workers:
  - name: worker
    command: ["node_modules/.bin/tsx", "src/worker.ts"]
postgres:
  image: registry.example/pgvector:v0.5.1
  database: themem
  user: themem
  uriEnv: LOOPZEROMEM_PG_URI
  dataPath: /var/lib/postgresql/data
  storage: 2Gi
`

func renderDataPlane(t *testing.T, values string) []workloadDoc {
	t.Helper()
	f := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(f, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	out := renderChart(t, uniqueChart(t, "data-plane-service"), "loopzero-memory", nil, f)
	return parseDocs(t, out)
}

type workloadDoc struct {
	obj k8sObject
	raw string
}

func parseDocs(t *testing.T, out string) []workloadDoc {
	t.Helper()
	var docs []workloadDoc
	for _, part := range strings.Split(out, "\n---") {
		var o k8sObject
		if err := yaml.Unmarshal([]byte(part), &o); err != nil {
			t.Fatalf("parse rendered doc: %v\n%s", err, part)
		}
		if o.Kind != "" {
			docs = append(docs, workloadDoc{obj: o, raw: part})
		}
	}
	return docs
}

func findDoc(t *testing.T, docs []workloadDoc, kind, name string) workloadDoc {
	t.Helper()
	for _, d := range docs {
		if d.obj.Kind == kind && d.obj.Metadata.Name == name {
			return d
		}
	}
	t.Fatalf("no %s %q rendered", kind, name)
	return workloadDoc{}
}

func asWorkload(t *testing.T, d workloadDoc) workload {
	t.Helper()
	var w workload
	if err := yaml.Unmarshal([]byte(d.raw), &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func envIndex(env []envVar, name string) int {
	for i, e := range env {
		if e.Name == name {
			return i
		}
	}
	return -1
}

func TestDataPlaneServiceRunsThePinnedImageAsNonRoot(t *testing.T) {
	docs := renderDataPlane(t, dataPlaneValues)
	app := asWorkload(t, findDoc(t, docs, "Deployment", "loopzero-memory"))
	c := app.Spec.Template.Spec.Containers[0]

	if c.Image != "registry.example/loopzero-memory:0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("image = %q", c.Image)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 8080 {
		t.Errorf("ports = %+v, want 8080", c.Ports)
	}
	if app.Spec.Template.Spec.SecurityContext["runAsNonRoot"] != true {
		t.Errorf("pod securityContext = %v, want runAsNonRoot", app.Spec.Template.Spec.SecurityContext)
	}
	if c.SecurityContext["allowPrivilegeEscalation"] != false {
		t.Errorf("container securityContext = %v, want no privilege escalation", c.SecurityContext)
	}
}

func TestDataPlaneServiceProbesItsHealthPath(t *testing.T) {
	c := asWorkload(t, findDoc(t, renderDataPlane(t, dataPlaneValues), "Deployment", "loopzero-memory")).Spec.Template.Spec.Containers[0]
	for name, probe := range map[string]map[string]any{"readiness": c.ReadinessProbe, "liveness": c.LivenessProbe} {
		get, _ := probe["httpGet"].(map[string]any)
		if get["path"] != "/v1/health" {
			t.Errorf("%s probe = %v, want httpGet /v1/health", name, probe)
		}
	}
}

// The URI embeds the password through Kubernetes' $(VAR) expansion, which only resolves a
// variable defined EARLIER in the list — defined later, the literal "$(…)" reaches the app.
func TestDataPlaneServiceBuildsItsDatabaseURIFromItsOwnPostgres(t *testing.T) {
	env := asWorkload(t, findDoc(t, renderDataPlane(t, dataPlaneValues), "Deployment", "loopzero-memory")).Spec.Template.Spec.Containers[0].Env

	pw, uri := envIndex(env, "POSTGRES_PASSWORD"), envIndex(env, "LOOPZEROMEM_PG_URI")
	if pw < 0 || uri < 0 {
		t.Fatalf("env = %+v, want POSTGRES_PASSWORD and LOOPZEROMEM_PG_URI", env)
	}
	if pw > uri {
		t.Errorf("POSTGRES_PASSWORD is defined after the URI that expands it")
	}
	want := "postgres://themem:$(POSTGRES_PASSWORD)@loopzero-memory-postgres:5432/themem"
	if env[uri].Value != want {
		t.Errorf("LOOPZEROMEM_PG_URI = %q, want %q", env[uri].Value, want)
	}
}

func TestDataPlaneServiceReadsSecretsFromTheExistingSecret(t *testing.T) {
	env := asWorkload(t, findDoc(t, renderDataPlane(t, dataPlaneValues), "Deployment", "loopzero-memory")).Spec.Template.Spec.Containers[0].Env
	for name, key := range map[string]string{
		"OPENAI_API_KEY": "openai-api-key", "THEMORY_ADMIN_BOOTSTRAP_TOKEN": "admin-bootstrap-token", "POSTGRES_PASSWORD": "postgres-password",
	} {
		i := envIndex(env, name)
		if i < 0 {
			t.Errorf("%s not in env", name)
			continue
		}
		ref, _ := env[i].ValueFrom["secretKeyRef"].(map[string]any)
		if ref["name"] != "loopzero-memory" || ref["key"] != key {
			t.Errorf("%s valueFrom = %v, want secret loopzero-memory key %s", name, env[i].ValueFrom, key)
		}
	}
	if i := envIndex(env, "LOOPZEROMEM_EMBEDDER"); i < 0 || env[i].Value != "openai" {
		t.Errorf("plain env LOOPZEROMEM_EMBEDDER missing: %+v", env)
	}
}

// The edge reaches the service at a fixed host port, so the node port must be pinned
// (an unpinned one moves on every recreation and the cluster's port map stops matching).
func TestDataPlaneServiceIsReachableOnItsPinnedNodePort(t *testing.T) {
	svc := findDoc(t, renderDataPlane(t, dataPlaneValues), "Service", "loopzero-memory").raw
	for _, want := range []string{"type: NodePort", "nodePort: 30181", "port: 8080"} {
		if !strings.Contains(svc, want) {
			t.Errorf("Service lacks %q:\n%s", want, svc)
		}
	}
}

func TestDataPlaneServiceWithoutANodePortStaysInsideTheCluster(t *testing.T) {
	values := strings.Replace(dataPlaneValues, "nodePort: 30181\n", "", 1)
	svc := findDoc(t, renderDataPlane(t, values), "Service", "loopzero-memory").raw
	if strings.Contains(svc, "NodePort") {
		t.Errorf("Service is a NodePort without a nodePort value:\n%s", svc)
	}
}

func TestDataPlaneServiceRunsEachWorkerWithTheAppsEnvironment(t *testing.T) {
	docs := renderDataPlane(t, dataPlaneValues)
	c := asWorkload(t, findDoc(t, docs, "Deployment", "loopzero-memory-worker")).Spec.Template.Spec.Containers[0]
	if strings.Join(c.Command, " ") != "node_modules/.bin/tsx src/worker.ts" {
		t.Errorf("worker command = %v", c.Command)
	}
	if envIndex(c.Env, "LOOPZEROMEM_PG_URI") < 0 || envIndex(c.Env, "OPENAI_API_KEY") < 0 {
		t.Errorf("worker env = %+v, want the app's database and secrets", c.Env)
	}
	if c.ReadinessProbe != nil {
		t.Errorf("worker has an HTTP probe but serves nothing: %v", c.ReadinessProbe)
	}
}

func TestDataPlaneServiceKeepsItsDatabaseOnAVolume(t *testing.T) {
	docs := renderDataPlane(t, dataPlaneValues)
	pg := asWorkload(t, findDoc(t, docs, "StatefulSet", "loopzero-memory-postgres"))
	c := pg.Spec.Template.Spec.Containers[0]
	if c.Image != "registry.example/pgvector:v0.5.1" {
		t.Errorf("postgres image = %q", c.Image)
	}
	if len(pg.Spec.VolumeClaimTemplates) != 1 || !strings.Contains(findDoc(t, docs, "StatefulSet", "loopzero-memory-postgres").raw, "storage: 2Gi") {
		t.Errorf("postgres has no 2Gi volume claim")
	}
	for _, name := range []string{"POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD"} {
		if envIndex(c.Env, name) < 0 {
			t.Errorf("postgres env lacks %s", name)
		}
	}
	findDoc(t, docs, "Service", "loopzero-memory-postgres")
}

// A tag names one build; a service without one would run whatever a mutable tag points at.
func TestDataPlaneServiceRefusesAnImageWithoutATag(t *testing.T) {
	f := filepath.Join(t.TempDir(), "values.yaml")
	values := strings.Replace(dataPlaneValues, `  tag: "0123456789abcdef0123456789abcdef01234567"`+"\n", "", 1)
	if err := os.WriteFile(f, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := renderChartE(t, uniqueChart(t, "data-plane-service"), "loopzero-memory", nil, f); err == nil {
		t.Error("rendered a service with no image tag")
	}
}

// The app migrates its database on boot and exits when it cannot reach it, so starting it
// before Postgres accepts connections cost a crash-loop on every install (measured on the
// droplet: two restarts before the first Ready). Every process waits for its database, as
// its database user: without -U, pg_isready takes the OS user, which the app's uid does not
// have, and answers "no attempt" forever (measured: the init container never finished).
func TestDataPlaneServiceWaitsForItsDatabaseBeforeStarting(t *testing.T) {
	docs := renderDataPlane(t, dataPlaneValues)
	for _, name := range []string{"loopzero-memory", "loopzero-memory-worker"} {
		init := asWorkload(t, findDoc(t, docs, "Deployment", name)).Spec.Template.Spec.InitContainers
		if len(init) != 1 {
			t.Fatalf("%s: init containers = %+v, want one that waits for Postgres", name, init)
		}
		cmd := strings.Join(init[0].Command, " ")
		if init[0].Image != "registry.example/pgvector:v0.5.1" || !strings.Contains(cmd, "pg_isready") || !strings.Contains(cmd, "loopzero-memory-postgres") || !strings.Contains(cmd, "-U themem") {
			t.Errorf("%s: init container %s runs %q, want pg_isready -U themem against loopzero-memory-postgres", name, init[0].Image, cmd)
		}
	}
}

// loopzero-memory refuses to serve requests as a superuser: row level security does not
// apply to one, so its tenant isolation would hold nothing (measured 2026-09-30, the
// first install crash-looped on exactly that refusal). A service declares the login
// roles it connects as; the chart creates each one without superuser or BYPASSRLS
// before the service starts, and the owner's URI stays on postgres.uriEnv for migrations.
const dataPlaneRoleValues = dataPlaneValues + `  roles:
    - name: themory_app
      uriEnv: LOOPZEROMEM_PG_URI
      passwordKey: app-role-password
`

func roleValues() string {
	return strings.Replace(dataPlaneRoleValues, "uriEnv: LOOPZEROMEM_PG_URI\n  dataPath", "uriEnv: LOOPZEROMEM_MIGRATE_PG_URI\n  dataPath", 1)
}

func TestDataPlaneServiceConnectsAsItsDeclaredRole(t *testing.T) {
	env := asWorkload(t, findDoc(t, renderDataPlane(t, roleValues()), "Deployment", "loopzero-memory")).Spec.Template.Spec.Containers[0].Env

	pw, uri := envIndex(env, "THEMORY_APP_PASSWORD"), envIndex(env, "LOOPZEROMEM_PG_URI")
	if pw < 0 || uri < 0 || pw > uri {
		t.Fatalf("env = %+v, want THEMORY_APP_PASSWORD defined before LOOPZEROMEM_PG_URI", env)
	}
	if want := "postgres://themory_app:$(THEMORY_APP_PASSWORD)@loopzero-memory-postgres:5432/themem"; env[uri].Value != want {
		t.Errorf("LOOPZEROMEM_PG_URI = %q, want %q", env[uri].Value, want)
	}
	if ref, _ := env[pw].ValueFrom["secretKeyRef"].(map[string]any); ref["key"] != "app-role-password" || ref["name"] != "loopzero-memory" {
		t.Errorf("THEMORY_APP_PASSWORD reads %v, want loopzero-memory/app-role-password", env[pw].ValueFrom)
	}
	if m := envIndex(env, "LOOPZEROMEM_MIGRATE_PG_URI"); m < 0 || !strings.HasPrefix(env[m].Value, "postgres://themem:") {
		t.Errorf("the owner's URI is not on LOOPZEROMEM_MIGRATE_PG_URI: %+v", env)
	}
}

func TestDataPlaneServiceCreatesItsRolesBeforeItStarts(t *testing.T) {
	init := asWorkload(t, findDoc(t, renderDataPlane(t, roleValues()), "Deployment", "loopzero-memory")).Spec.Template.Spec.InitContainers

	if len(init) != 2 || init[0].Name != "wait-for-postgres" || init[1].Name != "ensure-roles" {
		t.Fatalf("init containers = %+v, want wait-for-postgres then ensure-roles", init)
	}
	script := strings.Join(init[1].Command, " ")
	for _, want := range []string{"themory_app", "NOSUPERUSER", "NOBYPASSRLS", "ON_ERROR_STOP=1", "THEMORY_APP_PASSWORD"} {
		if !strings.Contains(script, want) {
			t.Errorf("ensure-roles does not carry %q:\n%s", want, script)
		}
	}
	if envIndex(init[1].Env, "THEMORY_APP_PASSWORD") < 0 || envIndex(init[1].Env, "POSTGRES_PASSWORD") < 0 {
		t.Errorf("ensure-roles env = %+v, want the owner's and the role's password", init[1].Env)
	}
}

func TestDataPlaneServiceWithoutRolesConnectsAsTheOwner(t *testing.T) {
	init := asWorkload(t, findDoc(t, renderDataPlane(t, dataPlaneValues), "Deployment", "loopzero-memory")).Spec.Template.Spec.InitContainers
	for _, c := range init {
		if c.Name == "ensure-roles" {
			t.Errorf("a service that declares no role got an ensure-roles init container")
		}
	}
}

// A role name is written into ensure-roles' SQL as a literal, so the schema admits only a
// plain lowercase identifier: a quote in it would end the literal and run what follows.
func TestDataPlaneServiceRefusesARoleNameThatIsNotAnIdentifier(t *testing.T) {
	f := filepath.Join(t.TempDir(), "values.yaml")
	values := strings.Replace(roleValues(), "- name: themory_app", `- name: "x'); DROP TABLE memories; --"`, 1)
	if err := os.WriteFile(f, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := renderChartE(t, uniqueChart(t, "data-plane-service"), "loopzero-memory", nil, f); err == nil {
		t.Error("rendered a role whose name breaks out of the SQL literal")
	}
}

// A service whose migrations are a separate command (loopzero-rag, #474) declares it; the
// chart runs it with the service's own image and environment as the app's last init
// container — after its roles exist, before it serves — so a deploy leaves the schema at
// the version the image expects. Only the app runs it: a worker migrating too would race
// the app for the same schema.
func migrationValues() string {
	return dataPlaneValues + "migrations:\n  command: [\"node\", \"dist/migrate.js\"]\n"
}

func TestDataPlaneServiceMigratesBeforeTheAppStarts(t *testing.T) {
	w := asWorkload(t, findDoc(t, renderDataPlane(t, migrationValues()), "Deployment", "loopzero-memory"))
	init := w.Spec.Template.Spec.InitContainers

	last := init[len(init)-1]
	if last.Name != "migrate" {
		t.Fatalf("init containers = %+v, want migrate last", init)
	}
	if last.Image != "registry.example/loopzero-memory:0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("migrate runs %s, want the service's own image", last.Image)
	}
	if got := strings.Join(last.Command, " "); got != "node dist/migrate.js" {
		t.Errorf("migrate command = %q, want %q", got, "node dist/migrate.js")
	}
	if envIndex(last.Env, "LOOPZEROMEM_PG_URI") < 0 || envIndex(last.Env, "POSTGRES_PASSWORD") < 0 {
		t.Errorf("migrate env = %+v, want the service's database environment", last.Env)
	}
}

func TestDataPlaneServiceWorkersDoNotMigrate(t *testing.T) {
	w := asWorkload(t, findDoc(t, renderDataPlane(t, migrationValues()), "Deployment", "loopzero-memory-worker"))
	for _, c := range w.Spec.Template.Spec.InitContainers {
		if c.Name == "migrate" {
			t.Errorf("the worker runs the migrations too; only the app may")
		}
	}
}

func TestDataPlaneServiceWithoutMigrationsHasNoMigrateStep(t *testing.T) {
	w := asWorkload(t, findDoc(t, renderDataPlane(t, dataPlaneValues), "Deployment", "loopzero-memory"))
	for _, c := range w.Spec.Template.Spec.InitContainers {
		if c.Name == "migrate" {
			t.Errorf("a service that declares no migrations got a migrate init container")
		}
	}
}

// loopzero-rag's server and worker read the same LOOPZERORAG_PG_URI but must connect as different
// roles — rag_app is bound to one workspace by row level security, rag_worker crosses them
// through named policies — and each refuses to boot as the other (#474). A role can name
// the processes it serves ("app" is the service itself); without that it serves them all.
func perProcessRoleValues() string {
	return strings.Replace(dataPlaneValues, "uriEnv: LOOPZEROMEM_PG_URI\n  dataPath", `uriEnv: LOOPZEROMEM_MIGRATE_PG_URI
  roles:
    - { name: themory_app, uriEnv: LOOPZEROMEM_PG_URI, passwordKey: app-role-password, processes: [app] }
    - { name: themory_worker, uriEnv: LOOPZEROMEM_PG_URI, passwordKey: worker-role-password, processes: [worker] }
  dataPath`, 1)
}

func TestDataPlaneServiceConnectsEachProcessAsItsOwnRole(t *testing.T) {
	docs := renderDataPlane(t, perProcessRoleValues())
	for deploy, wantRole := range map[string]string{"loopzero-memory": "themory_app", "loopzero-memory-worker": "themory_worker"} {
		env := asWorkload(t, findDoc(t, docs, "Deployment", deploy)).Spec.Template.Spec.Containers[0].Env
		var uris []string
		for _, e := range env {
			if e.Name == "LOOPZEROMEM_PG_URI" {
				uris = append(uris, e.Value)
			}
		}
		if len(uris) != 1 || !strings.HasPrefix(uris[0], "postgres://"+wantRole+":") {
			t.Errorf("%s: LOOPZEROMEM_PG_URI = %q, want exactly one, as %s", deploy, uris, wantRole)
		}
	}
}

func TestDataPlaneServiceCreatesEveryRoleWhateverProcessItServes(t *testing.T) {
	init := asWorkload(t, findDoc(t, renderDataPlane(t, perProcessRoleValues()), "Deployment", "loopzero-memory")).Spec.Template.Spec.InitContainers
	for _, c := range init {
		if c.Name != "ensure-roles" {
			continue
		}
		script := strings.Join(c.Command, " ")
		for _, role := range []string{"themory_app", "themory_worker"} {
			if !strings.Contains(script, role) {
				t.Errorf("ensure-roles does not create %s", role)
			}
		}
		return
	}
	t.Fatal("no ensure-roles init container")
}
