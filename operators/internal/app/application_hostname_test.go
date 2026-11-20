// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app

import (
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/pkg/naming"
)

const hostnameTestBaseDomain = "dev.getloopscale.com"

func hostnameTestApp(appName, appLabel, hostname string) *loopzerov1.LoopZeroApplication {
	app := &loopzerov1.LoopZeroApplication{
		Spec: loopzerov1.LoopZeroApplicationSpec{
			TenantRef:   "tn-acme",
			Project:     "shop",
			Environment: "production",
			ServiceName: "web",
			AppName:     appName,
			Hostname:    hostname,
		},
	}
	if appLabel != "" {
		app.ObjectMeta = metav1.ObjectMeta{Labels: map[string]string{naming.LabelApp: appLabel}}
	}
	return app
}

// The routed formula the IngressRoute renderer emits for this app.
const routedHost = "tn-acme-shop-web-production.dev.getloopscale.com"

func TestHostnameToAssign_UsesTheRoutedFormulaWhenSpecNamesTheApp(t *testing.T) {
	got, assign, err := hostnameToAssign(hostnameTestApp("web", "", ""), hostnameTestBaseDomain)
	if err != nil || !assign || got != routedHost {
		t.Fatalf("hostnameToAssign = (%q, %v, %v), want (%q, true, nil)", got, assign, err, routedHost)
	}
}

func TestHostnameToAssign_FallsBackToTheAppLabelWhenSpecPredatesTheField(t *testing.T) {
	got, assign, err := hostnameToAssign(hostnameTestApp("", "web", ""), hostnameTestBaseDomain)
	if err != nil || !assign || got != routedHost {
		t.Fatalf("hostnameToAssign = (%q, %v, %v), want (%q, true, nil)", got, assign, err, routedHost)
	}
}

func TestHostnameToAssign_PrefersSpecAppNameOverTheLabel(t *testing.T) {
	got, _, err := hostnameToAssign(hostnameTestApp("web", "stale", ""), hostnameTestBaseDomain)
	if err != nil || got != routedHost {
		t.Fatalf("hostnameToAssign = (%q, %v), want %q", got, err, routedHost)
	}
}

func TestHostnameToAssign_KeepsAnExplicitHostname(t *testing.T) {
	_, assign, err := hostnameToAssign(hostnameTestApp("web", "", "custom.example.com"), hostnameTestBaseDomain)
	if err != nil || assign {
		t.Fatalf("hostnameToAssign assign=%v err=%v, want the explicit hostname kept", assign, err)
	}
}

func TestHostnameToAssign_ReplacesTheLegacyProjectEnvDefault(t *testing.T) {
	// "shop-production.dev.getloopscale.com" is what the controller used to write
	// by default; no IngressRoute ever matched it (the J-25 defect).
	got, assign, err := hostnameToAssign(hostnameTestApp("web", "", "shop-production.dev.getloopscale.com"), hostnameTestBaseDomain)
	if err != nil || !assign || got != routedHost {
		t.Fatalf("hostnameToAssign = (%q, %v, %v), want (%q, true, nil)", got, assign, err, routedHost)
	}
}

func TestHostnameToAssign_KeepsTheLegacyDefaultWhenTheAppIsUnknown(t *testing.T) {
	_, assign, err := hostnameToAssign(hostnameTestApp("", "", "shop-production.dev.getloopscale.com"), hostnameTestBaseDomain)
	if err != nil || assign {
		t.Fatalf("hostnameToAssign assign=%v err=%v, want the stored hostname kept", assign, err)
	}
}

func TestHostnameToAssign_RefusesWhenNoHostnameAndNoAppName(t *testing.T) {
	_, _, err := hostnameToAssign(hostnameTestApp("", "", ""), hostnameTestBaseDomain)
	if !errors.Is(err, errApplicationNameUnknown) {
		t.Fatalf("hostnameToAssign err = %v, want errApplicationNameUnknown", err)
	}
}
