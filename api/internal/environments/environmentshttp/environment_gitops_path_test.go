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

func TestCreateDoesNotPrepareAPathForAnEnvironmentItRefused(t *testing.T) {
	preparer := &recordingPathPreparer{}
	h := NewEnvironmentHandler(&fakeEnvMgr{addErr: errors.New("add failed")}).WithGitOpsPathPreparer(preparer)

	createEnvironment(h, `{"name":"qa","project":"web"}`)

	if len(preparer.calls) != 0 {
		t.Errorf("prepared = %v, want none", preparer.calls)
	}
}

// The environment exists by the time the path is written. Answering an error
// would make a retry fail with ENV_ALREADY_EXISTS, and the first release writes
// the directory anyway.
func TestCreateStillAnswers201WhenTheGitOpsPathCannotBeWritten(t *testing.T) {
	preparer := &recordingPathPreparer{err: errors.New("gitea unreachable")}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithGitOpsPathPreparer(preparer)

	w := createEnvironment(h, `{"name":"qa","project":"web"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}
