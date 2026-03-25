// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// reconcileArgoCDPhase handles the ConfiguringArgoCD phase.
// Steps: check reachability, validate/create JWT, register repo credentials.
func (r *PlatformBootstrapReconciler) reconcileArgoCDPhase(ctx context.Context, pb *loopzerov1.PlatformBootstrap, maxRetries int) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	argoURL := pb.Spec.ArgoCD.InternalURL
	argoNS := pb.Spec.ArgoCD.Namespace
	if argoNS == "" {
		argoNS = "argocd"
	}

	// Step 1: Check ArgoCD reachable.
	if err := r.checkArgoCDReachable(ctx, argoURL); err != nil {
		logger.Info("argocd unreachable", "url", argoURL, "error", err)
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("ArgoCD unreachable: %v", err))
	}

	// Step 2: Check if token already exists in platform-tokens Secret.
	platformSecret, err := r.getSecret(ctx, "loopzero-platform-tokens", pb.Namespace)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("platform tokens secret not found: %v", err))
	}

	existingToken := string(platformSecret.Data["ARGOCD_TOKEN"])
	if existingToken != "" {
		logger.Info("ArgoCD token already exists in platform-tokens, skipping JWT creation")
	} else {
		// Step 3: Read ArgoCD admin password from its initial admin secret.
		adminSecretName := pb.Spec.ArgoCD.AdminSecretName
		if adminSecretName == "" {
			adminSecretName = "argocd-initial-admin-secret"
		}

		var adminSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{Name: adminSecretName, Namespace: argoNS}, &adminSecret); err != nil {
			if errors.IsNotFound(err) {
				return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("ArgoCD admin secret %s/%s not found", argoNS, adminSecretName))
			}
			return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to read ArgoCD admin secret: %v", err))
		}

		adminPassword := string(adminSecret.Data["password"])
		if adminPassword == "" {
			return r.requeueWithRetry(ctx, pb, maxRetries, "ArgoCD admin secret has empty password")
		}

		// Step 4: Create JWT via ArgoCD session API.
		jwt, err := r.createArgoCDSession(ctx, argoURL, adminPassword)
		if err != nil {
			return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to create ArgoCD session: %v", err))
		}

		// Step 5: Store JWT in platform-tokens Secret.
		if err := r.updateSecretData(ctx, platformSecret, "ARGOCD_TOKEN", jwt); err != nil {
			return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to store ArgoCD token: %v", err))
		}
	}

	// Step 6: Create repo credential Secrets in ArgoCD namespace.
	giteaToken := string(platformSecret.Data["GITEA_TOKEN"])
	for _, repoCred := range pb.Spec.ArgoCD.RepoCredentials {
		if err := r.ensureRepoCrendentialSecret(ctx, argoNS, repoCred, giteaToken); err != nil {
			return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to create repo credential %s: %v", repoCred.Name, err))
		}
	}

	r.setCondition(pb, conditions.TypeArgoCDReady, metav1.ConditionTrue, conditions.ReasonReady, "ArgoCD configured")
	return r.transitionToNextPhase(ctx, pb, statemachine.PlatformBootstrapArgoCDReady, "ArgoCD configuration complete")
}

// checkArgoCDReachable checks if ArgoCD is reachable by calling GET /api/version.
func (r *PlatformBootstrapReconciler) checkArgoCDReachable(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/version", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	// ArgoCD uses self-signed certs in cluster; the HTTPClient should be configured
	// with InsecureSkipVerify for internal calls.
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("connecting to ArgoCD: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from ArgoCD version endpoint", resp.StatusCode)
	}
	return nil
}

// createArgoCDSession creates a session JWT by posting admin credentials to ArgoCD.
func (r *PlatformBootstrapReconciler) createArgoCDSession(ctx context.Context, baseURL, password string) (string, error) {
	body := fmt.Sprintf(`{"username":"admin","password":"%s"}`, password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/session", strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("creating ArgoCD session: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ArgoCD session creation failed with status %d", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding ArgoCD session response: %w", err)
	}

	return result.Token, nil
}

// ensureRepoCrendentialSecret creates a repo credential Secret in the ArgoCD namespace
// if it does not already exist (idempotent).
func (r *PlatformBootstrapReconciler) ensureRepoCrendentialSecret(ctx context.Context, argoNS string, cred loopzerov1.ArgoCDRepoCredential, giteaToken string) error {
	var existing corev1.Secret
	err := r.Get(ctx, client.ObjectKey{Name: cred.Name, Namespace: argoNS}, &existing)
	if err == nil {
		// Secret already exists, skip.
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("checking repo credential secret: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cred.Name,
			Namespace: argoNS,
			Labels: map[string]string{
				"argocd.argoproj.io/secret-type": "repo-creds",
				"loopzero.io/managed-by":             "platformbootstrap-controller",
			},
		},
		StringData: map[string]string{
			"type":     "git",
			"url":      cred.URL,
			"username": "loopzero-platform",
			"password": giteaToken,
		},
	}

	if err := r.Create(ctx, secret); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil // Idempotent.
		}
		return fmt.Errorf("creating repo credential secret: %w", err)
	}
	return nil
}
