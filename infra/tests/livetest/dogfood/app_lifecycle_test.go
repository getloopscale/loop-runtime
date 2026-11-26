// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dogfood

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Hermetic tests of what the app lifecycle journey is built from. The journey
// itself (journey_app_lifecycle_live_test.go) runs against staging only.

func bundleEntries(t *testing.T, bundle []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("the bundle is not gzip: %v", err)
	}
	entries := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatalf("read the bundle: %v", err)
		}
		body, _ := io.ReadAll(tr)
		entries[header.Name] = string(body)
	}
}

func TestProbeAppBundle_CarriesTheAppAndItsManifest(t *testing.T) {
	bundle, err := ProbeAppBundle(ProbeProject)
	if err != nil {
		t.Fatalf("ProbeAppBundle: %v", err)
	}

	entries := bundleEntries(t, bundle)

	for _, name := range []string{"package.json", "package-lock.json", "server.js", ".loopzero/services.json"} {
		if entries[name] == "" {
			t.Errorf("the bundle has no %s; it has %v", name, keysOf(entries))
		}
	}
}

// The source id of an upload is the hash of its content, and a deploy of a
// source that already has an image reuses the image. A bundle that changed
// between two runs would rebuild the app on every validation.
func TestProbeAppBundle_IsTheSameBytesEveryTime(t *testing.T) {
	first, err := ProbeAppBundle(ProbeProject)
	if err != nil {
		t.Fatalf("ProbeAppBundle: %v", err)
	}
	time.Sleep(1100 * time.Millisecond) // a second later, so a timestamp would show
	second, err := ProbeAppBundle(ProbeProject)
	if err != nil {
		t.Fatalf("ProbeAppBundle: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Error("two bundles of the same fixture differ")
	}
}

// The manifest is what the API validates on upload. The canonical build has
// no Dockerfile to fall back on: without `start` and a lockfile it fails.
func TestProbeAppManifest_NamesTheProjectAndAServiceTheCanonicalBuildAccepts(t *testing.T) {
	manifest, err := ProbeAppManifest(ProbeProject)
	if err != nil {
		t.Fatalf("ProbeAppManifest: %v", err)
	}
	var parsed struct {
		Version  int              `json:"version"`
		Project  string           `json:"project"`
		Services []map[string]any `json:"services"`
	}
	if err := json.Unmarshal(manifest, &parsed); err != nil {
		t.Fatalf("services.json is not JSON: %v", err)
	}

	if parsed.Version != 2 || parsed.Project != ProbeProject {
		t.Errorf("version=%d project=%q, want version 2 and project %q", parsed.Version, parsed.Project, ProbeProject)
	}
	if len(parsed.Services) != 1 || parsed.Services[0]["name"] != ProbeApp {
		t.Fatalf("services = %v, want the one service %q", parsed.Services, ProbeApp)
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "api", "internal", "source", "services.schema.json"))
	if err != nil {
		t.Fatalf("read the API's services.json schema: %v", err)
	}
	var doc struct {
		Definitions struct {
			Service struct {
				Required []string `json:"required"`
			} `json:"Service"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil || len(doc.Definitions.Service.Required) == 0 {
		t.Fatalf("the schema names no required service field: %v", err)
	}
	for _, field := range doc.Definitions.Service.Required {
		if _, ok := parsed.Services[0][field]; !ok {
			t.Errorf("the service has no %q, which the API's schema requires", field)
		}
	}
	if parsed.Services[0]["runtime"] != "node" {
		t.Errorf("runtime = %v, want node", parsed.Services[0]["runtime"])
	}
}

// The API files a source under the project its manifest names, so a journey
// that deploys to another project sends that name, in the upload's manifest
// and in the copy inside the bundle.
func TestProbeAppManifest_NamesTheProjectItIsDeployedTo(t *testing.T) {
	manifest, err := ProbeAppManifest("leftover")
	if err != nil {
		t.Fatalf("ProbeAppManifest: %v", err)
	}
	bundle, err := ProbeAppBundle("leftover")
	if err != nil {
		t.Fatalf("ProbeAppBundle: %v", err)
	}

	for where, raw := range map[string]string{"the upload's manifest": string(manifest), "the bundle's manifest": bundleEntries(t, bundle)[".loopzero/services.json"]} {
		var parsed struct {
			Project  string           `json:"project"`
			Services []map[string]any `json:"services"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			t.Fatalf("%s is not JSON: %v", where, err)
		}
		if parsed.Project != "leftover" || len(parsed.Services) != 1 {
			t.Errorf("%s names project %q with %d service(s), want leftover with 1", where, parsed.Project, len(parsed.Services))
		}
	}
}

func TestSecretDigest_IsTheSHA256TheProbeAppAnswers(t *testing.T) {
	// printf abc | sha256sum
	if got := SecretDigest("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("SecretDigest(abc) = %s", got)
	}
}

func TestNewSecretValue_IsRandomAndLongEnough(t *testing.T) {
	a, err := NewSecretValue()
	if err != nil {
		t.Fatalf("NewSecretValue: %v", err)
	}
	b, _ := NewSecretValue()
	if len(a) < 32 || a == b {
		t.Errorf("two values of %d and %d characters, equal=%v; want 32 or more and different", len(a), len(b), a == b)
	}
}

// engineStub records what the journey's client sends.
type engineStub struct {
	t        *testing.T
	uploads  []uploadSeen
	requests []string
	bodies   map[string]map[string]any
	headers  map[string]http.Header
	answers  map[string]stubAnswer
}

type uploadSeen struct {
	parts  []string
	bundle []byte
}

type stubAnswer struct {
	status int
	body   string
}

func newEngineStub(t *testing.T) (*engineStub, Env) {
	t.Helper()
	stub := &engineStub{t: t, bodies: map[string]map[string]any{}, headers: map[string]http.Header{}, answers: map[string]stubAnswer{}}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	return stub, Env{CloudBaseURL: srv.URL, HTTPClient: srv.Client()}
}

func (s *engineStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.RequestURI()
	s.requests = append(s.requests, key)
	s.headers[key] = r.Header.Clone()
	if r.URL.Path == "/engine/api/v1/source/upload" {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			s.t.Errorf("upload content type: %v", err)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		var seen uploadSeen
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			seen.parts = append(seen.parts, part.FormName())
			if part.FormName() == "bundle" {
				seen.bundle = data
			}
		}
		s.uploads = append(s.uploads, seen)
	} else if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.bodies[key] = body
	}
	answer, ok := s.answers[key]
	if !ok {
		answer = stubAnswer{http.StatusNotFound, `{"code":"NOT_STUBBED","message":"no answer for ` + key + `"}`}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(answer.status)
	_, _ = io.WriteString(w, answer.body)
}

func TestEngine_UploadSendsTheManifestBeforeTheBundle(t *testing.T) {
	stub, env := newEngineStub(t)
	stub.answers["POST /engine/api/v1/source/upload"] = stubAnswer{200, `{"commit_sha":"ch-abc","apps":["app"]}`}
	engine := NewEngine(env, "token-1")

	sourceID, err := engine.UploadSource(context.Background(), []byte(`{"version":2}`), []byte("bundle-bytes"))

	if err != nil || sourceID != "ch-abc" {
		t.Fatalf("UploadSource = %q, %v; want ch-abc", sourceID, err)
	}
	if len(stub.uploads) != 1 || strings.Join(stub.uploads[0].parts, ",") != "services_json,bundle" {
		t.Fatalf("multipart parts = %+v, want services_json then bundle: the API refuses another order", stub.uploads)
	}
	if string(stub.uploads[0].bundle) != "bundle-bytes" {
		t.Errorf("the bundle part carries %q", stub.uploads[0].bundle)
	}
	if got := stub.headers["POST /engine/api/v1/source/upload"].Get("Authorization"); got != "Bearer token-1" {
		t.Errorf("the upload was not authenticated with the token")
	}
}

func TestEngine_ARefusalNamesTheRequestAndTheAnswerButNotWhatWasSent(t *testing.T) {
	stub, env := newEngineStub(t)
	stub.answers["POST /engine/api/v1/secrets?project=staging-validate"] = stubAnswer{422, `{"code":"VALIDATION_FAILED","message":"name is invalid"}`}
	engine := NewEngine(env, "token-1")

	err := engine.SetSecret(context.Background(), ProbeProject, "production", ProbeApp, "PROBE_ALPHA", "s3cr3t-value-never-shown")

	if err == nil {
		t.Fatal("a 422 was reported as success")
	}
	for _, want := range []string{"POST", "/api/v1/secrets", "422", "VALIDATION_FAILED"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not name %s", err, want)
		}
	}
	if strings.Contains(err.Error(), "s3cr3t-value-never-shown") || strings.Contains(err.Error(), "token-1") {
		t.Errorf("the error carries the secret value or the token: %v", err)
	}
}

func TestEngine_SetSecretSendsTheValueForTheAppAndEnvironment(t *testing.T) {
	stub, env := newEngineStub(t)
	key := "POST /engine/api/v1/secrets?project=staging-validate"
	stub.answers[key] = stubAnswer{200, `{"name":"PROBE_ALPHA","created":true}`}

	if err := NewEngine(env, "token-1").SetSecret(context.Background(), ProbeProject, "production", ProbeApp, "PROBE_ALPHA", "v1"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}

	body := stub.bodies[key]
	if body["name"] != "PROBE_ALPHA" || body["value"] != "v1" || body["environment"] != "production" || body["app"] != ProbeApp {
		t.Errorf("the request names %v/%v/%v, want PROBE_ALPHA for app %s in production", body["name"], body["environment"], body["app"], ProbeApp)
	}
}

const planRefusal = `{"type":"https://getloopscale.com/errors/projects-exceed-plan","title":"Projects Exceed Plan","status":422,"detail":"the Starter plan allows up to 1"}`

func TestEngine_EnsureProjectCreatesItsProjectOrReusesWhatItFinds(t *testing.T) {
	for name, tc := range map[string]struct {
		list        string
		create      stubAnswer
		wantProject string
		wantCreate  bool
		wantReused  bool
	}{
		"an empty tenant": {
			list: `{"projects":[]}`, create: stubAnswer{201, `{"name":"staging-validate","team":"tn-1"}`},
			wantProject: ProbeProject, wantCreate: true,
		},
		"its own project left by an earlier run": {
			list:        `{"projects":[{"name":"staging-validate","team":"tn-1"}]}`,
			wantProject: ProbeProject,
		},
		// The validation tenant is on a plan that allows one project. A
		// project someone left there holds the only slot, and the tenant
		// exists to be validated on, so the journey deploys to it.
		"another project holds the plan's only slot": {
			list: `{"projects":[{"name":"leftover","team":"tn-1"}]}`, create: stubAnswer{422, planRefusal},
			wantProject: "leftover", wantCreate: true, wantReused: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			stub, env := newEngineStub(t)
			stub.answers["GET /engine/api/v1/projects"] = stubAnswer{200, tc.list}
			if tc.create.status != 0 {
				stub.answers["POST /engine/api/v1/projects"] = tc.create
			}

			got, err := NewEngine(env, "token-1").EnsureProject(context.Background(), ProbeProject)

			if err != nil {
				t.Fatalf("EnsureProject: %v", err)
			}
			if got.Name != tc.wantProject || got.TenantID != "tn-1" || got.Reused != tc.wantReused {
				t.Errorf("EnsureProject = %+v, want project %s of tn-1, reused=%v", got, tc.wantProject, tc.wantReused)
			}
			created := strings.Contains(strings.Join(stub.requests, "\n"), "POST /engine/api/v1/projects")
			if created != tc.wantCreate {
				t.Errorf("tried to create = %v, want %v; requests: %v", created, tc.wantCreate, stub.requests)
			}
		})
	}
}

// With several projects in the way the journey cannot tell which one is its
// to deploy over, and any other refusal is not about the plan.
func TestEngine_EnsureProjectFailsWhenItCannotTellWhichProjectToUse(t *testing.T) {
	for name, tc := range map[string]struct {
		list   string
		create stubAnswer
		want   string
	}{
		"two other projects":               {`{"projects":[{"name":"a","team":"tn-1"},{"name":"b","team":"tn-1"}]}`, stubAnswer{422, planRefusal}, "[a b]"},
		"a refusal that is not the plan's": {`{"projects":[{"name":"a","team":"tn-1"}]}`, stubAnswer{500, `{"code":"INTERNAL_ERROR"}`}, "HTTP 500"},
	} {
		t.Run(name, func(t *testing.T) {
			stub, env := newEngineStub(t)
			stub.answers["GET /engine/api/v1/projects"] = stubAnswer{200, tc.list}
			stub.answers["POST /engine/api/v1/projects"] = tc.create

			_, err := NewEngine(env, "token-1").EnsureProject(context.Background(), ProbeProject)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a failure naming %s", err, tc.want)
			}
		})
	}
}

func TestEngine_SubmitBuildSendsAnIdempotencyKey(t *testing.T) {
	stub, env := newEngineStub(t)
	stub.answers["POST /engine/api/v1/builds"] = stubAnswer{202, `{"build_id":"build-1"}`}

	id, err := NewEngine(env, "token-1").SubmitBuild(context.Background(), map[string]any{"project_id": ProbeProject})

	if err != nil || id != "build-1" {
		t.Fatalf("SubmitBuild = %q, %v", id, err)
	}
	if stub.headers["POST /engine/api/v1/builds"].Get("Idempotency-Key") == "" {
		t.Error("the build was submitted without an Idempotency-Key, which the API requires")
	}
}

func TestBuildOutcome_TellsRunningFromDoneFromFailed(t *testing.T) {
	for status, want := range map[string]string{
		"SUCCEEDED": "succeeded", "FAILED": "failed", "CANCELLED": "failed", "TIMED_OUT": "failed",
		"QUEUED": "running", "BUILDING": "running", "DEPLOYING": "running", "": "running",
	} {
		if got := BuildOutcome(status); got != want {
			t.Errorf("BuildOutcome(%q) = %s, want %s", status, got, want)
		}
	}
}

func TestStreamHasLine_FindsTheMarkerInAnEventStream(t *testing.T) {
	stream := "event: log\ndata: {\"line\":\"{\\\"msg\\\":\\\"listening\\\"}\"}\n\n" +
		": keepalive\n\n" +
		"event: log\ndata: {\"line\":\"{\\\"msg\\\":\\\"request\\\",\\\"marker\\\":\\\"mk-123\\\"}\"}\n\n"

	found, err := StreamHasLine(context.Background(), strings.NewReader(stream), "mk-123")
	if err != nil || !found {
		t.Fatalf("StreamHasLine = %v, %v; want the marker found", found, err)
	}

	found, err = StreamHasLine(context.Background(), strings.NewReader(stream), "mk-999")
	if err != nil || found {
		t.Fatalf("StreamHasLine = %v, %v; want not found when the stream ends without the marker", found, err)
	}
}

func TestRolloutReplicas_ReadsSpecReplicasFromTheCellThatHasTheRollout(t *testing.T) {
	var calls []string
	kubectl := func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[1] == "cell-0" {
			return "", fmt.Errorf(`Error from server (NotFound): namespaces "ns-1" not found`)
		}
		return "2", nil
	}

	replicas, cell, err := RolloutReplicas(context.Background(), kubectl, []string{"cell-0", "cell-1"}, "ns-1", "staging-validate-app")

	if err != nil || replicas != 2 || cell != "cell-1" {
		t.Fatalf("RolloutReplicas = %d, %q, %v; want 2 on cell-1", replicas, cell, err)
	}
	want := "--context cell-1 -n ns-1 get rollouts.argoproj.io staging-validate-app -o jsonpath={.spec.replicas}"
	if calls[len(calls)-1] != want {
		t.Errorf("kubectl ran with %q, want %q", calls[len(calls)-1], want)
	}
}

func TestRolloutReplicas_FailsWhenNoCellHasTheRollout(t *testing.T) {
	kubectl := func(context.Context, ...string) (string, error) { return "", errors.New("not found") }

	if _, _, err := RolloutReplicas(context.Background(), kubectl, []string{"cell-0"}, "ns-1", "r"); err == nil {
		t.Fatal("no error although no cell has the rollout")
	}
	if _, _, err := RolloutReplicas(context.Background(), kubectl, nil, "ns-1", "r"); err == nil || !strings.Contains(err.Error(), CellContextsEnv) {
		t.Fatalf("err = %v, want it to name %s when no cell context is configured", err, CellContextsEnv)
	}
}

func TestTenantNamespace_IsTheEnvironmentsNamespaceOnTheCell(t *testing.T) {
	if got := TenantNamespace("tn-550e0845313d5feb", "loop-app", "production"); got != "loopruntime-tn-550e0845313d5feb-loop-app-production" {
		t.Errorf("TenantNamespace = %s", got)
	}
}

func TestWaitFor_ReportsTheStepAndTheLastStateWhenTheDeadlinePasses(t *testing.T) {
	attempts := 0
	err := WaitFor(context.Background(), "step 9: the widget turns blue", 60*time.Millisecond, 10*time.Millisecond, func(context.Context) (bool, string) {
		attempts++
		return false, "still red"
	})

	if err == nil || attempts < 2 {
		t.Fatalf("err = %v after %d attempts, want a timeout after several", err, attempts)
	}
	for _, want := range []string{"step 9: the widget turns blue", "still red", "60ms"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not carry %q", err, want)
		}
	}
}

func TestWaitFor_StopsAsSoonAsTheConditionHolds(t *testing.T) {
	attempts := 0
	err := WaitFor(context.Background(), "step", time.Minute, time.Millisecond, func(context.Context) (bool, string) {
		attempts++
		return attempts == 3, "not yet"
	})

	if err != nil || attempts != 3 {
		t.Fatalf("err = %v after %d attempts, want success on the third", err, attempts)
	}
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestHPAStatus_ReadsTheMetricsAndWhetherTheHPACanScale(t *testing.T) {
	var calls []string
	kubectl := func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[1] == "cell-0" {
			return "", fmt.Errorf(`Error from server (NotFound): horizontalpodautoscalers "x" not found`)
		}
		return "cpu memory|True|ValidMetricFound", nil
	}

	got, err := HPAStatus(context.Background(), kubectl, []string{"cell-0", "cell-1"}, "ns-1", "staging-validate-app")

	if err != nil {
		t.Fatalf("HPAStatus: %v", err)
	}
	if strings.Join(got.Metrics, ",") != "cpu,memory" || !got.ScalingActive || got.Reason != "ValidMetricFound" || got.Cell != "cell-1" {
		t.Errorf("HPAStatus = %+v, want cpu and memory, active, on cell-1", got)
	}
	if !strings.Contains(calls[len(calls)-1], "get horizontalpodautoscalers.autoscaling staging-validate-app") {
		t.Errorf("kubectl ran with %q", calls[len(calls)-1])
	}
}

func TestHPAStatus_ReportsAnHPAThatReadsNoMetric(t *testing.T) {
	// What the cells answered before they had a metrics server.
	kubectl := func(context.Context, ...string) (string, error) {
		return "cpu|False|FailedGetResourceMetric", nil
	}

	got, err := HPAStatus(context.Background(), kubectl, []string{"cell-0"}, "ns-1", "app")

	if err != nil || got.ScalingActive || got.Reason != "FailedGetResourceMetric" {
		t.Errorf("HPAStatus = %+v, %v; want inactive with the reason", got, err)
	}
}

func TestHPAStatus_FailsWhenNoCellHasTheHPA(t *testing.T) {
	kubectl := func(context.Context, ...string) (string, error) { return "", errors.New("not found") }

	if _, err := HPAStatus(context.Background(), kubectl, []string{"cell-0"}, "ns-1", "app"); err == nil {
		t.Fatal("no error although no cell has the HPA")
	}
}
