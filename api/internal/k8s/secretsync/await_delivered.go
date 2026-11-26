// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secretsync

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// forceSyncAnnotation is the annotation External Secrets documents for asking
// for a sync now. The name carries no meaning to the operator: it refreshes an
// ExternalSecret whose status.syncedResourceVersion differs from the generation
// and the hash of the labels and annotations it reads (shouldRefresh in
// pkg/controllers/externalsecret, read at v0.14.3), so a value it has not seen
// is what forces the refresh.
const forceSyncAnnotation = "force-sync"

const (
	// defaultDeliveryTimeout bounds the wait of a deploy for its secrets. A
	// forced refresh takes about a second; the rest is room for the operator
	// and the secret store to be slow.
	defaultDeliveryTimeout = 60 * time.Second
	defaultDeliveryPoll    = 500 * time.Millisecond
	// refreshClockTolerance is how far the operator's clock may run behind
	// this process's. status.refreshTime is the operator's clock, in whole
	// seconds.
	refreshClockTolerance = 2 * time.Second
)

// forceSyncToken is an annotation value no earlier write used.
func forceSyncToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// forcedSync is one ExternalSecret a deploy asked the operator to refresh.
type forcedSync struct {
	name  string
	store string
	// syncedBefore is status.syncedResourceVersion when the sync was forced. A
	// reconcile that read the object before the force writes this value again.
	syncedBefore string
	forcedAt     time.Time
	// last is why the ExternalSecret did not count as delivered when it was
	// last read.
	last string
}

// AwaitDelivered asks External Secrets to refresh every ExternalSecret this
// API wrote for the environment and returns once each one reports a successful
// refresh that began after the request. An environment with no ExternalSecret
// returns at once.
//
// The secret values are stored before the deploy that calls this, so a refresh
// that began after the call read them. A refresh counts when the ExternalSecret
// is Ready, its status.syncedResourceVersion is not the one it had before the
// force (the operator read the forced object, or a later one) and its
// status.refreshTime, the start of that reconcile, is not older than the force.
// Ready alone does not count: it is also what the refresh of a minute ago left.
//
// It returns a *deploycontracts.SecretsNotDeliveredError when that does not
// happen within the deadline, and the caller's own error when ctx ends first.
func (m *SecretK8sManager) AwaitDelivered(ctx context.Context, tenantID, projectID, environment string) error {
	cellID, err := m.tenantCells.GetTenantCellID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("await secrets: read cell of tenant %s: %w", tenantID, err)
	}
	if cellID != m.runtimeCellID {
		// requireServedCell: this instance wrote no ExternalSecret for the
		// tenant and has no client for its cluster.
		return nil
	}
	namespace, err := tenantNamespaceFor(tenantID, projectID, environment)
	if err != nil {
		return err
	}
	client := m.runtimeDynamic.Resource(externalSecretGVR).Namespace(namespace)
	list, err := client.List(ctx, metav1.ListOptions{LabelSelector: managedBySelector})
	if err != nil {
		return fmt.Errorf("await secrets: list external secrets in %s: %w", namespace, err)
	}
	if len(list.Items) == 0 {
		return nil
	}

	timeout, poll := m.deliveryTimeout, m.deliveryPoll
	if timeout <= 0 {
		timeout = defaultDeliveryTimeout
	}
	if poll <= 0 {
		poll = defaultDeliveryPoll
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pending := make(map[string]*forcedSync, len(list.Items))
	for i := range list.Items {
		forced, err := forceSync(waitCtx, client, &list.Items[i])
		if err != nil {
			return fmt.Errorf("await secrets: force a sync of %s/%s: %w", namespace, list.Items[i].GetName(), err)
		}
		if forced != nil {
			pending[forced.name] = forced
		}
	}

	for {
		for name, forced := range pending {
			current, err := client.Get(waitCtx, name, metav1.GetOptions{})
			switch {
			case k8serrors.IsNotFound(err):
				// Deleted since the force: its last secret was removed.
				delete(pending, name)
			case err != nil:
				forced.last = "could not be read: " + err.Error()
			default:
				if forced.last = undeliveredReason(current, forced); forced.last == "" {
					delete(pending, name)
				}
			}
		}
		if len(pending) == 0 {
			return nil
		}
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return fmt.Errorf("await secrets in %s: %w", namespace, ctx.Err())
			}
			return notDeliveredError(namespace, timeout, pending)
		case <-time.After(poll):
		}
	}
}

// forceSync writes a new force-sync value on es and returns what a refresh
// after it has to differ from. It returns nil when es was deleted meanwhile.
func forceSync(ctx context.Context, client dynamic.ResourceInterface, es *unstructured.Unstructured) (*forcedSync, error) {
	// The write races the operator's own writes and other requests on the same
	// object: a conflict is answered by one fresh read, never by a loop.
	for attempt := 0; ; attempt++ {
		forced := &forcedSync{name: es.GetName(), forcedAt: time.Now()}
		forced.store, _, _ = unstructured.NestedString(es.Object, "spec", "secretStoreRef", "name")
		forced.syncedBefore, _, _ = unstructured.NestedString(es.Object, "status", "syncedResourceVersion")
		forced.last = "no refresh was reported after the deploy asked for one"

		annotations := es.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[forceSyncAnnotation] = forceSyncToken()
		es.SetAnnotations(annotations)

		_, err := client.Update(ctx, es, metav1.UpdateOptions{})
		if err == nil {
			return forced, nil
		}
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		if !k8serrors.IsConflict(err) || attempt > 0 {
			return nil, err
		}
		fresh, getErr := client.Get(ctx, es.GetName(), metav1.GetOptions{})
		if k8serrors.IsNotFound(getErr) {
			return nil, nil
		}
		if getErr != nil {
			return nil, getErr
		}
		es = fresh
	}
}

// undeliveredReason says why es does not yet show a successful refresh that
// began after forced, or "" when it does.
func undeliveredReason(es *unstructured.Unstructured, forced *forcedSync) string {
	status, reason, message := readyCondition(es)
	if status != "True" {
		if message == "" && reason == "" {
			return "it reports no Ready condition"
		}
		return fmt.Sprintf("it is not Ready (%s): %s", reason, message)
	}
	synced, _, _ := unstructured.NestedString(es.Object, "status", "syncedResourceVersion")
	if synced == "" || synced == forced.syncedBefore {
		return "no refresh was reported after the deploy asked for one"
	}
	raw, _, _ := unstructured.NestedString(es.Object, "status", "refreshTime")
	refreshed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return fmt.Sprintf("its status.refreshTime %q is not a time", raw)
	}
	if refreshed.Before(forced.forcedAt.Truncate(time.Second).Add(-refreshClockTolerance)) {
		return fmt.Sprintf("its last refresh began before the deploy asked for one (%s, asked at %s)",
			refreshed.UTC().Format(time.RFC3339), forced.forcedAt.UTC().Format(time.RFC3339))
	}
	return ""
}

// readyCondition reads the Ready condition of es.
func readyCondition(es *unstructured.Unstructured) (status, reason, message string) {
	conditions, _, _ := unstructured.NestedSlice(es.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if kind, _, _ := unstructured.NestedString(condition, "type"); kind != "Ready" {
			continue
		}
		status, _, _ = unstructured.NestedString(condition, "status")
		reason, _, _ = unstructured.NestedString(condition, "reason")
		message, _, _ = unstructured.NestedString(condition, "message")
		return status, reason, message
	}
	return "", "", ""
}

// notDeliveredError names the first ExternalSecret, by name, that did not sync.
func notDeliveredError(namespace string, waited time.Duration, pending map[string]*forcedSync) error {
	names := make([]string, 0, len(pending))
	for name := range pending {
		names = append(names, name)
	}
	sort.Strings(names)
	first := pending[names[0]]
	reason := first.last
	if len(names) > 1 {
		reason = fmt.Sprintf("%s (%d ExternalSecrets of the environment did not sync)", reason, len(names))
	}
	return &deploycontracts.SecretsNotDeliveredError{
		Store: first.store, Namespace: namespace, ExternalSecret: first.name,
		Reason: reason, Waited: waited,
	}
}
