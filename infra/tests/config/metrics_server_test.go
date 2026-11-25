// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// EKS ships no metrics server, and the cell helmfile installs one only when the
// environment asks for it. An HPA reads CPU and memory through that API: without
// it every autoscaled tenant app stays at its minimum with `cpu: <unknown>`.
func TestEKSEnvironmentsInstallTheMetricsServer(t *testing.T) {
	for _, env := range []string{"staging"} {
		data, err := os.ReadFile("../../helmfile/environments/" + env + ".yaml")
		if err != nil {
			t.Fatalf("read %s values: %v", env, err)
		}
		var values struct {
			InstallMetricsServer bool `yaml:"installMetricsServer"`
			Profile              struct {
				AWSSecretsManager bool `yaml:"awsSecretsManager"`
			} `yaml:"profile"`
		}
		if err := yaml.Unmarshal(data, &values); err != nil {
			t.Fatalf("parse %s values: %v", env, err)
		}
		if !values.Profile.AWSSecretsManager {
			t.Fatalf("%s no longer runs the AWS layout; this test lists the wrong environments", env)
		}
		if !values.InstallMetricsServer {
			t.Errorf("%s runs on EKS and does not set installMetricsServer: true, so no HPA can scale", env)
		}
	}
}
