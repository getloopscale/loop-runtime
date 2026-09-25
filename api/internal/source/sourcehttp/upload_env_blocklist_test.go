// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sourcehttp

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
	"github.com/getloopscale/loopzero/api/internal/source"
)

// countingStore records whether validation let the upload reach PushBundle.
type countingStore struct{ pushes int }

func (c *countingStore) PushBundle(_ context.Context, _, _ string, bundle io.Reader) (source.PushResult, error) {
	_, _ = io.Copy(io.Discard, bundle)
	c.pushes++
	return source.PushResult{SourceID: "sha-1"}, nil
}

func uploadWithReservedEnvKey(t *testing.T, h *SourceHandler) *httptest.ResponseRecorder {
	t.Helper()
	servicesJSON := `{"version":2,"project":"my-api","services":[{` +
		`"name":"web","runtime":"node","port":3000,` +
		`"proxy":"/api","dev":"npm run dev","start":"node server.js",` +
		`"healthcheck":"/health","cors":false,"passSetCookie":false,` +
		`"env":{"LD_PRELOAD":"/x.so"}}]}`
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("services_json", servicesJSON)
	bundlePart, _ := w.CreateFormFile("bundle", "bundle.tar.gz")
	_, _ = bundlePart.Write([]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	h.Upload(rec, routestest.WithAuth(req, routestest.DefaultUserContext()))
	return rec
}

// TestUploadEnforcesTheReservedEnvBlocklistUnlessConfiguredToWarn pins the
// handler's blocklist mode to its construction: enforcing by default whatever
// the process environment says, and warning only when the composition root
// passes EnvBlocklistWarn.
func TestUploadEnforcesTheReservedEnvBlocklistUnlessConfiguredToWarn(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "warn")

	t.Run("default handler refuses", func(t *testing.T) {
		store := &countingStore{}

		rec := uploadWithReservedEnvKey(t, NewSourceHandler(store))

		if rec.Code != http.StatusUnprocessableEntity || store.pushes != 0 {
			t.Fatalf("want 422 and no push, got %d with %d push(es): %s", rec.Code, store.pushes, rec.Body.String())
		}
	})

	t.Run("warn handler accepts", func(t *testing.T) {
		store := &countingStore{}

		rec := uploadWithReservedEnvKey(t, NewSourceHandler(store, WithEnvBlocklistMode(source.EnvBlocklistWarn)))

		if store.pushes != 1 {
			t.Fatalf("warn mode did not reach the push, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
