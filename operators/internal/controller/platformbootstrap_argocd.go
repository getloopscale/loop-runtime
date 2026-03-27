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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// reconcileArgoCDPhase handles the ConfiguringArgoCD phase.
// Steps: check reachability, validate/create JWT, register repo credentials.
func (r *PlatformBootstrapReconciler) reconcileArgoCDPhase(ctx context.Context, pb *loopzerov1.PlatformBootstrap, maxRetries int) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	argoURL := pb.Spec.ArgoCD.InternalURL
	argoNS := pb.Spec.ArgoCD.Namespace
	if argoNS == "" {
		argoNS = constants.DefaultArgoCDNamespace
	}

	// Step 1: Check ArgoCD reachable.
	if err := r.checkArgoCDReachable(ctx, argoURL); err != nil {
		logger.Info("argocd unreachable", "url", argoURL, "error", err)
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("ArgoCD unreachable: %v", err))
	}

	// Step 2: Check if token already exists in platform-tokens Secret.
	platformSecret, err := r.getSecret(ctx, constants.SecretPlatformTokens, pb.Namespace)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("platform tokens secret not found: %v", err))
	}

	existingToken, _ := secretString(platformSecret, constants.KeyArgoCDToken)
	if existingToken != "" {
		logger.Info("ArgoCD token already exists in platform-tokens, skipping JWT creation")
	} else {
		// Step 3: Read ArgoCD admin password from its initial admin secret.
		adminSecretName := pb.Spec.ArgoCD.AdminSecretName
		if adminSecretName == "" {
			adminSecretName = constants.SecretArgoCDInitialAdmin
		}

		var adminSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{Name: adminSecretName, Namespace: argoNS}, &adminSecret); err != nil {
			if errors.IsNotFound(err) {
				return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("ArgoCD admin secret %s/%s not found", argoNS, adminSecretName))
			}
			return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to read ArgoCD admin secret: %v", err))
		}

		adminPassword, passErr := secretString(&adminSecret, constants.KeyPassword)
		if passErr != nil {
			return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("ArgoCD admin secret: %v", passErr))
		}

		// Step 4: Create JWT via ArgoCD session API.
		jwt, err := r.createArgoCDSession(ctx, argoURL, adminPassword)
		if err != nil {
			return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to create ArgoCD session: %v", err))
		}

		// Step 5: Store JWT in platform-tokens Secret.
		if err := r.updateSecretData(ctx, platformSecret, constants.KeyArgoCDToken, jwt); err != nil {
			return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to store ArgoCD token: %v", err))
		}
	}

	// Step 6: Create repo credential Secrets in ArgoCD namespace.
	giteaToken, giteaErr := secretString(platformSecret, constants.KeyGiteaToken)
	if giteaErr != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("platform-tokens missing Gitea token: %v", giteaErr))
	}
	for _, repoCred := range pb.Spec.ArgoCD.RepoCredentials {
		if err := r.ensureRepoCredentialSecret(ctx, argoNS, repoCred, giteaToken); err != nil {
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
	bodyBytes, marshalErr := json.Marshal(map[string]string{"username": constants.DefaultArgoCDUsername, "password": password})
	if marshalErr != nil {
		return "", fmt.Errorf("marshal ArgoCD session request: %w", marshalErr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/session", strings.NewReader(string(bodyBytes)))
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

// ensureRepoCredentialSecret creates or updates a repo credential Secret in the ArgoCD namespace.
// Uses CreateOrUpdate so credential rotation (e.g., Gitea token refresh) is reflected.
func (r *PlatformBootstrapReconciler) ensureRepoCredentialSecret(ctx context.Context, argoNS string, cred loopzerov1.ArgoCDRepoCredential, giteaToken string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cred.Name,
			Namespace: argoNS,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		if secret.Labels == nil {
			secret.Labels = make(map[string]string)
		}
		secret.Labels[constants.ArgoCDSecretTypeLabel] = constants.ArgoCDSecretTypeRepoCreds
		secret.Labels[constants.LabelManagedBy] = constants.ManagedByPlatformBootstrapController
		secret.StringData = map[string]string{
			"type":     "git",
			"url":      cred.URL,
			"username": constants.DefaultArgoCDRepoUsername,
			"password": giteaToken,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("repo credential secret %s/%s: %w", argoNS, cred.Name, err)
	}
	return nil
}
