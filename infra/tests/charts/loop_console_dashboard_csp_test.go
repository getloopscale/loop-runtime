// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
)

// The dashboard image's Content-Security-Policy adds to 'self' the origins nginx reads
// from /etc/nginx/conf.d/csp-origins.conf (getloopscale/console#525). The chart renders
// that file from config.cspConnectOrigins and config.cspFormOrigins and mounts it, and a
// change to it rolls the pods.
func TestLoopRuntimeDashboardMountsTheCSPOriginsOfItsEnvironment(t *testing.T) {
	chart := uniqueChart(t, "loop-console-dashboard")
	out := renderChart(t, chart, "loop-console-dashboard", map[string]string{
		"config.cspConnectOrigins": "{https://auth.example.com,https://cloud.example.com}",
		"config.cspFormOrigins":    "{https://auth.example.com}",
	})

	config := renderedObject(t, out, "ConfigMap", "loop-console-dashboard-config")
	origins, _ := dig(config, "data", "csp-origins.conf").(string)
	for _, want := range []string{
		`map "" $csp_connect_origins { default "https://auth.example.com https://cloud.example.com"; }`,
		`map "" $csp_form_origins { default "https://auth.example.com"; }`,
	} {
		if !strings.Contains(origins, want) {
			t.Errorf("csp-origins.conf lacks %q:\n%s", want, origins)
		}
	}

	docs := chartrender.SplitYAMLByKind(out, "Deployment")
	if len(docs) != 1 {
		t.Fatalf("loop-console-dashboard rendered %d Deployments, want 1", len(docs))
	}
	var deploy appsv1.Deployment
	if err := yaml.Unmarshal([]byte(docs[0]), &deploy); err != nil {
		t.Fatalf("decode the Deployment: %v", err)
	}
	mounted := false
	for _, m := range deploy.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.MountPath == "/etc/nginx/conf.d/csp-origins.conf" && m.SubPath == "csp-origins.conf" {
			mounted = true
		}
	}
	if !mounted {
		t.Errorf("csp-origins.conf is not mounted at /etc/nginx/conf.d/csp-origins.conf: %+v", deploy.Spec.Template.Spec.Containers[0].VolumeMounts)
	}
	if deploy.Spec.Template.Annotations["checksum/config"] == "" {
		t.Errorf("the pod template carries no checksum/config annotation: %v", deploy.Spec.Template.Annotations)
	}
}

// Without origins the file still defines both variables, empty, as the image's own does.
func TestLoopRuntimeDashboardCSPOriginsDefaultToSameOrigin(t *testing.T) {
	out := renderChart(t, uniqueChart(t, "loop-console-dashboard"), "loop-console-dashboard", nil)
	origins, _ := dig(renderedObject(t, out, "ConfigMap", "loop-console-dashboard-config"), "data", "csp-origins.conf").(string)
	for _, want := range []string{
		`map "" $csp_connect_origins { default ""; }`,
		`map "" $csp_form_origins { default ""; }`,
	} {
		if !strings.Contains(origins, want) {
			t.Errorf("csp-origins.conf lacks %q:\n%s", want, origins)
		}
	}
}

// Staging's dashboard reaches Kratos on the auth host and the control plane on the cloud
// host.
func TestStagingDashboardAllowsItsAuthAndCloudHosts(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "staging", "loop-console-dashboard", stagingOutputs...)
	connect := dig(values, "config", "cspConnectOrigins")
	form := dig(values, "config", "cspFormOrigins")
	wantConnect := []any{"https://auth.staging.getloopscale.com", "https://cloud.staging.getloopscale.com"}
	wantForm := []any{"https://auth.staging.getloopscale.com"}
	if !equalAny(connect, wantConnect) {
		t.Errorf("config.cspConnectOrigins = %v, want %v", connect, wantConnect)
	}
	if !equalAny(form, wantForm) {
		t.Errorf("config.cspFormOrigins = %v, want %v", form, wantForm)
	}
}

func equalAny(got any, want []any) bool {
	list, ok := got.([]any)
	if !ok || len(list) != len(want) {
		return false
	}
	for i := range want {
		if list[i] != want[i] {
			return false
		}
	}
	return true
}
