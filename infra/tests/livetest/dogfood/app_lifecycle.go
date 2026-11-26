// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dogfood

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The building blocks of the app lifecycle journey
// (journey_app_lifecycle_live_test.go): a real app deployed through the public
// API, the way the CLI deploys one. Nothing here is tied to Ginkgo, so the
// hermetic tests in app_lifecycle_test.go cover it without staging.

const (
	// ProbeProject is the project the journey creates and deploys to. It is
	// the journey's own: nothing else uses the name, so a rerun reuses what
	// it finds. See EnsureProject for the one case it deploys elsewhere.
	ProbeProject = "staging-validate"
	// ProbeApp is the one service of the probe app (fixtures/env-probe).
	ProbeApp = "app"
	// ProbeEnvironment is where the journey deploys.
	ProbeEnvironment = "production"
	// ProbePort is the port the probe app listens on.
	ProbePort = 3000

	// CellContextsEnv names the kubectl contexts of the cells, separated by
	// spaces. The journey reads the app's Rollout from the one that has it.
	CellContextsEnv = "LOOPRUNTIME_DOGFOOD_CELL_CONTEXTS"

	probeFixtureRoot  = "fixtures/env-probe"
	probeManifestPath = ".loopruntime/services.json"
)

// all: so the embed includes .loopruntime, which holds the manifest.
//
//go:embed all:fixtures/env-probe
var probeFixture embed.FS

// ProbeAppManifest is the services.json of the probe app for a deploy to
// project. The API files a source under the project its manifest names, so
// the fixture's own name is replaced when the journey deploys elsewhere.
func ProbeAppManifest(project string) ([]byte, error) {
	raw, err := probeFixture.ReadFile(probeFixtureRoot + "/" + probeManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read the probe app manifest: %w", err)
	}
	if project == ProbeProject {
		return raw, nil
	}
	// The manifest has one "project" line; replacing the name there keeps
	// every other byte, and with them the key order the fixture is read in.
	fixtureLine := fmt.Sprintf("%q: %q", "project", ProbeProject)
	if bytes.Count(raw, []byte(fixtureLine)) != 1 {
		return nil, fmt.Errorf("the probe app manifest does not name project %s exactly once", ProbeProject)
	}
	return bytes.Replace(raw, []byte(fixtureLine), []byte(fmt.Sprintf("%q: %q", "project", project)), 1), nil
}

// ProbeAppBundle is the source bundle of the probe app for a deploy to
// project: a tar.gz of the fixture. The bytes are the same on every call for
// a project (sorted entries, no timestamps, no owner), because the API derives
// the source id from the content and reuses the image of a source it has
// already built.
func ProbeAppBundle(project string) ([]byte, error) {
	var paths []string
	err := fs.WalkDir(probeFixture, probeFixtureRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk the probe app fixture: %w", err)
	}
	sort.Strings(paths)

	var out bytes.Buffer
	gz, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(gz)
	for _, path := range paths {
		data, err := probeFixture.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if strings.HasSuffix(path, "/"+probeManifestPath) {
			if data, err = ProbeAppManifest(project); err != nil {
				return nil, err
			}
		}
		header := &tar.Header{
			Name: strings.TrimPrefix(path, probeFixtureRoot+"/"), Mode: 0o644, Size: int64(len(data)),
			Typeflag: tar.TypeReg, Format: tar.FormatPAX,
		}
		if err := tw.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("bundle %s: %w", path, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("bundle %s: %w", path, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// NewSecretValue returns a random value for a secret: 64 hex characters.
func NewSecretValue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("random secret value: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// SecretDigest is the SHA-256 of a value, in hex: what the probe app answers
// for a variable. The journey compares digests, never values.
func SecretDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// TenantNamespace is the namespace of a project's environment on its cell.
func TenantNamespace(tenantID, project, environment string) string {
	return "loopruntime-" + tenantID + "-" + project + "-" + environment
}

// WaitFor calls check every interval until it reports true, and fails
// after timeout with the step and the last state check described. Every wait
// of the journey goes through it, so none is unbounded and each failure says
// which step stopped and what it last saw.
func WaitFor(ctx context.Context, step string, timeout, interval time.Duration, check func(context.Context) (bool, string)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	last := "nothing observed"
	for {
		ok, state := check(ctx)
		if state != "" {
			last = state
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: not within %s; last state: %s", step, timeout, last)
		case <-time.After(interval):
		}
	}
}

// BuildOutcome reduces a build status to running, succeeded or failed.
func BuildOutcome(status string) string {
	switch strings.ToUpper(status) {
	case "SUCCEEDED":
		return "succeeded"
	case "FAILED", "CANCELLED", "CANCELED", "TIMED_OUT", "DEPLOY_FAILED", "BUILD_FAILED":
		return "failed"
	default:
		return "running"
	}
}

// Engine calls the engine API through the cloud service's /engine proxy, with
// the validation identity's token: the path the console and the CLI use.
type Engine struct {
	base   string
	token  string
	client *http.Client
}

// NewEngine returns a client for env's engine, authenticated with token.
func NewEngine(env Env, token string) *Engine {
	return &Engine{base: strings.TrimRight(env.CloudBaseURL, "/") + "/engine", token: token, client: env.HTTPClient}
}

// do sends one request and decodes a 2xx JSON answer into out (nil to ignore
// it). A refusal names the request and the API's answer. It never includes
// what was sent, which may be a secret value.
func (e *Engine) do(ctx context.Context, method, path string, headers map[string]string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, e.base+path, body)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.token)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("%s %s: read the answer: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s answered HTTP %d: %s", method, path, resp.StatusCode, truncate(string(raw), 400))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: the answer is not JSON: %w", method, path, err)
	}
	return nil
}

func (e *Engine) json(ctx context.Context, method, path string, headers map[string]string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode the body of %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(raw)
		if headers == nil {
			headers = map[string]string{}
		}
		headers["Content-Type"] = "application/json"
	}
	return e.do(ctx, method, path, headers, body, out)
}

// ProbeTarget is the project the journey deploys to.
type ProbeTarget struct {
	Name     string
	TenantID string
	// Reused is true when Name is not the journey's own project: the plan
	// allowed no other, and this one held the slot.
	Reused bool
}

// EnsureProject returns the project to deploy to: name, created unless an
// earlier run left it.
//
// The validation tenant may be on a plan that allows one project. When
// creating name is refused for that reason and exactly one project exists, the
// journey deploys to that one: the tenant exists to be validated on, and a
// project left there would otherwise fail every validation until someone
// deleted it. With several projects in the way it fails, naming them.
func (e *Engine) EnsureProject(ctx context.Context, name string) (ProbeTarget, error) {
	var list struct {
		Projects []struct {
			Name string `json:"name"`
			Team string `json:"team"`
		} `json:"projects"`
	}
	if err := e.json(ctx, http.MethodGet, "/api/v1/projects", nil, nil, &list); err != nil {
		return ProbeTarget{}, err
	}
	var others []string
	for _, project := range list.Projects {
		if project.Name == name {
			return ProbeTarget{Name: name, TenantID: project.Team}, nil
		}
		others = append(others, project.Name)
	}
	var created struct {
		Team string `json:"team"`
	}
	err := e.json(ctx, http.MethodPost, "/api/v1/projects", idempotencyKey(), map[string]string{"name": name}, &created)
	switch {
	case err == nil && created.Team != "":
		return ProbeTarget{Name: name, TenantID: created.Team}, nil
	case err == nil:
		return ProbeTarget{}, fmt.Errorf("POST /api/v1/projects answered no team for %s", name)
	case isPlanRefusal(err) && len(list.Projects) == 1:
		return ProbeTarget{Name: list.Projects[0].Name, TenantID: list.Projects[0].Team, Reused: true}, nil
	default:
		return ProbeTarget{}, fmt.Errorf("%w (the tenant's other projects: %v)", err, others)
	}
}

// isPlanRefusal reports whether the API refused a project for the plan's
// project count.
func isPlanRefusal(err error) bool {
	return strings.Contains(err.Error(), "HTTP 422") && strings.Contains(err.Error(), "projects-exceed-plan")
}

// UploadSource uploads a source bundle with its manifest and returns the
// source id. The manifest part goes first: the API refuses another order.
func (e *Engine) UploadSource(ctx context.Context, manifest, bundle []byte) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := writePart(form, "services_json", "services.json", "application/json", manifest); err != nil {
		return "", err
	}
	if err := writePart(form, "bundle", "bundle.tar.gz", "application/octet-stream", bundle); err != nil {
		return "", err
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	var answer struct {
		CommitSHA string `json:"commit_sha"`
	}
	headers := map[string]string{"Content-Type": form.FormDataContentType()}
	if err := e.do(ctx, http.MethodPost, "/api/v1/source/upload", headers, &body, &answer); err != nil {
		return "", err
	}
	if answer.CommitSHA == "" {
		return "", fmt.Errorf("POST /api/v1/source/upload answered no commit_sha")
	}
	return answer.CommitSHA, nil
}

func writePart(form *multipart.Writer, field, filename, contentType string, data []byte) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
	header.Set("Content-Type", contentType)
	part, err := form.CreatePart(header)
	if err != nil {
		return fmt.Errorf("multipart part %s: %w", field, err)
	}
	_, err = part.Write(data)
	return err
}

// SubmitBuild submits a build (POST /api/v1/builds) and returns its id.
func (e *Engine) SubmitBuild(ctx context.Context, request map[string]any) (string, error) {
	var answer struct {
		BuildID string `json:"build_id"`
	}
	if err := e.json(ctx, http.MethodPost, "/api/v1/builds", idempotencyKey(), request, &answer); err != nil {
		return "", err
	}
	if answer.BuildID == "" {
		return "", fmt.Errorf("POST /api/v1/builds answered no build_id")
	}
	return answer.BuildID, nil
}

// Build is the state of a build.
type Build struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// GetBuild reads a build.
func (e *Engine) GetBuild(ctx context.Context, id string) (Build, error) {
	var build Build
	err := e.json(ctx, http.MethodGet, "/api/v1/builds/"+url.PathEscape(id), nil, nil, &build)
	return build, err
}

// ProjectStatus is what the journey reads from a project's status.
type ProjectStatus struct {
	URL           string `json:"url"`
	SyncStatus    string `json:"sync_status"`
	ActiveRelease *struct {
		ID      string `json:"id"`
		BuildID string `json:"build_id"`
		Status  string `json:"status"`
	} `json:"active_release"`
}

// Status reads a project's status in an environment.
func (e *Engine) Status(ctx context.Context, project, environment string) (ProjectStatus, error) {
	var status ProjectStatus
	path := "/api/v1/projects/" + url.PathEscape(project) + "/status?environment=" + url.QueryEscape(environment)
	err := e.json(ctx, http.MethodGet, path, nil, nil, &status)
	return status, err
}

// SetSecret sets one secret of an app. The value is sent and never echoed.
func (e *Engine) SetSecret(ctx context.Context, project, environment, app, name, value string) error {
	path := "/api/v1/secrets?project=" + url.QueryEscape(project)
	body := map[string]string{"name": name, "value": value, "environment": environment, "app": app}
	return e.json(ctx, http.MethodPost, path, idempotencyKey(), body, nil)
}

// Scale sets the replica count of an app.
func (e *Engine) Scale(ctx context.Context, project, environment, app string, replicas int) error {
	path := "/api/v1/projects/" + url.PathEscape(project) + "/scale"
	body := map[string]any{"environment": environment, "app": app, "replicas": replicas}
	return e.json(ctx, http.MethodPost, path, idempotencyKey(), body, nil)
}

// Autoscale saves an autoscaling range for an app, with CPU and memory
// utilization targets in percent.
func (e *Engine) Autoscale(ctx context.Context, project, environment, app string, minReplicas, maxReplicas, cpu, memory int) error {
	path := "/api/v1/projects/" + url.PathEscape(project) + "/scale"
	body := map[string]any{"environment": environment, "app": app, "min": minReplicas, "max": maxReplicas, "cpu": cpu, "memory": memory}
	return e.json(ctx, http.MethodPost, path, idempotencyKey(), body, nil)
}

// MetricSeries is the answer of the metrics endpoint for one signal.
type MetricSeries struct {
	Status string `json:"status"`
	Data   []struct {
		T int64   `json:"t"`
		V float64 `json:"v"`
	} `json:"data"`
	CollectingHint string `json:"collecting_hint"`
}

// Metric reads one signal of a project over a range.
func (e *Engine) Metric(ctx context.Context, project, signal, rng string) (MetricSeries, error) {
	var series MetricSeries
	path := "/api/v1/projects/" + url.PathEscape(project) + "/metrics?signal=" + url.QueryEscape(signal) + "&range=" + url.QueryEscape(rng)
	err := e.json(ctx, http.MethodGet, path, nil, nil, &series)
	return series, err
}

// LogStreamHas opens the runtime log stream of a project and reports whether a
// line carrying needle arrived before ctx ended or the server closed the
// stream.
func (e *Engine) LogStreamHas(ctx context.Context, project, environment, since, needle string) (bool, error) {
	path := "/api/v1/projects/" + url.PathEscape(project) + "/logs/stream?env=" + url.QueryEscape(environment) + "&since=" + url.QueryEscape(since)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base+path, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+e.token)
	// A stream outlives the client's request timeout; ctx bounds it instead.
	streaming := &http.Client{Transport: e.client.Transport}
	resp, err := streaming.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		return false, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return false, fmt.Errorf("GET %s answered HTTP %d: %s", path, resp.StatusCode, truncate(string(raw), 400))
	}
	return StreamHasLine(ctx, resp.Body, needle)
}

// StreamHasLine reads a server-sent event stream until a line carries needle.
// It reports false when the stream ends, or ctx does, without one.
func StreamHasLine(ctx context.Context, stream io.Reader, needle string) (bool, error) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), needle) {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return false, fmt.Errorf("read the event stream: %w", err)
	}
	return false, nil
}

// ProbeAnswer is what the probe app says about one variable.
type ProbeAnswer struct {
	Present bool    `json:"present"`
	SHA256  *string `json:"sha256"`
}

// ProbeVariable asks the deployed probe app about a variable.
func ProbeVariable(ctx context.Context, client *http.Client, appURL, name string) (ProbeAnswer, error) {
	var answer ProbeAnswer
	status, raw, err := getApp(ctx, client, strings.TrimRight(appURL, "/")+"/env/"+url.PathEscape(name))
	if err != nil {
		return answer, err
	}
	if status != http.StatusOK {
		return answer, fmt.Errorf("GET /env/%s on the app answered HTTP %d", name, status)
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return answer, fmt.Errorf("GET /env/%s on the app: the answer is not JSON: %w", name, err)
	}
	return answer, nil
}

// CallApp requests a path of the deployed app and returns the status.
func CallApp(ctx context.Context, client *http.Client, appURL, pathAndQuery string) (int, error) {
	status, _, err := getApp(ctx, client, strings.TrimRight(appURL, "/")+pathAndQuery)
	return status, err
}

func getApp(ctx context.Context, client *http.Client, target string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw, err
}

// Kubectl runs kubectl with args and returns its standard output.
type Kubectl func(ctx context.Context, args ...string) (string, error)

// HostKubectl runs the kubectl on PATH, with the caller's kubeconfig.
func HostKubectl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...) //nolint:gosec // the arguments are the journey's own: a context, a namespace and a Rollout name
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, truncate(strings.TrimSpace(stderr.String()), 300))
	}
	return strings.TrimSpace(string(out)), nil
}

// CellContexts are the kubectl contexts CellContextsEnv names.
func CellContexts() []string {
	return strings.Fields(os.Getenv(CellContextsEnv))
}

// RolloutReplicas reads spec.replicas of a Rollout from the first cell that
// has it, and says which cell that was.
func RolloutReplicas(ctx context.Context, kubectl Kubectl, cells []string, namespace, name string) (replicas int, cell string, err error) {
	if len(cells) == 0 {
		return 0, "", fmt.Errorf("no cell context to read the Rollout from: set %s to the kubectl contexts of the cells", CellContextsEnv)
	}
	var failures []string
	for _, candidate := range cells {
		out, err := kubectl(ctx, "--context", candidate, "-n", namespace, "get", "rollouts.argoproj.io", name, "-o", "jsonpath={.spec.replicas}")
		if err != nil {
			failures = append(failures, candidate+": "+err.Error())
			continue
		}
		replicas, convErr := strconv.Atoi(strings.TrimSpace(out))
		if convErr != nil {
			failures = append(failures, fmt.Sprintf("%s: spec.replicas is %q", candidate, out))
			continue
		}
		return replicas, candidate, nil
	}
	return 0, "", fmt.Errorf("no cell has the Rollout %s/%s: %s", namespace, name, strings.Join(failures, "; "))
}

// HPAState is what an HPA says about the metrics it scales on.
type HPAState struct {
	// Metrics are the resource metrics of the spec, such as cpu and memory.
	Metrics []string
	// ScalingActive is the HPA's own condition: true once it computes a
	// replica count from its metrics.
	ScalingActive bool
	// Reason is the reason of that condition, such as ValidMetricFound or
	// FailedGetResourceMetric.
	Reason string
	Cell   string
}

// HPAStatus reads an HPA from the first cell that has it.
func HPAStatus(ctx context.Context, kubectl Kubectl, cells []string, namespace, name string) (HPAState, error) {
	if len(cells) == 0 {
		return HPAState{}, fmt.Errorf("no cell context to read the HPA from: set %s to the kubectl contexts of the cells", CellContextsEnv)
	}
	const condition = `{.status.conditions[?(@.type=="ScalingActive")]`
	jsonpath := "jsonpath={.spec.metrics[*].resource.name}|" + condition + ".status}|" + condition + ".reason}"
	var failures []string
	for _, candidate := range cells {
		out, err := kubectl(ctx, "--context", candidate, "-n", namespace, "get", "horizontalpodautoscalers.autoscaling", name, "-o", jsonpath)
		if err != nil {
			failures = append(failures, candidate+": "+err.Error())
			continue
		}
		fields := strings.Split(strings.TrimSpace(out), "|")
		if len(fields) != 3 {
			failures = append(failures, fmt.Sprintf("%s: unexpected answer %q", candidate, out))
			continue
		}
		return HPAState{
			Metrics:       strings.Fields(fields[0]),
			ScalingActive: fields[1] == "True",
			Reason:        fields[2],
			Cell:          candidate,
		}, nil
	}
	return HPAState{}, fmt.Errorf("no cell has the HPA %s/%s: %s", namespace, name, strings.Join(failures, "; "))
}

func idempotencyKey() map[string]string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return map[string]string{"Idempotency-Key": hex.EncodeToString(raw)}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
