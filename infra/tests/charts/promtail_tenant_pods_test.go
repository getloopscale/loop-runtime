// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"regexp"
	"strings"
	"testing"
)

// relabel applies a job's relabel_configs to the labels service discovery gives one
// target, the way Prometheus and Promtail do for the actions the job uses (replace,
// keep, drop): the source values joined by the separator are matched against the
// anchored regex. It returns the target's labels, and false when the target is dropped.
func relabel(t *testing.T, configs []any, discovered map[string]string) (map[string]string, bool) {
	t.Helper()
	labels := map[string]string{}
	for k, v := range discovered {
		labels[k] = v
	}
	text := func(rule map[string]any, key, fallback string) string {
		if v, ok := rule[key].(string); ok {
			return v
		}
		return fallback
	}
	for _, c := range configs {
		rule, _ := c.(map[string]any)
		var values []string
		sources, _ := rule["source_labels"].([]any)
		for _, s := range sources {
			values = append(values, labels[s.(string)])
		}
		value := strings.Join(values, text(rule, "separator", ";"))
		re := regexp.MustCompile("^(?:" + text(rule, "regex", "(.*)") + ")$")
		switch action := text(rule, "action", "replace"); action {
		case "keep":
			if !re.MatchString(value) {
				return nil, false
			}
		case "drop":
			if re.MatchString(value) {
				return nil, false
			}
		case "replace":
			match := re.FindStringSubmatchIndex(value)
			if match == nil {
				continue
			}
			target := text(rule, "target_label", "")
			if replaced := string(re.ExpandString(nil, text(rule, "replacement", "$1"), value, match)); replaced == "" {
				delete(labels, target)
			} else {
				labels[target] = replaced
			}
		default:
			t.Fatalf("relabel action %q is not one this test evaluates", action)
		}
	}
	return labels, true
}

func loopRuntimePodsRelabels(t *testing.T, helmfile, env string, extraEnv []string) []any {
	t.Helper()
	job := promtailJobs(t, helmfile, env, extraEnv)["loop-runtime-pods"]
	if job == nil {
		t.Fatal("no loop-runtime-pods job")
	}
	configs, _ := job["relabel_configs"].([]any)
	return configs
}

// What pod service discovery gives for a pod of a tenant app: the pod's own labels and
// its namespace's name. It gives no label of the namespace, which is what the job used to
// decide on, so every tenant pod was dropped and Runtime logs stayed empty
// (getloopscale/loop-runtime#110).
var tenantPodDiscovered = map[string]string{
	"__meta_kubernetes_namespace":                                      "loop-runtime-tn-acme-shop-production",
	"__meta_kubernetes_pod_uid":                                        "0e1d",
	"__meta_kubernetes_pod_container_name":                             "web",
	"__meta_kubernetes_pod_label_app":                                  "shop-web",
	"__meta_kubernetes_pod_label_runtime_getloopscale_com_tenant":      "tn-acme",
	"__meta_kubernetes_pod_label_runtime_getloopscale_com_project":     "shop",
	"__meta_kubernetes_pod_label_runtime_getloopscale_com_app_name":    "web",
	"__meta_kubernetes_pod_label_runtime_getloopscale_com_environment": "production",
}

func TestPromtailKeepsATenantPodByItsOwnLabels(t *testing.T) {
	cases := []struct {
		name, helmfile, env string
		extraEnv            []string
	}{
		{"staging cell", "cell.helmfile.yaml.gotmpl", "staging", stagingMonitoringOutputs()},
		{"dev build", "build.helmfile.yaml.gotmpl", "dev", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			labels, kept := relabel(t, loopRuntimePodsRelabels(t, c.helmfile, c.env, c.extraEnv), tenantPodDiscovered)
			if !kept {
				t.Fatal("the loop-runtime-pods job drops a pod labelled runtime.getloopscale.com/tenant")
			}
			// namespace, app and container are what loop-runtime-api selects a project's runtime
			// logs by (api/internal/logs/loki/client.go, buildRuntimeLogQL); app is the
			// app's name, as the Runtime logs filter sends it.
			for label, want := range map[string]string{
				"namespace":                "loop-runtime-tn-acme-shop-production",
				"container":                "web",
				"app":                      "web",
				"loop_runtime_tenant":      "tn-acme",
				"loop_runtime_project":     "shop",
				"loop_runtime_environment": "production",
			} {
				if got := labels[label]; got != want {
					t.Errorf("%s = %q, want %q", label, got, want)
				}
			}
			if got := labels["__path__"]; !strings.HasPrefix(got, "/var/log/pods/*0e1d/web/") {
				t.Errorf("__path__ = %q, want the pod's log directory", got)
			}
		})
	}
}

func TestPromtailKeepsAPlatformComponentPod(t *testing.T) {
	labels, kept := relabel(t, loopRuntimePodsRelabels(t, "cell.helmfile.yaml.gotmpl", "staging", stagingMonitoringOutputs()), map[string]string{
		"__meta_kubernetes_namespace":                                    "loop-runtime-system",
		"__meta_kubernetes_pod_uid":                                      "77aa",
		"__meta_kubernetes_pod_container_name":                           "manager",
		"__meta_kubernetes_pod_label_runtime_getloopscale_com_component": "app-operator",
		"__meta_kubernetes_pod_label_runtime_getloopscale_com_app":       "app-operator",
	})
	if !kept {
		t.Fatal("the loop-runtime-pods job drops a pod labelled runtime.getloopscale.com/component")
	}
	for _, label := range []string{"loop_runtime_component", "app"} {
		if got := labels[label]; got != "app-operator" {
			t.Errorf("%s = %q, want app-operator", label, got)
		}
	}
}

// The other jobs name a pod's app from app.kubernetes.io/name. Their relabel had a regex
// without a capture group, which replaces with nothing: Loki held no `app` label at all
// on staging on 2026-11-25.
func TestPromtailPlatformPodsCarryTheirAppName(t *testing.T) {
	job := promtailJobs(t, "cell.helmfile.yaml.gotmpl", "staging", stagingMonitoringOutputs())["platform-pods"]
	configs, _ := job["relabel_configs"].([]any)
	labels, kept := relabel(t, configs, map[string]string{
		"__meta_kubernetes_namespace":                        "traefik",
		"__meta_kubernetes_pod_uid":                          "4b4b",
		"__meta_kubernetes_pod_container_name":               "traefik",
		"__meta_kubernetes_pod_label_app_kubernetes_io_name": "traefik",
	})
	if !kept {
		t.Fatal("the platform-pods job drops a traefik pod")
	}
	if got := labels["app"]; got != "traefik" {
		t.Errorf("app = %q, want traefik", got)
	}
}

func TestPromtailLoopruntimePodsJobDropsAPodThatIsNeitherTenantNorComponent(t *testing.T) {
	_, kept := relabel(t, loopRuntimePodsRelabels(t, "cell.helmfile.yaml.gotmpl", "staging", stagingMonitoringOutputs()), map[string]string{
		"__meta_kubernetes_namespace":          "kube-system",
		"__meta_kubernetes_pod_uid":            "9c9c",
		"__meta_kubernetes_pod_container_name": "coredns",
		"__meta_kubernetes_pod_label_k8s_app":  "kube-dns",
	})
	if kept {
		t.Error("the loop-runtime-pods job keeps a pod with neither runtime.getloopscale.com/tenant nor runtime.getloopscale.com/component")
	}
}
