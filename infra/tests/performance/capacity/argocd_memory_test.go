// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package capacity

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// TestCapacityArgoCDMemoryPerApp validates the cell-budget.md assumption:
//
//	"ArgoCD memory vs Application count — ~500 kB/app is from community
//	 discussions, not LoopRuntime profiling."
//
// The test creates N synthetic ArgoCD Application CRs (manual sync, no
// real repo) and measures go_memstats_heap_inuse_bytes before and after.
// Delta / N = measured bytes per Application.
//
// Run:
//
//	# Port-forward ArgoCD metrics first:
//	kubectl --context=$LOOP_RUNTIME_CORE_CONTEXT -n argocd port-forward svc/argocd-metrics 18082:8082 &
//
//	LOOP_RUNTIME_CAPACITY=1 \
//	LOOP_RUNTIME_ARGOCD_METRICS_URL=http://localhost:18082 \
//	LOOP_RUNTIME_CORE_CONTEXT=do-sfo3-loop-runtime-infra-dev-sfo3-mgmt \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityArgoCDMemoryPerApp -v -timeout 15m
func TestCapacityArgoCDMemoryPerApp(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	metricsURL := os.Getenv("LOOP_RUNTIME_ARGOCD_METRICS_URL")
	mgmtContext := os.Getenv("LOOP_RUNTIME_CORE_CONTEXT")
	if metricsURL == "" || mgmtContext == "" {
		t.Fatal("LOOP_RUNTIME_ARGOCD_METRICS_URL and LOOP_RUNTIME_CORE_CONTEXT required")
	}

	controllerReplicas := 3 // ArgoCD controller replicas (T-05 sharding).
	if s := os.Getenv("LOOP_RUNTIME_ARGOCD_REPLICAS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			controllerReplicas = n
		}
	}

	const (
		appCount    = 100
		argoNS      = "argocd"
		benchLabel  = "loop-runtime.dev/capacity-bench"
		settleTime  = 60 * time.Second
		projectedKB = 500 // cell-budget.md projection: ~500 kB/app
	)

	cfg := NewConfig("argocd-memory")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Build dynamic client from mgmt kubeconfig context.
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: mgmtContext},
	).ClientConfig()
	if err != nil {
		t.Fatalf("build kubeconfig for context %s: %v", mgmtContext, err)
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}

	appGVR := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}

	// --- Cleanup function (runs on success AND failure) ---
	defer func() {
		t.Log("cleaning up synthetic ArgoCD Applications...")
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanCancel()

		list, err := dynClient.Resource(appGVR).Namespace(argoNS).List(cleanCtx, metav1.ListOptions{
			LabelSelector: benchLabel + "=true",
		})
		if err != nil {
			t.Logf("cleanup list error: %v", err)
			return
		}
		for _, item := range list.Items {
			_ = dynClient.Resource(appGVR).Namespace(argoNS).Delete(cleanCtx, item.GetName(), metav1.DeleteOptions{})
		}
		t.Logf("cleaned up %d synthetic Applications", len(list.Items))
	}()

	// --- Step 1: Baseline memory reading ---
	t.Log("reading baseline ArgoCD heap memory...")
	beforeHeap, err := ScrapePrometheusMetric(metricsURL, "go_memstats_heap_inuse_bytes")
	if err != nil {
		t.Fatalf("scrape baseline: %v", err)
	}
	t.Logf("  baseline heap: %.1f Mi", beforeHeap/1024/1024)

	// --- Step 2: Create N synthetic Applications ---
	t.Logf("creating %d synthetic ArgoCD Applications...", appCount)
	created := 0
	for i := 0; i < appCount; i++ {
		appName := fmt.Sprintf("cap-bench-app-%03d", i)
		app := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "argoproj.io/v1alpha1",
				"kind":       "Application",
				"metadata": map[string]interface{}{
					"name":      appName,
					"namespace": argoNS,
					"labels": map[string]interface{}{
						benchLabel: "true",
					},
				},
				"spec": map[string]interface{}{
					"project": "default",
					"source": map[string]interface{}{
						"repoURL":        "https://git.example.com/cap-bench/noop.git",
						"path":           ".",
						"targetRevision": "HEAD",
					},
					"destination": map[string]interface{}{
						"server":    "https://kubernetes.default.svc",
						"namespace": fmt.Sprintf("cap-bench-ns-%03d", i),
					},
					// Manual sync — ArgoCD will NOT attempt to clone the repo.
					"syncPolicy": map[string]interface{}{},
				},
			},
		}

		_, err := dynClient.Resource(appGVR).Namespace(argoNS).Create(ctx, app, metav1.CreateOptions{})
		if err != nil {
			t.Logf("  warning: failed to create %s: %v", appName, err)
			continue
		}
		created++
	}
	t.Logf("  created %d / %d Applications", created, appCount)

	if created < appCount/2 {
		t.Fatalf("too few Applications created (%d/%d) — test results unreliable", created, appCount)
	}

	// --- Step 3: Wait for controller to ingest ---
	t.Logf("waiting %v for ArgoCD controller to ingest Applications...", settleTime)
	// intentional-sleep: benchmark warmup / measurement window
	time.Sleep(settleTime)

	// --- Step 4: Post-creation memory reading ---
	afterHeap, err := ScrapePrometheusMetric(metricsURL, "go_memstats_heap_inuse_bytes")
	if err != nil {
		t.Fatalf("scrape post-creation: %v", err)
	}
	t.Logf("  post-creation heap: %.1f Mi", afterHeap/1024/1024)

	// --- Step 5: Calculate results ---
	deltaBytes := int64(afterHeap) - int64(beforeHeap)
	if deltaBytes < 0 {
		deltaBytes = 0 // GC may have reclaimed during settle — not useful
	}

	// IMPORTANT: With ArgoCD controller sharding (T-05), each replica handles
	// ~apps/replicas Applications. The port-forward targets ONE replica, so
	// the measured delta covers only that replica's share. To estimate the
	// true per-app cost, we divide by the number of apps that replica handles
	// (created / controllerReplicas), not the total. This corrects for the
	// sharding factor that would otherwise undercount by ~3x.
	appsPerReplica := int64(created) / int64(controllerReplicas)
	if appsPerReplica < 1 {
		appsPerReplica = int64(created) // single replica or few apps
	}
	perAppBytes := deltaBytes / appsPerReplica
	projectedBytes := int64(projectedKB) * 1024

	result := MemoryResult{
		Subsystem:         "argocd-memory-per-app",
		StartedAt:         time.Now(),
		BeforeHeapBytes:   int64(beforeHeap),
		PeakHeapBytes:     int64(afterHeap),
		AfterHeapBytes:    int64(afterHeap),
		DeltaHeapBytes:    deltaBytes,
		ProjectedBytes:    projectedBytes,
		ActualVsProjected: float64(perAppBytes) / float64(projectedBytes),
		CorrelationData: map[string]interface{}{
			"app_count":           created,
			"controller_replicas": controllerReplicas,
			"apps_per_replica":    appsPerReplica,
			"per_app_bytes":       perAppBytes,
			"per_app_kb":          perAppBytes / 1024,
			"projected_kb":        projectedKB,
		},
	}

	t.Log("=== argocd-memory-per-app ===")
	t.Logf("  apps created:  %d (sharded across %d replicas, ~%d/replica)", created, controllerReplicas, appsPerReplica)
	t.Logf("  heap before:   %.1f Mi", beforeHeap/1024/1024)
	t.Logf("  heap after:    %.1f Mi", afterHeap/1024/1024)
	t.Logf("  delta:         %.1f Mi (%.1f kB/app, corrected for sharding)", float64(deltaBytes)/1024/1024, float64(perAppBytes)/1024)
	t.Logf("  projected:     %d kB/app", projectedKB)
	t.Logf("  actual/proj:   %.2f", result.ActualVsProjected)

	if err := WriteMemoryResult(result, cfg.OutFile); err != nil {
		t.Errorf("write result: %v", err)
	} else {
		t.Logf("  result:        %s", cfg.OutFile)
	}

	// --- Assertions ---
	if perAppBytes > 2*1024*1024 {
		t.Errorf("memory per app (%d kB) exceeds 2 MB — 512 Mi limit at risk with 100+ apps", perAppBytes/1024)
	}
	if perAppBytes < 100*1024 && perAppBytes > 0 {
		t.Logf("NOTE: per-app memory (%d kB) well below 500 kB projection — budget may be conservative", perAppBytes/1024)
	}
	if deltaBytes == 0 {
		t.Logf("WARNING: zero delta — GC reclaimed during settle, or Applications not ingested. Re-run with longer settle time.")
	}
}
