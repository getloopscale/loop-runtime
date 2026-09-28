// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestOpenTunnelForService_RefusesAMissingRestConfig(t *testing.T) {
	_, err := OpenTunnelForService(context.Background(), nil, nil, TunnelTarget{Namespace: "ns"})
	if err == nil {
		t.Fatal("a nil rest.Config must be refused before any pod is looked up")
	}
}

// The Hydra admin pod is labelled by its service name on some installs and by
// plain "hydra" on others; Run and Verify each carried their own copy of the
// fallback.
func TestOpenHydraAdminTunnel_FallsBackToThePlainHydraLabel(t *testing.T) {
	var asked []map[string]string
	d := Deps{Ctx: context.Background(), PortForwardCtor: func(_ context.Context, target TunnelTarget) (*Tunnel, error) {
		asked = append(asked, target.PodLabels)
		if reflect.DeepEqual(target.PodLabels, PodMetaLabels("hydra")) {
			return &Tunnel{LocalPort: 1, Close: func() {}}, nil
		}
		return nil, errors.New("no Ready pod")
	}}
	tunnel, err := openHydraAdminTunnel(d)
	if err != nil {
		t.Fatalf("openHydraAdminTunnel: %v", err)
	}
	defer tunnel.Close()
	want := []map[string]string{PodMetaLabels(hydraAdminServiceName), PodMetaLabels("hydra")}
	if !reflect.DeepEqual(asked, want) {
		t.Errorf("labels tried = %v, want %v", asked, want)
	}
}

func TestOpenHydraAdminTunnel_ReportsTheServiceWhenNoLabelMatches(t *testing.T) {
	d := Deps{Ctx: context.Background(), PortForwardCtor: func(context.Context, TunnelTarget) (*Tunnel, error) {
		return nil, errors.New("no Ready pod")
	}}
	if _, err := openHydraAdminTunnel(d); err == nil {
		t.Fatal("want an error when neither label finds a pod")
	}
}
