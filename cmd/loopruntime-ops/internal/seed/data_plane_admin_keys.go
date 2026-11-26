// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// DataPlaneAdminKeysSeed gives loopruntime-cloud the platform-admin key of every data-plane
// service running on the core cluster (#470).
//
// loopruntime-cloud is the edge's authorisation brain for /v1/{memory,rag,lens}/*: its
// forward-auth endpoints validate the caller's loopruntime_live_* key and mint a per-tenant
// key on the service with a platform-admin key. Without that key every mint fails and
// the edge answers 503; and nothing on dev-public ever wrote one — the rag and lens
// ExternalSecrets pointed at an OpenBao property no step writes, through a
// ClusterSecretStore that does not exist here (measured 2026-09-30: SecretSyncedError).
//
// Per enabled service (its Deployment exists in loopruntime-data-plane):
//
//  1. A key already in loopruntime-system that still authenticates against the service's
//     cheapest admin read is kept. Minting again would leave the old key alive and
//     orphaned, and for memory and rag would need a bootstrap that is single-use.
//  2. Otherwise a key is minted with the service's bootstrap token and written.
//  3. A refused bootstrap with no working key is a FAIL that says so. The seed never
//     resets the service's database to un-spend a bootstrap: that is a decision about
//     somebody's data, not a seeding step.
//
// Then loopruntime-cloud, which reads configuration only at boot, is rolled onto the keys and
// waited for. The pod template carries a digest of the Secrets' versions it was rolled
// onto, so a run that stored a key and failed before the rollout is repaired by the
// next run, and a run where nothing changed touches nothing.
type DataPlaneAdminKeysSeed struct{}

func NewDataPlaneAdminKeysSeed() *DataPlaneAdminKeysSeed { return &DataPlaneAdminKeysSeed{} }
func (s *DataPlaneAdminKeysSeed) Name() string           { return "data-plane-admin-keys" }

const (
	dataPlaneNamespace    = "loopruntime-data-plane"
	dataPlaneBootstrapKey = "admin-bootstrap-token"
	// dataPlaneKeysAnnotation records, on loopruntime-cloud's pod template, which versions of the
	// admin-key Secrets its pods were started with. Changing it is what rolls them.
	dataPlaneKeysAnnotation    = "runtime.getloopscale.com/data-plane-admin-keys"
	loopruntimeCloudNamespace  = "loopruntime-system"
	loopruntimeCloudDeployment = "loopruntime-cloud"
	dataPlaneKeyName           = "gateway-platform-admin"
)

// dataPlaneAdminKey is how one data-plane service issues and checks loopruntime-cloud's key.
// The mint bodies are the ones loopruntime-workspace's dev/{memory,rag,lens}-up.sh used; the
// response field differs per service (rag answers raw_key).
type dataPlaneAdminKey struct {
	service       string // Deployment, pod label and bootstrap Secret in loopruntime-data-plane
	port          int    // the app container's port
	adminReadPath string // a cheap GET that answers 200 only to a live admin key
	secretName    string // the loopruntime-system Secret loopruntime-cloud reads
	envKey        string // the key in it, which is also loopruntime-cloud's environment variable
	scopes        []string
	workspaceID   string // "" when the service's bootstrap mint takes none
	mintField     string
	edgePath      string // where the public edge serves the service
}

var dataPlaneAdminKeys = []dataPlaneAdminKey{
	{
		service: "loopruntime-memory", port: 8080, adminReadPath: "/v1/admin/keys/list",
		secretName: "loopruntime-memory-platform-admin", envKey: "THEMORY_PLATFORM_ADMIN_KEY",
		scopes:    []string{"memory:platform-admin", "memory:admin"},
		mintField: "secret", edgePath: "/v1/memory/",
	},
	{ // #nosec G101 -- Secret resource and key NAMES, not credential values
		service: "loopruntime-rag", port: 8787, adminReadPath: "/v1/admin/keys",
		secretName: "loopruntime-rag-admin-key", envKey: "RAG_PLATFORM_ADMIN_KEY",
		scopes:      []string{"rag:platform-admin", "rag:admin", "rag:read", "rag:write", "rag:delete"},
		workspaceID: "default", mintField: "raw_key", edgePath: "/v1/rag/",
	},
	{
		service: "loopruntime-lens", port: 4318, adminReadPath: "/v1/admin/keys",
		secretName: "loopruntime-lens-admin-key", envKey: "LENS_PLATFORM_ADMIN_KEY",
		scopes: []string{"observability:platform-admin", "observability:admin", "observability:read",
			"observability:write", "observability:delete"},
		workspaceID: "default", mintField: "secret", edgePath: "/v1/lens/",
	},
}

// dataPlaneHTTP bounds every call to a service through its tunnel.
var dataPlaneHTTP = &http.Client{Timeout: 15 * time.Second}

// loopruntimeCloudSettle is how long the seed waits for loopruntime-cloud after rolling it: the
// release's startup probe allows a slow boot, and a restart without the wait races
// whatever runs next against pods still on the old keys.
const loopruntimeCloudSettle = 180 * time.Second

// BootstrapRefusedError reports that a service refused its bootstrap token. For memory
// and rag the token is single-use, so this is what a lost key looks like.
type BootstrapRefusedError struct {
	Service string
	Status  int
	Detail  string
}

func (e *BootstrapRefusedError) Error() string {
	return fmt.Sprintf("%s refused the bootstrap token (HTTP %d: %s): no stored key authenticates and none can be minted — "+
		"the bootstrap was already spent on a key this cluster no longer holds; recover that key, or rotate the service's "+
		"bootstrap deliberately (this seed never resets database rows)", e.Service, e.Status, e.Detail)
}

func (s *DataPlaneAdminKeysSeed) Run(d Deps) Result {
	enabled, err := deployedDataPlane(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if len(enabled) == 0 {
		return Result{Verdict: SKIP, Reason: "no data-plane service is deployed in " + dataPlaneNamespace}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would ensure loopruntime-cloud holds a working admin key for %s", serviceNames(enabled))}
	}
	// The services are independent: one that cannot be keyed does not stop the others,
	// and loopruntime-cloud is still rolled onto the keys that exist. The run FAILs afterwards,
	// naming every service it could not key.
	var notes, failures []string
	for _, spec := range enabled {
		note, err := ensureDataPlaneAdminKey(d, spec)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		notes = append(notes, note)
	}
	rolled, err := rollLoopRuntimeCloudOntoKeys(d, enabled)
	if err != nil {
		failures = append(failures, err.Error())
	} else {
		notes = append(notes, rolled)
	}
	if len(failures) > 0 {
		return Result{Verdict: FAIL, Reason: strings.Join(failures, "; ") + " (done: " + strings.Join(notes, "; ") + ")"}
	}
	return Result{Verdict: PASS, Reason: strings.Join(notes, "; ")}
}

// Verify re-checks what Run guarantees: every stored key authenticates, and the running
// loopruntime-cloud was started with the Secrets as they are now.
func (s *DataPlaneAdminKeysSeed) Verify(d Deps) Result {
	enabled, err := deployedDataPlane(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if len(enabled) == 0 {
		return Result{Verdict: SKIP, Reason: "no data-plane service is deployed in " + dataPlaneNamespace}
	}
	for _, spec := range enabled {
		key, err := storedAdminKey(d, spec)
		if err != nil {
			return Result{Verdict: FAIL, Reason: err.Error()}
		}
		if key == "" {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s holds no %s", loopruntimeCloudNamespace, spec.secretName, spec.envKey)}
		}
		ok, err := withDataPlaneTunnel(d, spec, func(base string) (bool, error) {
			return adminKeyAuthenticates(d.Ctx, dataPlaneHTTP, base, spec, key)
		})
		if err != nil {
			return Result{Verdict: FAIL, Reason: err.Error()}
		}
		if !ok {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s no longer accepts the key in %s/%s", spec.service, loopruntimeCloudNamespace, spec.secretName)}
		}
	}
	want, err := adminKeysDigest(d, enabled)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	var cloud appsv1.Deployment
	if err := d.CtrlClient.Get(d.Ctx, ctrlclient.ObjectKey{Namespace: loopruntimeCloudNamespace, Name: loopruntimeCloudDeployment}, &cloud); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v", loopruntimeCloudNamespace, loopruntimeCloudDeployment, err)}
	}
	if cloud.Spec.Template.Annotations[dataPlaneKeysAnnotation] != want {
		return Result{Verdict: FAIL, Reason: "loopruntime-cloud was not started with the admin keys as they are stored now — run the seed again"}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("loopruntime-cloud holds a working admin key for %s", serviceNames(enabled))}
}

// deployedDataPlane returns the specs of the services whose Deployment exists: that is
// what `dataPlane.services` produces, read from the cluster rather than re-derived.
func deployedDataPlane(d Deps) ([]dataPlaneAdminKey, error) {
	if d.CtrlClient == nil {
		return nil, fmt.Errorf("no core client to find the data-plane Deployments")
	}
	var out []dataPlaneAdminKey
	for _, spec := range dataPlaneAdminKeys {
		var dep appsv1.Deployment
		err := d.CtrlClient.Get(d.Ctx, ctrlclient.ObjectKey{Namespace: dataPlaneNamespace, Name: spec.service}, &dep)
		switch {
		case err == nil:
			out = append(out, spec)
		case !isNotFoundErr(err):
			return nil, fmt.Errorf("get deployment %s/%s: %w", dataPlaneNamespace, spec.service, err)
		}
	}
	return out, nil
}

func serviceNames(specs []dataPlaneAdminKey) string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.service)
	}
	return strings.Join(names, ", ")
}

// storedAdminKey returns the key loopruntime-cloud would read, or "" when there is none. Only a
// missing Secret is an absence; any other error is returned, because minting on it would
// spend a single-use bootstrap on an API hiccup.
func storedAdminKey(d Deps, spec dataPlaneAdminKey) (string, error) {
	sec, err := d.Kube.GetSecret(d.Ctx, loopruntimeCloudNamespace, spec.secretName)
	if err != nil {
		if isNotFoundErr(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s/%s: %w", loopruntimeCloudNamespace, spec.secretName, err)
	}
	return string(sec.Data[spec.envKey]), nil
}

func ensureDataPlaneAdminKey(d Deps, spec dataPlaneAdminKey) (string, error) {
	stored, err := storedAdminKey(d, spec)
	if err != nil {
		return "", err
	}
	var minted string
	_, err = withDataPlaneTunnel(d, spec, func(base string) (bool, error) {
		if stored != "" {
			ok, err := adminKeyAuthenticates(d.Ctx, dataPlaneHTTP, base, spec, stored)
			if err != nil || ok {
				return ok, err
			}
		}
		bootstrap, err := dataPlaneBootstrapToken(d, spec)
		if err != nil {
			return false, err
		}
		key, err := mintPlatformAdminKey(d.Ctx, dataPlaneHTTP, base, spec, bootstrap)
		if err != nil {
			return false, err
		}
		// A key the service will not accept for its own admin read would fail every tenant
		// mint loopruntime-cloud attempts; catch it here rather than behind the edge.
		ok, err := adminKeyAuthenticates(d.Ctx, dataPlaneHTTP, base, spec, key)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, fmt.Errorf("%s minted a key that its own admin read rejects", spec.service)
		}
		minted = key
		return true, nil
	})
	if err != nil {
		return "", err
	}
	if minted == "" {
		return spec.service + ": kept the stored key", nil
	}
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx, dataPlaneAdminKeySecret(spec, minted)); err != nil {
		return "", fmt.Errorf("write %s/%s: %w", loopruntimeCloudNamespace, spec.secretName, err)
	}
	return spec.service + ": minted a new key", nil
}

// withDataPlaneTunnel opens a tunnel to the service's app pod and runs fn against it.
func withDataPlaneTunnel(d Deps, spec dataPlaneAdminKey, fn func(base string) (bool, error)) (bool, error) {
	if d.PortForwardCtor == nil {
		return false, fmt.Errorf("PortForwardCtor not wired (composition root gap)")
	}
	tunnel, err := d.PortForwardCtor(d.Ctx, TunnelTarget{
		Namespace: dataPlaneNamespace,
		PodLabels: PodLabels{"app.kubernetes.io/name": spec.service, "app.kubernetes.io/component": "app"},
		// The admin read answers 401 unauthenticated, which is what the tunnel probe waits for.
		RemotePort: spec.port, ProbePath: spec.adminReadPath,
	})
	if err != nil {
		return false, fmt.Errorf("port-forward %s: %w", spec.service, err)
	}
	defer tunnel.Close()
	return fn(fmt.Sprintf("http://127.0.0.1:%d", tunnel.LocalPort))
}

func dataPlaneBootstrapToken(d Deps, spec dataPlaneAdminKey) (string, error) {
	sec, err := d.Kube.GetSecret(d.Ctx, dataPlaneNamespace, spec.service)
	if err != nil {
		return "", fmt.Errorf("%s has no working admin key and no %s to mint one (read %s/%s: %w)",
			spec.service, dataPlaneBootstrapKey, dataPlaneNamespace, spec.service, err)
	}
	token := string(sec.Data[dataPlaneBootstrapKey])
	if token == "" {
		return "", fmt.Errorf("%s has no working admin key and %s/%s carries no %s",
			spec.service, dataPlaneNamespace, spec.service, dataPlaneBootstrapKey)
	}
	return token, nil
}

// adminKeyAuthenticates reports whether the service accepts key for its admin read: 200
// is yes, 401 and 403 are no, anything else is an error — an outage is not a verdict on
// the key.
func adminKeyAuthenticates(ctx context.Context, hc *http.Client, base string, spec dataPlaneAdminKey, key string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+spec.adminReadPath, nil)
	if err != nil {
		return false, fmt.Errorf("build %s admin read: %w", spec.service, err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := hc.Do(req)
	if err != nil {
		return false, fmt.Errorf("%s admin read: %w", spec.service, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, nil
	default:
		return false, fmt.Errorf("%s admin read %s answered HTTP %d — cannot tell whether the stored key is valid",
			spec.service, spec.adminReadPath, resp.StatusCode)
	}
}

// mintPlatformAdminKey mints loopruntime-cloud's key with the bootstrap token. A 401 or 403 is a
// *BootstrapRefusedError. The success body carries the key, so it never reaches an error.
func mintPlatformAdminKey(ctx context.Context, hc *http.Client, base string, spec dataPlaneAdminKey, bootstrap string) (string, error) {
	payload := map[string]any{"name": dataPlaneKeyName, "scopes": spec.scopes}
	if spec.workspaceID != "" {
		payload["workspace_id"] = spec.workspaceID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode %s mint: %w", spec.service, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/admin/keys", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build %s mint: %w", spec.service, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bootstrap)
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s mint: %w", spec.service, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read %s mint response: %w", spec.service, err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", &BootstrapRefusedError{Service: spec.service, Status: resp.StatusCode, Detail: snippet(raw)}
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return "", fmt.Errorf("%s mint answered HTTP %d: %s", spec.service, resp.StatusCode, snippet(raw))
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", fmt.Errorf("%s mint answered HTTP %d with a body that is not JSON", spec.service, resp.StatusCode)
	}
	key, _ := fields[spec.mintField].(string)
	if key == "" {
		return "", fmt.Errorf("%s mint answered HTTP %d without a %q field", spec.service, resp.StatusCode, spec.mintField)
	}
	return key, nil
}

func dataPlaneAdminKeySecret(spec dataPlaneAdminKey, key string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: loopruntimeCloudNamespace,
			Name:      spec.secretName,
			Labels: map[string]string{
				"app.kubernetes.io/name":       spec.secretName,
				"app.kubernetes.io/managed-by": "loopruntime-ops-seed-data-plane-admin-keys",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{spec.envKey: []byte(key)},
	}
}

// adminKeysDigest names the admin-key Secrets as they are stored now by their resource
// versions — never their contents, so nothing derived from a key lands on a pod template.
// A Secret not written yet is left out: it has nothing for loopruntime-cloud to load.
func adminKeysDigest(d Deps, specs []dataPlaneAdminKey) (string, error) {
	parts := make([]string, 0, len(specs))
	for _, spec := range specs {
		sec, err := d.Kube.GetSecret(d.Ctx, loopruntimeCloudNamespace, spec.secretName)
		if isNotFoundErr(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read %s/%s: %w", loopruntimeCloudNamespace, spec.secretName, err)
		}
		parts = append(parts, spec.secretName+"@"+sec.ResourceVersion)
	}
	if len(parts) == 0 {
		return "", nil
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8]), nil
}

// rollLoopRuntimeCloudOntoKeys rolls loopruntime-cloud when its pods were not started with the Secrets
// as stored now, and waits for it. A loopruntime-cloud not deployed yet reads them when it starts.
func rollLoopRuntimeCloudOntoKeys(d Deps, specs []dataPlaneAdminKey) (string, error) {
	want, err := adminKeysDigest(d, specs)
	if err != nil {
		return "", err
	}
	if want == "" {
		return "no admin key stored — loopruntime-cloud left alone", nil
	}
	var cloud appsv1.Deployment
	key := ctrlclient.ObjectKey{Namespace: loopruntimeCloudNamespace, Name: loopruntimeCloudDeployment}
	if err := d.CtrlClient.Get(d.Ctx, key, &cloud); err != nil {
		if isNotFoundErr(err) {
			return "loopruntime-cloud not deployed yet — nothing to restart", nil
		}
		return "", fmt.Errorf("get %s/%s: %w", loopruntimeCloudNamespace, loopruntimeCloudDeployment, err)
	}
	if cloud.Spec.Template.Annotations[dataPlaneKeysAnnotation] == want {
		return "loopruntime-cloud already runs on these keys", nil
	}
	base := cloud.DeepCopy()
	if cloud.Spec.Template.Annotations == nil {
		cloud.Spec.Template.Annotations = map[string]string{}
	}
	cloud.Spec.Template.Annotations[dataPlaneKeysAnnotation] = want
	if err := d.CtrlClient.Patch(d.Ctx, &cloud, ctrlclient.MergeFrom(base)); err != nil {
		return "", fmt.Errorf("roll %s/%s onto the admin keys: %w", loopruntimeCloudNamespace, loopruntimeCloudDeployment, err)
	}
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, loopruntimeCloudNamespace, loopruntimeCloudDeployment, d.settleTimeout(loopruntimeCloudSettle)); err != nil {
		return "", fmt.Errorf("loopruntime-cloud did not settle on the admin keys: %w", err)
	}
	return "loopruntime-cloud rolled onto the keys", nil
}
