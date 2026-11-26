// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package minio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/minio/madmin-go/v3"
)

// adminServer answers the MinIO admin API: user-info with the given status
// and error code, a policy attach, and 200 for every other call, counting
// user creations.
func adminServer(t *testing.T, userInfoStatus int, userInfoCode string) (*AdminProvisioner, *atomic.Int32) {
	t.Helper()
	var addUser atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user-info"):
			w.WriteHeader(userInfoStatus)
			if userInfoCode != "" {
				_, _ = w.Write([]byte(`{"Code":"` + userInfoCode + `","Message":"` + userInfoCode + `"}`))
			} else {
				_, _ = w.Write([]byte(`{"status":"enabled"}`))
			}
		case strings.HasSuffix(r.URL.Path, "/add-user"):
			addUser.Add(1)
		case strings.Contains(r.URL.Path, "/policy/attach"):
			// The attach response is encrypted with the caller's secret.
			body, err := madmin.EncryptData("secret12345", []byte(`{}`))
			if err != nil {
				t.Errorf("encrypt attach response: %v", err)
			}
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	p, err := NewAdminProvisioner(AdminConfig{Endpoint: srv.URL, AccessKey: "admin", SecretKey: "secret12345"})
	if err != nil {
		t.Fatalf("NewAdminProvisioner: %v", err)
	}
	return p, &addUser
}

// A failed existence check is not "the user is absent": treating it so would
// add the user again with a fresh secret, rotating credentials under
// in-flight builds.
func TestEnsureTenantCredentials_FailsWhenTheExistenceCheckFails(t *testing.T) {
	p, addUser := adminServer(t, http.StatusForbidden, "AccessDenied")

	_, _, err := p.EnsureTenantCredentials(context.Background(), "acme")

	if err == nil {
		t.Fatal("EnsureTenantCredentials succeeded although the user lookup failed")
	}
	if addUser.Load() != 0 {
		t.Fatalf("add-user called %d time(s) after a failed lookup; it must not rotate credentials", addUser.Load())
	}
}

func TestEnsureTenantCredentials_CreatesTheUserWhenMinIOSaysItDoesNotExist(t *testing.T) {
	p, addUser := adminServer(t, http.StatusNotFound, "XMinioAdminNoSuchUser")

	_, secret, err := p.EnsureTenantCredentials(context.Background(), "acme")

	if err != nil {
		t.Fatalf("EnsureTenantCredentials: %v", err)
	}
	if addUser.Load() != 1 || secret == "" {
		t.Fatalf("add-user calls = %d, secret empty = %v; want the user created", addUser.Load(), secret == "")
	}
}

func TestEnsureTenantCredentials_KeepsAnExistingUser(t *testing.T) {
	p, addUser := adminServer(t, http.StatusOK, "")

	_, secret, err := p.EnsureTenantCredentials(context.Background(), "acme")

	if err != nil {
		t.Fatalf("EnsureTenantCredentials: %v", err)
	}
	if addUser.Load() != 0 || secret != "" {
		t.Fatalf("an existing user was re-added (calls=%d)", addUser.Load())
	}
}

func TestIsNotFoundError_ReadsMinIOErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{madmin.ErrorResponse{Code: "XMinioAdminNoSuchUser"}, true},
		{madmin.ErrorResponse{Code: "XMinioAdminNoSuchPolicy"}, true},
		{madmin.ErrorResponse{Code: "AccessDenied", Message: "user does not exist"}, false},
		{errors.New("resource not found"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isNotFoundError(c.err); got != c.want {
			t.Errorf("isNotFoundError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestAdminEndpoint_TakesTLSFromTheScheme(t *testing.T) {
	cases := []struct {
		in         string
		host       string
		secure     bool
		wantsError bool
	}{
		{"minio.loopruntime-system.svc.cluster.local:9000", "minio.loopruntime-system.svc.cluster.local:9000", false, false},
		{"http://minio:9000", "minio:9000", false, false},
		{"https://minio.example.com", "minio.example.com", true, false},
		{"ftp://minio", "", false, true},
		{"", "", false, true},
	}
	for _, c := range cases {
		host, secure, err := adminEndpoint(c.in)
		if (err != nil) != c.wantsError || host != c.host || secure != c.secure {
			t.Errorf("adminEndpoint(%q) = (%q, %v, %v), want (%q, %v, error=%v)", c.in, host, secure, err, c.host, c.secure, c.wantsError)
		}
	}
}
