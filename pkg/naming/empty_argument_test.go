// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package naming

import (
	"strings"
	"testing"
)

// A composer refusing an empty input must say WHICH input was empty. "argument
// must not be empty" makes a caller with three slugs in hand guess, and the
// operator's preview reconciler asserts on the field name.
func TestComposerErrorNamesTheEmptyArgument(t *testing.T) {
	cases := []struct {
		name string
		call func() (string, error)
		want string
	}{
		{"namespace tenant", func() (string, error) { return Namespace("", "api", "prod") }, "namespace: tenant slug must not be empty"},
		{"namespace project", func() (string, error) { return Namespace("acme", "", "prod") }, "namespace: project slug must not be empty"},
		{"namespace env", func() (string, error) { return Namespace("acme", "api", "") }, "namespace: env name must not be empty"},
		{"tenant namespace", func() (string, error) { return TenantNamespace("") }, "tenant namespace: tenant name must not be empty"},
		{"build namespace", func() (string, error) { return BuildNamespace("") }, "build namespace: tenant slug must not be empty"},
		{"repo name team", func() (string, error) { return RepoName("", "api") }, "repo name: team slug must not be empty"},
		{"gitops repo env", func() (string, error) { return GitOpsRepoName("acme", "api", "") }, "gitops repo name: env name must not be empty"},
		{"argo app project", func() (string, error) { return ArgoAppName("acme", "", "prod") }, "argo app name: project slug must not be empty"},
		{"environment CR tenant", func() (string, error) { return EnvironmentCRName("", "api", "prod") }, "environment CR name: tenant name must not be empty"},
		{"cert secret app", func() (string, error) { return AppCertSecretName("api", "") }, "app cert secret name: app name must not be empty"},
		{"repo binding env", func() (string, error) { return GitRepoBindingName("acme", "") }, "git repo binding name: env name must not be empty"},
		{"argo repo secret", func() (string, error) { return ArgoRepoSecretName("") }, "argo repo secret name: tenant name must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.call()
			if err == nil {
				t.Fatalf("want an error for an empty argument, got %q", got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the empty argument; want it to contain %q", err, tc.want)
			}
		})
	}
}
