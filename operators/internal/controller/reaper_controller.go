// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
)

const (
	// AnnotationReaperRollbackRequested tracks the release for which a rollback
	// was already requested, preventing cascade rollbacks.
	// MEET-2026-03-30-022: annotation-based single-attempt guard.
	AnnotationReaperRollbackRequested = "loopzero.io/reaper-rollback-requested"

	// DefaultUnhealthyThreshold is the duration an app must be continuously
	// unhealthy before the reaper triggers a rollback.
	DefaultUnhealthyThreshold = 5 * time.Minute

	// ReaperRequeueInterval is the periodic polling interval for the reaper.
	// Uses requeueAfter instead of event-driven to avoid hot loops on flapping apps.
	ReaperRequeueInterval = 60 * time.Second
)

// ReaperController watches LoopZeroApplication CRDs and triggers automatic rollback
// for workloads that have been unhealthy for longer than the configured threshold.
//
// Design decisions (MEET-2026-03-30-022):
//   - Separate controller from ApplicationController (SRP — F4-C1)
//   - Triggers rollback via API HTTP endpoint (not direct Gitea push — F4-C3)
//   - Uses existing ApplicationHealthy condition lastTransitionTime (F4-C4)
//   - Single rollback attempt per unhealthy episode (annotation guard)
//   - Read-only on CRD status (never writes health status)
//   - Lock timeout from API = "mutation in progress, requeue" (not error)
type ReaperController struct {
	client.Client
	Scheme             *runtime.Scheme
	APIBaseURL         string        // API endpoint for rollback (e.g., "http://loopzero-api.platform-system:8080")
	APIToken           string        // Service account token for internal API calls
	UnhealthyThreshold time.Duration // How long before triggering rollback (default 5m)
}

// Reconcile checks if a LoopZeroApplication has been unhealthy for longer than the
// threshold and triggers a rollback via the API if so.
func (r *ReaperController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("controller", "reaper")

	var app loopzerov1.LoopZeroApplication
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip apps being deleted.
	if !app.DeletionTimestamp.IsZero() {
		return ctrl.Result{RequeueAfter: ReaperRequeueInterval}, nil
	}

	threshold := r.UnhealthyThreshold
	if threshold == 0 {
		threshold = DefaultUnhealthyThreshold
	}

	// Find the ApplicationHealthy condition.
	healthyCondition := findCondition(app.Status.Conditions, conditions.TypeApplicationHealthy)
	if healthyCondition == nil {
		// No health condition yet — app is likely still being provisioned.
		return ctrl.Result{RequeueAfter: ReaperRequeueInterval}, nil
	}

	// If healthy, nothing to do.
	if healthyCondition.Status == metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: ReaperRequeueInterval}, nil
	}

	// Check how long the app has been unhealthy.
	unhealthySince := healthyCondition.LastTransitionTime.Time
	unhealthyDuration := time.Since(unhealthySince)

	if unhealthyDuration < threshold {
		// Not unhealthy long enough yet. Requeue to check again.
		remainingWait := threshold - unhealthyDuration
		logger.V(1).Info("app unhealthy but below threshold",
			"unhealthy_duration", unhealthyDuration.Round(time.Second),
			"threshold", threshold,
			"recheck_in", remainingWait.Round(time.Second),
		)
		return ctrl.Result{RequeueAfter: remainingWait + time.Second}, nil
	}

	// App has been unhealthy for longer than threshold.
	// Check if we already requested a rollback for this episode.
	currentAnnotation := app.Annotations[AnnotationReaperRollbackRequested]
	releaseID := app.Labels["loopzero.io/release-id"]
	if currentAnnotation != "" && currentAnnotation == releaseID {
		// Already requested rollback for this release. Don't cascade.
		logger.Info("rollback already requested for this unhealthy episode, skipping",
			"release_id", releaseID,
			"unhealthy_since", unhealthySince,
		)
		return ctrl.Result{RequeueAfter: ReaperRequeueInterval}, nil
	}

	// Trigger rollback via API.
	logger.Info("triggering automatic rollback for prolonged unhealthy workload",
		"tenant", app.Labels[labelTenant],
		"project", app.Labels[labelProject],
		"environment", app.Labels[labelEnvironment],
		"release_id", releaseID,
		"unhealthy_duration", unhealthyDuration.Round(time.Second),
	)

	err := r.requestRollback(ctx, app)
	if err != nil {
		// Lock timeout or API unavailable = "mutation in progress, requeue".
		// Not an error — just means something else is happening.
		logger.Info("rollback request failed, will retry",
			"error", err,
			"requeue_in", "30s",
		)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Mark this release as rollback-requested to prevent cascade.
	if app.Annotations == nil {
		app.Annotations = make(map[string]string)
	}
	app.Annotations[AnnotationReaperRollbackRequested] = releaseID
	if err := r.Update(ctx, &app); err != nil {
		logger.Error(err, "failed to update reaper annotation")
		return ctrl.Result{RequeueAfter: ReaperRequeueInterval}, err
	}

	// Emit K8s event for observability.
	logger.Info("automatic rollback triggered successfully",
		"release_id", releaseID,
	)

	return ctrl.Result{RequeueAfter: ReaperRequeueInterval}, nil
}

// requestRollback calls the API rollback endpoint.
// Returns nil on success, error on failure (lock timeout, network, etc.).
func (r *ReaperController) requestRollback(ctx context.Context, app loopzerov1.LoopZeroApplication) error {
	tenant := app.Labels[labelTenant]
	project := app.Labels[labelProject]
	environment := app.Labels[labelEnvironment]

	if tenant == "" || project == "" || environment == "" {
		return fmt.Errorf("missing required labels on LoopZeroApplication %s/%s", app.Namespace, app.Name)
	}

	body, err := json.Marshal(map[string]string{
		"project_id":  project,
		"environment": environment,
	})
	if err != nil {
		return fmt.Errorf("marshal rollback request: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/rollback", r.APIBaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create rollback request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Triggered-By", "reaper-controller")
	httpReq.Header.Set("X-Tenant-ID", tenant)
	if r.APIToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.APIToken)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("rollback API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		// 409 = lock contention or deploy in progress. Requeue.
		return fmt.Errorf("rollback conflict (lock contention or deploy in progress)")
	}
	if resp.StatusCode == http.StatusNotFound {
		// 404 = no healthy release to rollback to.
		// Emit event but don't retry — there's nothing to rollback to.
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("rollback API returned status %d", resp.StatusCode)
	}

	return nil
}

// findCondition returns the condition with the given type, or nil.
func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

// Label keys used to extract tenant/project/environment from LoopZeroApplication.
const (
	labelTenant      = "loopzero.io/tenant"
	labelProject     = "loopzero.io/project"
	labelEnvironment = "loopzero.io/environment"
)

// SetupWithManager sets up the reaper controller with the Manager.
func (r *ReaperController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.LoopZeroApplication{}).
		Named("reaper").
		Complete(r)
}
