// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environmentshttp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

type recordingPathPreparer struct {
	calls []string
	err   error
}

func (p *recordingPathPreparer) EnsureEnvironmentPath(_ context.Context, tenantID, project, environment string) error {
	p.calls = append(p.calls, tenantID+"/"+project+"/"+environment)
	return p.err
}

func createEnvironment(h *EnvironmentHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Create(w, req)
	return w
}

// getloopscale/loopzero#134: the directory the Application reads is written when
// the environment is created, not by its first release.
func TestCreatePreparesTheGitOpsPathOfTheNewEnvironment(t *testing.T) {
	preparer := &recordingPathPreparer{}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithGitOpsPathPreparer(preparer)

	w := createEnvironment(h, `{"name":"qa","project":"web"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	want := routestest.DefaultUserContext().TenantID + "/web/qa"
	if len(preparer.calls) != 1 || preparer.calls[0] != want {
		t.Errorf("prepared = %v, want [%s]", preparer.calls, want)
	}
}

// orderedEnvMgr records, in a list shared with the path preparer, when the
// environment is added.
type orderedEnvMgr struct {
	fakeEnvMgr
	steps *[]string
}

func (m *orderedEnvMgr) AddEnvironment(ctx context.Context, tenantID, project, name string) error {
	*m.steps = append(*m.steps, "environment")
	return m.fakeEnvMgr.AddEnvironment(ctx, tenantID, project, name)
}

type orderedPathPreparer struct{ steps *[]string }

func (p orderedPathPreparer) EnsureEnvironmentPath(context.Context, string, string, string) error {
	*p.steps = append(*p.steps, "path")
	return nil
}

// Adding the environment creates its Argo CD Application. Seen on staging: with
// the path written afterwards the Application read a missing directory first and
// stayed in ComparisonError for 3m20s, until Argo CD reconciled again.
func TestCreateWritesTheGitOpsPathBeforeItAddsTheEnvironment(t *testing.T) {
	var steps []string
	h := NewEnvironmentHandler(&orderedEnvMgr{steps: &steps}).WithGitOpsPathPreparer(orderedPathPreparer{steps: &steps})

	w := createEnvironment(h, `{"name":"qa","project":"web"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if len(steps) != 2 || steps[0] != "path" || steps[1] != "environment" {
		t.Errorf("steps = %v, want [path environment]", steps)
	}
}

func TestCreateDoesNotPrepareAPathForARequestItRefuses(t *testing.T) {
	preparer := &recordingPathPreparer{}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithGitOpsPathPreparer(preparer)

	w := createEnvironment(h, `{"name":"Not A Name","project":"web"}`)

	if w.Code < 400 {
		t.Fatalf("status = %d, want a refusal", w.Code)
	}
	if len(preparer.calls) != 0 {
		t.Errorf("prepared = %v, want none", preparer.calls)
	}
}

// A path that cannot be written does not refuse the environment: the first
// release writes the directory anyway.
func TestCreateStillAnswers201WhenTheGitOpsPathCannotBeWritten(t *testing.T) {
	preparer := &recordingPathPreparer{err: errors.New("gitea unreachable")}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithGitOpsPathPreparer(preparer)

	w := createEnvironment(h, `{"name":"qa","project":"web"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}
