// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testUserToken = "ghu_test_user_token"

// fakeGitHub answers the two calls of a user authorization: the code exchange
// on the web host and /user/installations on the API host. One server plays
// both hosts.
type fakeGitHub struct {
	mu sync.Mutex

	// exchangeStatus and exchangeBody answer POST /login/oauth/access_token.
	exchangeStatus int
	exchangeBody   string
	// installations is what the user can access, served perPage at a time.
	installations []map[string]any
	perPage       int
	// installationsStatus overrides the status of /user/installations.
	installationsStatus int

	exchanges     []map[string]string
	listAuth      []string
	listedPages   []string
	exchangeCalls int
}

func (f *fakeGitHub) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.exchangeCalls++
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		f.exchanges = append(f.exchanges, map[string]string{
			"client_id": r.PostForm.Get("client_id"), "client_secret": r.PostForm.Get("client_secret"), "code": r.PostForm.Get("code"),
		})
		if r.URL.RawQuery != "" {
			t.Errorf("the exchange sent a query string %q; the secret and the code belong in the body", r.URL.RawQuery)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("exchange Accept = %q, want application/json", got)
		}
		status := f.exchangeStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(f.exchangeBody))
	})
	mux.HandleFunc("GET /user/installations", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.listAuth = append(f.listAuth, r.Header.Get("Authorization"))
		f.listedPages = append(f.listedPages, r.URL.Query().Get("page"))
		if f.installationsStatus != 0 {
			w.WriteHeader(f.installationsStatus)
			return
		}
		page := 1
		_, _ = fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		perPage := f.perPage
		if perPage == 0 {
			perPage = 100
		}
		start := min((page-1)*perPage, len(f.installations))
		end := min(start+perPage, len(f.installations))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":   len(f.installations),
			"installations": f.installations[start:end],
		})
	})
	return mux
}

func installationOf(id, appID int64, login, accountType string) map[string]any {
	return map[string]any{"id": id, "app_id": appID, "account": map[string]any{"login": login, "type": accountType}}
}

func newUserAccess(t *testing.T, gh *fakeGitHub) *UserAccessClient {
	t.Helper()
	srv := httptest.NewServer(gh.handler(t))
	t.Cleanup(srv.Close)
	client, err := NewUserAccessClient(UserAccessConfig{
		AppID: 77, ClientID: "Iv1.client", ClientSecret: "s3cret", WebBaseURL: srv.URL, APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewUserAccessClient: %v", err)
	}
	return client
}

func TestUserAccess_ReturnsTheInstallationsOfThisAppTheUserCanAccess(t *testing.T) {
	gh := &fakeGitHub{
		exchangeBody: `{"access_token":"` + testUserToken + `","token_type":"bearer"}`,
		installations: []map[string]any{
			installationOf(101, 77, "acme", "Organization"),
			installationOf(202, 99, "acme", "Organization"), // another App's installation
			installationOf(303, 77, "alice", "User"),
		},
	}
	client := newUserAccess(t, gh)

	got, err := client.InstallationsForCode(context.Background(), "code-1")

	if err != nil {
		t.Fatalf("InstallationsForCode: %v", err)
	}
	want := []UserInstallation{
		{ID: 101, AccountLogin: "acme", AccountType: "Organization"},
		{ID: 303, AccountLogin: "alice", AccountType: "User"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("installations = %+v, want %+v", got, want)
	}
	if len(gh.exchanges) != 1 || gh.exchanges[0]["code"] != "code-1" ||
		gh.exchanges[0]["client_id"] != "Iv1.client" || gh.exchanges[0]["client_secret"] != "s3cret" {
		t.Errorf("the exchange did not carry the client credentials and the code: %d call(s)", len(gh.exchanges))
	}
	if len(gh.listAuth) != 1 || gh.listAuth[0] != "Bearer "+testUserToken {
		t.Errorf("/user/installations was not called once with the user's token")
	}
}

func TestUserAccess_ReadsEveryPageOfInstallations(t *testing.T) {
	gh := &fakeGitHub{exchangeBody: `{"access_token":"` + testUserToken + `"}`, perPage: 2}
	for id := int64(1); id <= 5; id++ {
		gh.installations = append(gh.installations, installationOf(id, 77, "acme", "Organization"))
	}
	client := newUserAccess(t, gh)
	client.pageSize = 2

	got, err := client.InstallationsForCode(context.Background(), "code-1")

	if err != nil {
		t.Fatalf("InstallationsForCode: %v", err)
	}
	if len(got) != 5 || got[4].ID != 5 {
		t.Errorf("got %d installations (%+v), want all 5", len(got), got)
	}
	if strings.Join(gh.listedPages, ",") != "1,2,3" {
		t.Errorf("pages read = %v, want 1,2,3", gh.listedPages)
	}
}

func TestUserAccess_ARefusedCodeIsReportedAsRejected(t *testing.T) {
	for name, body := range map[string]string{
		"bad or reused code": `{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired."}`,
	} {
		t.Run(name, func(t *testing.T) {
			gh := &fakeGitHub{exchangeBody: body, installations: []map[string]any{installationOf(101, 77, "acme", "Organization")}}
			client := newUserAccess(t, gh)

			got, err := client.InstallationsForCode(context.Background(), "used-code")

			if !errors.Is(err, ErrOAuthCodeRejected) {
				t.Fatalf("err = %v, want ErrOAuthCodeRejected", err)
			}
			if len(got) != 0 || len(gh.listAuth) != 0 {
				t.Errorf("installations were read (%d, %d list calls) after a refused code", len(got), len(gh.listAuth))
			}
		})
	}
}

func TestUserAccess_AFailureThatIsNotTheCodesIsNotReportedAsRejected(t *testing.T) {
	cases := map[string]*fakeGitHub{
		"exchange answers 503":             {exchangeStatus: http.StatusServiceUnavailable, exchangeBody: `{}`},
		"exchange refuses our credentials": {exchangeBody: `{"error":"incorrect_client_credentials"}`},
		"exchange answers no token":        {exchangeBody: `{}`},
		"installations answer 502":         {exchangeBody: `{"access_token":"` + testUserToken + `"}`, installationsStatus: http.StatusBadGateway},
		"installations refuse the token":   {exchangeBody: `{"access_token":"` + testUserToken + `"}`, installationsStatus: http.StatusUnauthorized},
	}
	for name, gh := range cases {
		t.Run(name, func(t *testing.T) {
			client := newUserAccess(t, gh)

			got, err := client.InstallationsForCode(context.Background(), "code-1")

			if err == nil || errors.Is(err, ErrOAuthCodeRejected) {
				t.Fatalf("err = %v, want a failure that is not ErrOAuthCodeRejected", err)
			}
			if len(got) != 0 {
				t.Errorf("returned %d installations with an error", len(got))
			}
			for _, secret := range []string{"s3cret", testUserToken} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("the error carries a credential: %v", err)
				}
			}
		})
	}
}

func TestUserAccess_AnUnreachableGitHubIsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	client, err := NewUserAccessClient(UserAccessConfig{AppID: 77, ClientID: "Iv1.client", ClientSecret: "s3cret", WebBaseURL: srv.URL, APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewUserAccessClient: %v", err)
	}

	_, err = client.InstallationsForCode(context.Background(), "code-1")

	if err == nil || errors.Is(err, ErrOAuthCodeRejected) {
		t.Fatalf("err = %v, want a failure that is not ErrOAuthCodeRejected", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("the error carries the client secret: %v", err)
	}
}

func TestNewUserAccessClient_RefusesAnIncompleteConfiguration(t *testing.T) {
	for name, cfg := range map[string]UserAccessConfig{
		"no app id":        {ClientID: "c", ClientSecret: "s"},
		"no client id":     {AppID: 77, ClientSecret: "s"},
		"no client secret": {AppID: 77, ClientID: "c"},
	} {
		t.Run(name, func(t *testing.T) {
			if client, err := NewUserAccessClient(cfg); err == nil || client != nil {
				t.Fatalf("NewUserAccessClient = %v, %v; want an error", client, err)
			}
		})
	}
}
