// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integrationsadapter

import (
	"context"
	"errors"
	"fmt"
	"testing"

	gh "github.com/getloopscale/loopzero/api/internal/github"
	"github.com/getloopscale/loopzero/api/internal/integrations/integrationshttp"
)

type fakeUserInstallations struct {
	found []gh.UserInstallation
	err   error
}

func (f fakeUserInstallations) InstallationsForCode(context.Context, string) ([]gh.UserInstallation, error) {
	return f.found, f.err
}

func TestUserInstallationSourceAdapter_MapsTheInstallationsGitHubReturned(t *testing.T) {
	adapter := &UserInstallationSourceAdapter{Client: fakeUserInstallations{found: []gh.UserInstallation{
		{ID: 101, AccountLogin: "acme", AccountType: "Organization"},
	}}}

	got, err := adapter.InstallationsForCode(context.Background(), "code-1")

	want := integrationshttp.Installation{ID: 101, AccountLogin: "acme", AccountType: "Organization"}
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("InstallationsForCode = %+v, %v; want [%+v]", got, err, want)
	}
}

// The handler answers 400 for a refused code and 502 for anything else, so
// the two must stay distinguishable across the adapter.
func TestUserInstallationSourceAdapter_KeepsARefusedCodeApartFromAnOutage(t *testing.T) {
	refused := &UserInstallationSourceAdapter{Client: fakeUserInstallations{err: fmt.Errorf("exchange: %w", gh.ErrOAuthCodeRejected)}}
	if _, err := refused.InstallationsForCode(context.Background(), "c"); !errors.Is(err, integrationshttp.ErrOAuthCodeRejected) {
		t.Errorf("refused code: err = %v, want integrationshttp.ErrOAuthCodeRejected", err)
	}

	outage := &UserInstallationSourceAdapter{Client: fakeUserInstallations{err: errors.New("status 503")}}
	if _, err := outage.InstallationsForCode(context.Background(), "c"); err == nil || errors.Is(err, integrationshttp.ErrOAuthCodeRejected) {
		t.Errorf("outage: err = %v, want a failure that is not ErrOAuthCodeRejected", err)
	}
}
