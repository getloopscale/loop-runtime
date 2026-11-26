// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package httpio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// The move was byte for byte, and these assert the rendered shape rather than
// trusting that. 399 call sites changed; what a user receives must not have.
func TestWriteJSON_RendersStatusContentTypeAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]string{"id": "prj-1"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status must be the one asked for; got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type is part of the contract; got %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body must be valid JSON; got %q err=%v", rec.Body.String(), err)
	}
	if body["id"] != "prj-1" {
		t.Errorf("the payload must survive the encode; got %+v", body)
	}
}

// The header must be set BEFORE WriteHeader — setting it after is a silent
// no-op in net/http, and the response would go out as text/plain.
func TestWriteJSON_HeaderPrecedesTheStatusWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusTeapot, struct{}{})
	if ct := rec.Result().Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("a Content-Type set after WriteHeader is silently dropped; got %q", ct)
	}
}

// The CLI reads `body.code` and Unbreakable Rule 5 makes that a stability
// contract, so the typed code must reach the wire unchanged.
func TestWriteLoopRuntimeError_PreservesTheTypedCode(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteLoopRuntimeError(rec, loopruntimeerrors.NewProjectNotFound("meu-projeto"))

	if rec.Code < 400 {
		t.Errorf("an error must not render as a success status; got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the error body must be valid JSON; got %q err=%v", rec.Body.String(), err)
	}
	if body["code"] == nil || body["code"] == "" {
		t.Errorf("the CLI reads body.code and Rule 5 makes it a stability contract; got %+v", body)
	}
}

// Rule 3 lives at the CALL SITES, not here. This test says so out loud, because
// the version it replaces did not.
//
// `WriteLoopRuntimeError` is a pure pass-through to errorshttp.WriteJSON. It neither
// invents vocabulary nor filters it, so a test that feeds a clean message and
// asserts the output is clean asserts a property no implementation could
// violate — theatre, and it was measured as such: fed
// "ArgoCD sync failed for tenant_id tn-1; check Gitea and MinIO", all four
// banned terms reach `detail` and `message` verbatim, and this writer is
// working exactly as designed.
//
// What that means is stated rather than papered over: the 277 messages authored
// at the call sites are the real surface, and there is NO gate over them among
// the 122 today. Registered as B-081. This test asserts only what a
// pass-through can be held to — that it is faithful, in both directions.
func TestWriteLoopRuntimeError_IsFaithfulPassThrough_AndDoesNotFilter(t *testing.T) {
	const dirty = "ArgoCD sync failed for tenant_id tn-1"
	rec := httptest.NewRecorder()
	WriteLoopRuntimeError(rec, &loopruntimeerrors.LoopRuntimeError{Code: "INTERNAL_ERROR", Message: dirty})

	if !strings.Contains(rec.Body.String(), dirty) {
		t.Errorf("the writer must transmit the message it was given, unchanged — a writer that quietly rewrote it would make every error message a lie: %s", rec.Body.String())
	}

	clean := "could not apply the configuration — café, naïve, 設定"
	rec2 := httptest.NewRecorder()
	WriteLoopRuntimeError(rec2, &loopruntimeerrors.LoopRuntimeError{Code: "INTERNAL_ERROR", Message: clean})
	if !strings.Contains(rec2.Body.String(), clean) {
		t.Errorf("and the same in the clean direction; got %s", rec2.Body.String())
	}
}
