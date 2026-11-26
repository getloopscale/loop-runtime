// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runtimeLayer runs `baseline_capture.py runtime` over the given pods JSON.
func runtimeLayer(t *testing.T, pods string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"pods.json": pods, "helm.json": "[]", "apps.json": `{"items":[]}`, "tens.json": `{"items":[]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("python3", "../../scripts/local/baseline_capture.py", "runtime")
	cmd.Env = append(os.Environ(), "TMP_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("baseline_capture.py runtime: %v\n%s", err, out)
	}
	return string(out)
}

func pod(ns, name, ownerKind, owner, phase string) string {
	refs := "[]"
	if owner != "" {
		refs = `[{"kind":"` + ownerKind + `","name":"` + owner + `"}]`
	}
	return `{"metadata":{"namespace":"` + ns + `","name":"` + name + `","ownerReferences":` + refs + `},"status":{"phase":"` + phase + `"}}`
}

// The cleanroom's Phase 5 compares the runtime before the rebuild with the runtime
// after it. Pods were identified by name, and a rebuild names every Deployment and
// DaemonSet pod afresh, so the diff reported every pod missing and every new one
// added — Phase 5 could never pass (#417). The layer identifies a pod by the workload
// that owns it.
func TestTheRuntimeLayerIsTheSameAcrossARebuild(t *testing.T) {
	before := `{"items":[` + strings.Join([]string{
		pod("loopruntime-system", "loopruntime-api-5f76bcc7c7-7jzrm", "ReplicaSet", "loopruntime-api-5f76bcc7c7", "Running"),
		pod("loopruntime-system", "loopruntime-api-5f76bcc7c7-chx8w", "ReplicaSet", "loopruntime-api-5f76bcc7c7", "Running"),
		pod("kube-system", "svclb-traefik-f821ae41-768x2", "DaemonSet", "svclb-traefik-f821ae41", "Running"),
		pod("loopruntime-system", "postgresql-0", "StatefulSet", "postgresql", "Running"),
	}, ",") + `]}`
	after := `{"items":[` + strings.Join([]string{
		pod("loopruntime-system", "loopruntime-api-74f97b6844-fmk2g", "ReplicaSet", "loopruntime-api-74f97b6844", "Running"),
		pod("loopruntime-system", "loopruntime-api-74f97b6844-wkjfp", "ReplicaSet", "loopruntime-api-74f97b6844", "Running"),
		pod("kube-system", "svclb-traefik-f821ae41-p74qg", "DaemonSet", "svclb-traefik-f821ae41", "Running"),
		pod("loopruntime-system", "postgresql-0", "StatefulSet", "postgresql", "Running"),
	}, ",") + `]}`

	if a, b := runtimeLayer(t, before), runtimeLayer(t, after); a != b {
		t.Errorf("the runtime layer differs across a rebuild that changed only pod names:\nbefore %s\nafter  %s", a, b)
	}
}

// A workload that lost a running pod is still a difference.
func TestTheRuntimeLayerSeesAWorkloadLoseAPod(t *testing.T) {
	two := `{"items":[` + pod("loopruntime-system", "loopruntime-api-a-1", "ReplicaSet", "loopruntime-api-a", "Running") + `,` +
		pod("loopruntime-system", "loopruntime-api-a-2", "ReplicaSet", "loopruntime-api-a", "Running") + `]}`
	one := `{"items":[` + pod("loopruntime-system", "loopruntime-api-b-1", "ReplicaSet", "loopruntime-api-b", "Running") + `,` +
		pod("loopruntime-system", "loopruntime-api-b-2", "ReplicaSet", "loopruntime-api-b", "CrashLoopBackOff") + `]}`

	if runtimeLayer(t, two) == runtimeLayer(t, one) {
		t.Error("a Deployment with one pod no longer Running reads the same as one with both Running")
	}
}

func svclbPod(name, owner string) string {
	return `{"metadata":{"namespace":"kube-system","name":"` + name + `","labels":{` +
		`"svccontroller.k3s.cattle.io/svcname":"traefik","svccontroller.k3s.cattle.io/svcnamespace":"traefik"},` +
		`"ownerReferences":[{"kind":"DaemonSet","name":"` + owner + `"}]},"status":{"phase":"Running"}}`
}

// k3s names its ServiceLB DaemonSet svclb-<service>-<prefix of the Service UID>, so a
// rebuild renames the DaemonSet itself: measured fa7e764e before, 778cbaf6 after, and
// Phase 5 reported the one workload missing (#417). The pod's service labels are the
// stable identity.
func TestTheRuntimeLayerIdentifiesServiceLBPodsByTheirService(t *testing.T) {
	before := `{"items":[` + svclbPod("svclb-traefik-fa7e764e-768x2", "svclb-traefik-fa7e764e") + `]}`
	after := `{"items":[` + svclbPod("svclb-traefik-778cbaf6-p74qg", "svclb-traefik-778cbaf6") + `]}`

	if a, b := runtimeLayer(t, before), runtimeLayer(t, after); a != b {
		t.Errorf("a rebuilt ServiceLB DaemonSet reads as a different workload:\nbefore %s\nafter  %s", a, b)
	}
}
