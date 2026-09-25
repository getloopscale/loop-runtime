// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"

	"github.com/getloopscale/loopzero/api/internal/buildplane/source"
)

// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T3.3 — handler-level discriminator
// ladder tests. Closes loopzero-cloud#31 typed PushBundleError mapping while
// preserving the 2 pre-existing discriminators (EC-3 absorption):
//   - errors.Is(err, source.ErrInvalidTenantOrProject) → 400 INVALID_TENANT_OR_PROJECT
//   - gitea-race substring match → 409 DEPLOY_IN_PROGRESS (F-03 fix 2026-05-14)
//   - errors.As(&pushErr) → 500 SOURCE_UPLOAD_FAILED with reason-specific Message (NEW)
//   - untyped fallback → 500 SOURCE_UPLOAD_FAILED neutral Message
//
// The discriminator ORDER is load-bearing. The 3 tests below lock each branch.

// fakeFixedErrSourceStore returns a fixed error from PushBundle. Used to
// exercise the handler's discriminator ladder without spinning up a real
// Gitea backend.
type fakeFixedErrSourceStore struct {
	err error
}

func (f *fakeFixedErrSourceStore) PushBundle(_ context.Context, _, _ string, bundle io.Reader) (source.PushResult, error) {
	_, _ = io.Copy(io.Discard, bundle)
	return source.PushResult{}, f.err
}

// buildValidUploadRequest constructs a minimal canonical multipart upload
// request with valid services.json + gzip-magic bundle bytes. The handler
// proceeds past validation and calls PushBundle, where our fake store
// returns the fixed error.
func buildValidUploadRequest(t *testing.T) *http.Request {
	t.Helper()
	servicesJSON := `{"version":2,"project":"my-api","services":[{` +
		`"name":"web","runtime":"node","port":3000,` +
		`"proxy":"/api","dev":"npm run dev","start":"node server.js",` +
		`"healthcheck":"/health","cors":false,"passSetCookie":false}]}`
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("services_json", servicesJSON)
	bundlePart, _ := w.CreateFormFile("bundle", "bundle.tar.gz")
	_, _ = bundlePart.Write([]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return routestest.WithAuth(req, routestest.DefaultUserContext())
}

// EC-3 regression-assurance #1 — ErrInvalidTenantOrProject sentinel path
// STAYS unchanged. The handler must emit INVALID_TENANT_OR_PROJECT 400
// BEFORE consulting the typed PushBundleError mapping.
func TestSourceHandler_Upload_PreservesInvalidTenantOrProject_post_typed_errors(t *testing.T) {
	store := &fakeFixedErrSourceStore{
		err: errors.New("invalid tenant or project ID: tenantID failed DNS-1123"),
	}
	// Wrap the canonical sentinel so errors.Is matches at the handler.
	store.err = wrapErrorIs(source.ErrInvalidTenantOrProject, store.err.Error())
	handler := NewSourceHandler(store)

	req := buildValidUploadRequest(t)
	rec := httptest.NewRecorder()
	handler.Upload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for ErrInvalidTenantOrProject, got %d (body: %s)",
			rec.Code, truncate(rec.Body.String(), 200))
	}
	if !bytesContains(rec.Body.String(), "INVALID_TENANT_OR_PROJECT") {
		t.Errorf("expected INVALID_TENANT_OR_PROJECT code in body, got: %s",
			truncate(rec.Body.String(), 200))
	}
	// Negative-assertion: must NOT have collapsed to SOURCE_UPLOAD_FAILED.
	if bytesContains(rec.Body.String(), "SOURCE_UPLOAD_FAILED") {
		t.Errorf("EC-3 violation: response body has SOURCE_UPLOAD_FAILED, want INVALID_TENANT_OR_PROJECT. body=%s",
			truncate(rec.Body.String(), 200))
	}
}

// EC-3 regression-assurance #2 — gitea-race substring path STAYS as
// DEPLOY_IN_PROGRESS 409 even when the underlying error has been wrapped
// in *PushBundleError{Reason: ReasonRemoteRejectedPush}. The handler must
// consult IsGiteaRaceCondition BEFORE the typed-error ladder.
func TestSourceHandler_Upload_PreservesDeployInProgress_when_gitea_substring_present(t *testing.T) {
	// Construct a typed *PushBundleError whose Error() string contains the
	// gitea-race substring — mirrors what PushBundle does in production
	// when wrapDownstreamPushError sees "cannot lock ref" anywhere in the
	// downstream error chain.
	underlying := errors.New("gitea source: git push: failed: cannot lock ref 'refs/heads/main'")
	typedErr := source.NewPushBundleError(source.ReasonRemoteRejectedPush, underlying,
		"gitea rejected the push (race)")
	store := &fakeFixedErrSourceStore{err: typedErr}
	handler := NewSourceHandler(store)

	req := buildValidUploadRequest(t)
	rec := httptest.NewRecorder()
	handler.Upload(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("EC-3 violation: gitea-race substring inside *PushBundleError should map to 409 DEPLOY_IN_PROGRESS, got status %d (body: %s)",
			rec.Code, truncate(rec.Body.String(), 200))
	}
	if !bytesContains(rec.Body.String(), "DEPLOY_IN_PROGRESS") {
		t.Errorf("expected DEPLOY_IN_PROGRESS code in body, got: %s",
			truncate(rec.Body.String(), 200))
	}
	// Negative-assertion: must NOT have collapsed to SOURCE_UPLOAD_FAILED
	// even though *PushBundleError.Reason = ReasonRemoteRejectedPush would
	// have suggested that mapping if the ladder order were inverted.
	if bytesContains(rec.Body.String(), "SOURCE_UPLOAD_FAILED") {
		t.Errorf("EC-3 violation: response body has SOURCE_UPLOAD_FAILED, want DEPLOY_IN_PROGRESS. The typed ladder mistakenly took precedence over the gitea-race substring check. body=%s",
			truncate(rec.Body.String(), 200))
	}
}

// New typed-error mapping — non-race PushBundleError emits SOURCE_UPLOAD_FAILED
// with a specific actionable Message keyed off the Reason. Crucially, the
// response body MUST NOT contain the legacy misleading "Ensure your git history
// is clean" text — Sprint 4 #31 root cause.
func TestSourceHandler_Upload_TypedReasons_emitSpecificMessage_not_legacy_text(t *testing.T) {
	cases := []struct {
		reason      source.PushBundleErrorReason
		wantSubstr  string
		wantBackend bool // expect "Source backend" / "remote" / "bundle" in Message
	}{
		{source.ReasonSourceBackendUnreachable, "Source backend is unreachable", true},
		{source.ReasonBundleMalformed, "source bundle is malformed", true},
		{source.ReasonBundleTooLarge, "exceeds the size limit", true},
		{source.ReasonArchiveReadError, "Could not read the source archive", true},
		// ReasonRemoteRejectedPush is excluded from this test because its
		// canonical Message ("The remote rejected the push...") would
		// indeed surface here — but only when the underlying error string
		// does NOT match the gitea-race substring set. We cover the
		// gitea-race override in the EC-3 test above.
	}
	for _, tc := range cases {
		t.Run(tc.reason.String(), func(t *testing.T) {
			typedErr := source.NewPushBundleError(tc.reason, errors.New("underlying"), "detail")
			store := &fakeFixedErrSourceStore{err: typedErr}
			handler := NewSourceHandler(store)

			req := buildValidUploadRequest(t)
			rec := httptest.NewRecorder()
			handler.Upload(rec, req)

			if !bytesContains(rec.Body.String(), "SOURCE_UPLOAD_FAILED") {
				t.Errorf("expected SOURCE_UPLOAD_FAILED code for reason %s, body: %s",
					tc.reason.String(), truncate(rec.Body.String(), 200))
			}
			if !bytesContains(rec.Body.String(), tc.wantSubstr) {
				t.Errorf("reason %s: expected Message to contain %q, body: %s",
					tc.reason.String(), tc.wantSubstr, truncate(rec.Body.String(), 200))
			}
			// CRITICAL: legacy misleading text MUST NOT appear.
			if bytesContains(rec.Body.String(), "Ensure your git history is clean") {
				t.Errorf("Sprint 4 #31 regression: legacy misleading 'Ensure your git history is clean' text appears in response for reason %s. body: %s",
					tc.reason.String(), truncate(rec.Body.String(), 200))
			}
		})
	}
}

// wrapErrorIs constructs a sentinel-bearing error so errors.Is at the handler
// finds source.ErrInvalidTenantOrProject in the chain. Uses fmt.Errorf %w.
func wrapErrorIs(sentinel error, prefix string) error {
	// %w preserves the error chain; errors.Is(returned, sentinel) == true.
	return &sentinelWrapped{base: sentinel, msg: prefix}
}

type sentinelWrapped struct {
	base error
	msg  string
}

func (s *sentinelWrapped) Error() string { return s.msg }
func (s *sentinelWrapped) Unwrap() error { return s.base }

// Ensure compile-time use of strings package (defensive — otherwise gofmt may
// strip the unused import if we refactor truncate() out).
var _ = strings.Contains
