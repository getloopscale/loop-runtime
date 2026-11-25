// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repositorieshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/getloopscale/loopzero/api/internal/github"
	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// --- Fake RepositoryStore ---

type fakeRepoStore struct {
	repo         *ProjectRepository
	repoByName   *ProjectRepository
	linkErr      error
	unlinkErr    error
	getByRepoErr error
	getByProjErr error
	// Plan J-09 T3.2 — capture autoDeploy passed by handler so tests can assert
	// the request → store mapping (otherwise we couldn't tell true-by-default
	// from true-by-handler-default).
	lastLinkAutoDeploy *bool
	// installationNotVerified makes the installation one the caller's tenant
	// may not use; verifiedErr fails the check. checkedInstallation and
	// checkedTenant record what the handler asked about.
	installationNotVerified bool
	verifiedErr             error
	checkedInstallation     int64
	checkedTenant           string
}

func (f *fakeRepoStore) InstallationVerifiedFor(_ context.Context, installationID int64, tenantID string) (bool, error) {
	f.checkedInstallation, f.checkedTenant = installationID, tenantID
	return !f.installationNotVerified, f.verifiedErr
}

func (f *fakeRepoStore) LinkRepository(_ context.Context, _, _, _ string, _ int64, _ string, autoDeploy bool) error {
	v := autoDeploy
	f.lastLinkAutoDeploy = &v
	return f.linkErr
}

func (f *fakeRepoStore) UnlinkRepository(_ context.Context, _, _ string) error {
	return f.unlinkErr
}

func (f *fakeRepoStore) GetByProject(_ context.Context, _, _ string) (*ProjectRepository, error) {
	return f.repoByName, f.getByProjErr
}

func (f *fakeRepoStore) GetByRepo(_ context.Context, _ string) (*ProjectRepository, error) {
	return f.repo, f.getByRepoErr
}

// A repository is connected only through an installation that GitHub confirmed
// a user of the caller's tenant can access. The installation id comes from the
// request, so every other installation is refused with the answer the
// repository picker gives, and nothing is linked.
func TestRepositories_Connect_RefusesAnInstallationNotVerifiedForTheCaller(t *testing.T) {
	store := &fakeRepoStore{installationNotVerified: true}
	deployer := recordingDeployer{ctxErr: make(chan error, 1)}
	h := NewRepositoryHandler(store).WithAutoDeployer(deployer, context.Background()).WithIntrospector(&fakeIntrospector{})

	body, _ := json.Marshal(map[string]any{"repo": "acme/web", "installation_id": 42})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/shop/connect", bytes.NewReader(body))
	req = routestest.WithChiParam(routestest.WithAuth(req, routestest.DefaultUserContext()), "name", "shop")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
	want := `{"code":"FORBIDDEN","message":"You do not have access to this installation."}`
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Errorf("body = %s\n want = %s", got, want)
	}
	if store.checkedInstallation != 42 || store.checkedTenant != routestest.DefaultUserContext().TenantID {
		t.Errorf("checked installation %d for tenant %q, want 42 for the caller's tenant", store.checkedInstallation, store.checkedTenant)
	}
	if store.lastLinkAutoDeploy != nil {
		t.Error("the repository was linked through an installation the caller may not use")
	}
	select {
	case <-deployer.ctxErr:
		t.Error("a deploy was started through an installation the caller may not use")
	default:
	}
}

func TestRepositories_Connect_AFailedOwnershipCheckLinksNothing(t *testing.T) {
	store := &fakeRepoStore{verifiedErr: errors.New("db down")}
	h := NewRepositoryHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/shop/connect", bytes.NewBufferString(`{"repo":"acme/web","installation_id":42}`))
	req = routestest.WithChiParam(routestest.WithAuth(req, routestest.DefaultUserContext()), "name", "shop")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
	if store.lastLinkAutoDeploy != nil {
		t.Error("the repository was linked although the ownership check failed")
	}
}

// --- Tests ---

func TestRepositories_Connect_HappyPath(t *testing.T) {
	store := &fakeRepoStore{}
	h := NewRepositoryHandler(store)

	body := `{"repo":"owner/my-repo","installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp connectResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Fatal("want ok=true")
	}
	if resp.Repo != "owner/my-repo" {
		t.Fatalf("want repo owner/my-repo, got %s", resp.Repo)
	}
	if resp.Branch != "main" {
		t.Fatalf("want default branch main, got %s", resp.Branch)
	}
}

func TestRepositories_Connect_CustomBranch(t *testing.T) {
	store := &fakeRepoStore{}
	h := NewRepositoryHandler(store)

	body := `{"repo":"owner/my-repo","installation_id":12345,"branch":"develop"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp connectResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Branch != "develop" {
		t.Fatalf("want branch develop, got %s", resp.Branch)
	}
}

func TestRepositories_Connect_MissingRepo(t *testing.T) {
	h := NewRepositoryHandler(&fakeRepoStore{})

	body := `{"installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Connect_AlreadyLinkedToOtherProject(t *testing.T) {
	store := &fakeRepoStore{
		repo: &ProjectRepository{
			TenantID:    "tn-other-tenant",
			ProjectName: "other-project",
		},
	}
	h := NewRepositoryHandler(store)

	body := `{"repo":"owner/my-repo","installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Connect_Unauthenticated(t *testing.T) {
	h := NewRepositoryHandler(&fakeRepoStore{})

	body := `{"repo":"owner/my-repo","installation_id":12345}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Disconnect_HappyPath(t *testing.T) {
	store := &fakeRepoStore{}
	h := NewRepositoryHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/my-api/connect", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Disconnect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// CHG-2026-04-23-AUDIT-21: DELETE /projects/{name}/connect must return 404
// when the project has no linked repository. Previously it returned 200 OK
// (silent success) because UnlinkRepository ignored RowsAffected=0. Same
// bug class as AUDIT-6 (team member remove) and AUDIT-12 (environment
// delete).
func TestRepositories_Disconnect_NotLinked_Returns404(t *testing.T) {
	store := &fakeRepoStore{
		unlinkErr: fmt.Errorf("store: %w", ports.ErrRepositoryNotLinked),
	}
	h := NewRepositoryHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/my-api/connect", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Disconnect(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (not linked), got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"] != "REPOSITORY_NOT_LINKED" {
		t.Errorf("want code=REPOSITORY_NOT_LINKED, got %v", body["code"])
	}
}

func TestRepositories_GetConnection_Found(t *testing.T) {
	store := &fakeRepoStore{
		repoByName: &ProjectRepository{
			ProjectName:   "my-api",
			RepoFullName:  "owner/my-repo",
			DefaultBranch: "main",
			AutoDeploy:    true,
			Provider:      "github",
		},
	}
	h := NewRepositoryHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/connect", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.GetConnection(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_GetConnection_NotFound(t *testing.T) {
	store := &fakeRepoStore{repoByName: nil}
	h := NewRepositoryHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/connect", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.GetConnection(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_GetConnection_Unauthenticated(t *testing.T) {
	h := NewRepositoryHandler(&fakeRepoStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/connect", nil)
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.GetConnection(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// Plan J-09 T3.2 (2026-06-05) — auto_deploy flag persistence tests.

func TestRepositories_Connect_AutoDeployFalse_Persisted(t *testing.T) {
	store := &fakeRepoStore{}
	h := NewRepositoryHandler(store)

	body := `{"repo":"owner/r","installation_id":42,"auto_deploy":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if store.lastLinkAutoDeploy == nil || *store.lastLinkAutoDeploy != false {
		t.Fatalf("expected store.LinkRepository called with autoDeploy=false; got %v", store.lastLinkAutoDeploy)
	}
	// Response field must echo the persisted value.
	if !bytes.Contains(w.Body.Bytes(), []byte(`"auto_deploy":false`)) {
		t.Errorf("response should echo auto_deploy=false; body=%s", w.Body.String())
	}
}

// Plan J-09 T5.1 (2026-06-05) — loopzero.yaml validation tests.

type fakeIntrospector struct {
	branchErr error
	fileBytes []byte
	fileErr   error
	// connect-then-deploy 2026-06-09: HEAD commit metadata for the
	// Vercel-canonical immediate deploy on link. Nil head + nil headErr
	// disables the deploy path silently — matches the production behavior
	// when WithAutoDeployer is unwired (most existing tests do not wire it).
	head    *github.HeadCommitInfo
	headErr error
}

func (f *fakeIntrospector) BranchExists(_ context.Context, _ int64, _, _, _ string) error {
	return f.branchErr
}

func (f *fakeIntrospector) GetFileContent(_ context.Context, _ int64, _, _, _, _ string) ([]byte, error) {
	if f.fileErr != nil {
		return nil, f.fileErr
	}
	return f.fileBytes, nil
}

func (f *fakeIntrospector) GetHeadCommit(_ context.Context, _ int64, _, _, _ string) (*github.HeadCommitInfo, error) {
	if f.headErr != nil {
		return nil, f.headErr
	}
	return f.head, nil
}

func connectReq(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	return req
}

func TestRepositories_Connect_BranchNotFound_400(t *testing.T) {
	intro := &fakeIntrospector{branchErr: &github.BranchNotFoundError{Owner: "owner", Repo: "r", Ref: "main"}}
	h := NewRepositoryHandler(&fakeRepoStore{}).WithIntrospector(intro)

	w := httptest.NewRecorder()
	h.Connect(w, connectReq(`{"repo":"owner/r","installation_id":42,"branch":"main"}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("BRANCH_NOT_FOUND")) {
		t.Errorf("want BRANCH_NOT_FOUND code, got %s", w.Body.String())
	}
}

func TestRepositories_Connect_MissingLoopZeroYaml_400(t *testing.T) {
	intro := &fakeIntrospector{fileErr: &github.FileNotFoundError{Path: "loopzero.yaml"}}
	h := NewRepositoryHandler(&fakeRepoStore{}).WithIntrospector(intro)

	w := httptest.NewRecorder()
	h.Connect(w, connectReq(`{"repo":"owner/r","installation_id":42}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("MISSING_LOOPZERO_YAML")) {
		t.Errorf("want MISSING_LOOPZERO_YAML, got %s", w.Body.String())
	}
}

func TestRepositories_Connect_EmptyLoopZeroYaml_400(t *testing.T) {
	intro := &fakeIntrospector{fileBytes: []byte("   \n\t  ")}
	h := NewRepositoryHandler(&fakeRepoStore{}).WithIntrospector(intro)

	w := httptest.NewRecorder()
	h.Connect(w, connectReq(`{"repo":"owner/r","installation_id":42}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("INVALID_LOOPZERO_YAML")) {
		t.Errorf("want INVALID_LOOPZERO_YAML, got %s", w.Body.String())
	}
}

func TestRepositories_Connect_InvalidYaml_400(t *testing.T) {
	intro := &fakeIntrospector{fileBytes: []byte("not: valid: yaml: ::\n  - broken")}
	h := NewRepositoryHandler(&fakeRepoStore{}).WithIntrospector(intro)

	w := httptest.NewRecorder()
	h.Connect(w, connectReq(`{"repo":"owner/r","installation_id":42}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("INVALID_LOOPZERO_YAML")) {
		t.Errorf("want INVALID_LOOPZERO_YAML, got %s", w.Body.String())
	}
}

const deployableLoopZeroYaml = "project: my-app\napps:\n  api:\n    type: server\n    runtime: node\n    start: node server.js\n"

// Connect refuses the descriptor a push could not deploy: a loopzero.yaml that
// declares no app used to link fine and then build nothing on every push.
func TestRepositories_Connect_LoopZeroYamlWithoutApps_400(t *testing.T) {
	intro := &fakeIntrospector{fileBytes: []byte("name: my-app\nframework: node\n")}
	h := NewRepositoryHandler(&fakeRepoStore{}).WithIntrospector(intro)

	w := httptest.NewRecorder()
	h.Connect(w, connectReq(`{"repo":"owner/r","installation_id":42}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("INVALID_LOOPZERO_YAML")) {
		t.Errorf("want INVALID_LOOPZERO_YAML, got %s", w.Body.String())
	}
}

func TestRepositories_Connect_LoopZeroYamlExists_200(t *testing.T) {
	intro := &fakeIntrospector{fileBytes: []byte(deployableLoopZeroYaml)}
	h := NewRepositoryHandler(&fakeRepoStore{}).WithIntrospector(intro)

	w := httptest.NewRecorder()
	h.Connect(w, connectReq(`{"repo":"owner/r","installation_id":42}`))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepositories_Connect_AutoDeployOmitted_DefaultsTrue(t *testing.T) {
	store := &fakeRepoStore{}
	h := NewRepositoryHandler(store)

	body := `{"repo":"owner/r","installation_id":42}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Connect(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if store.lastLinkAutoDeploy == nil || *store.lastLinkAutoDeploy != true {
		t.Fatalf("expected store.LinkRepository called with autoDeploy=true (default); got %v", store.lastLinkAutoDeploy)
	}
}

// recordingDeployer reports the context the connect-then-deploy ran under.
type recordingDeployer struct{ ctxErr chan error }

func (d recordingDeployer) HandlePush(ctx context.Context, _ github.PushRequest) error {
	d.ctxErr <- ctx.Err()
	return nil
}

// The deploy a connect triggers runs after the response, so it must be bound
// to the server's lifetime: when the server shuts down, the deploy is
// cancelled rather than cut mid-way by process exit with nothing recorded.
func TestRepositories_Connect_DeployIsBoundToTheServerLifetime(t *testing.T) {
	serverCtx, stopServer := context.WithCancel(context.Background())
	stopServer() // the server is already shutting down
	deployer := recordingDeployer{ctxErr: make(chan error, 1)}
	intro := &fakeIntrospector{fileBytes: []byte(deployableLoopZeroYaml), head: &github.HeadCommitInfo{SHA: "abc123"}}
	h := NewRepositoryHandler(&fakeRepoStore{}).WithIntrospector(intro).WithAutoDeployer(deployer, serverCtx)

	body := `{"repo":"owner/r","installation_id":42}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/connect", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	h.Connect(httptest.NewRecorder(), req)

	select {
	case err := <-deployer.ctxErr:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("deploy context error = %v, want context.Canceled from the stopped server", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connect did not trigger the deploy")
	}
}
