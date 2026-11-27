// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/reconcileloop"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// reconcileArgoCDPhase handles the ConfiguringArgoCD phase.
// Steps: check reachability, validate/create JWT, register repo credentials.
// ARCH-OP-001: Uses ArgoCDBootstrapClient interface for HTTP operations.
func (r *PlatformBootstrapReconciler) reconcileArgoCDPhase(ctx context.Context, pb, base *loopruntimev1.PlatformBootstrap, maxRetries int) (ctrl.Result, error) {
	ctx, span := reconcileloop.StartChildSpan(ctx, "bootstrap.argocd.configure")
	defer span.End()
	logger := log.FromContext(ctx)
	argoURL := strings.TrimRight(pb.Spec.ArgoCD.InternalURL, "/")
	argoNS := pb.Spec.ArgoCD.Namespace
	if argoNS == "" {
		argoNS = constants.DefaultArgoCDNamespace
	}

	argoClient := r.argoCDClient(argoURL)

	// Step 1: Check ArgoCD reachable.
	if err := argoClient.CheckReachable(ctx); err != nil {
		logger.Info("argocd unreachable", "url", argoURL, "error", err)
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("ArgoCD unreachable: %v", err))
	}

	// Step 2: Check if token already exists in platform-tokens Secret.
	platformSecret, err := r.getSecret(ctx, constants.SecretPlatformTokens, pb.Namespace)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("platform tokens secret not found: %v", err))
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
				return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("ArgoCD admin secret %s/%s not found", argoNS, adminSecretName))
			}
			return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("failed to read ArgoCD admin secret: %v", err))
		}

		adminPassword, passErr := secretString(&adminSecret, constants.KeyPassword)
		if passErr != nil {
			return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("ArgoCD admin secret: %v", passErr))
		}

		// Step 4: Generate apiKey token for loop-runtime-api account (non-expiring).
		// MEET-2026-03-28-013: replaced createArgoCDSession (24h session JWT) with
		// generateArgoCDApiKey (non-expiring apiKey scoped to sync+get via RBAC).
		apiKeyToken, err := argoClient.GenerateAPIKey(ctx, adminPassword)
		if err != nil {
			return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("failed to generate ArgoCD apiKey: %v", err))
		}

		// Step 5: Store apiKey JWT in platform-tokens Secret.
		if err := r.updateSecretData(ctx, platformSecret, constants.KeyArgoCDToken, apiKeyToken); err != nil {
			return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("failed to store ArgoCD token: %v", err))
		}
	}

	// Step 6: Create repo credential Secrets in ArgoCD namespace.
	giteaToken, giteaErr := secretString(platformSecret, constants.KeyGiteaToken)
	if giteaErr != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("platform-tokens missing Gitea token: %v", giteaErr))
	}
	for _, repoCred := range pb.Spec.ArgoCD.RepoCredentials {
		if err := r.ensureRepoCredentialSecret(ctx, argoNS, repoCred, giteaToken); err != nil {
			return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("failed to create repo credential %s: %v", repoCred.Name, err))
		}
	}

	r.setCondition(pb, conditions.TypeArgoCDReady, metav1.ConditionTrue, conditions.ReasonReady, "ArgoCD configured")
	return r.transitionToNextPhase(ctx, pb, base, statemachine.PBPEventArgoCDReady)
}

// argoCDHTTPClient is the default ArgoCDBootstrapClient implementation using HTTP.
// ARCH-OP-001: Extracted from inline HTTP calls in the reconciler for testability.
type argoCDHTTPClient struct {
	baseURL string
	client  *http.Client
}

// CheckReachable verifies ArgoCD is reachable by calling GET /api/version.
func (c *argoCDHTTPClient) CheckReachable(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/version", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to ArgoCD: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from ArgoCD version endpoint", resp.StatusCode)
	}
	return nil
}

// GenerateAPIKey creates a non-expiring apiKey token for the loop-runtime-api account.
// MEET-2026-03-28-013: replaces createArgoCDSession which generated 24h session JWTs.
//
// Flow:
//  1. Login as admin to get a temporary session token
//  2. Use the session token to generate an apiKey for loop-runtime-api account
//  3. Return the apiKey JWT (non-expiring, scoped via ArgoCD RBAC)
func (c *argoCDHTTPClient) GenerateAPIKey(ctx context.Context, password string) (string, error) {
	// Step 1: Login as admin (temporary session, only used to generate apiKey)
	sessionBody, err := json.Marshal(map[string]string{
		"username": constants.DefaultArgoCDUsername,
		"password": password,
	})
	if err != nil {
		return "", fmt.Errorf("marshal ArgoCD session request: %w", err)
	}
	sessionReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/session", strings.NewReader(string(sessionBody)))
	if err != nil {
		return "", fmt.Errorf("creating session request: %w", err)
	}
	sessionReq.Header.Set("Content-Type", "application/json")

	sessionResp, err := c.client.Do(sessionReq)
	if err != nil {
		return "", fmt.Errorf("creating ArgoCD admin session: %w", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()

	if sessionResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ArgoCD admin login failed with status %d", sessionResp.StatusCode)
	}

	var sessionResult struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(sessionResp.Body).Decode(&sessionResult); err != nil {
		return "", fmt.Errorf("decoding ArgoCD session response: %w", err)
	}

	// Step 2: Generate apiKey token for loop-runtime-api account
	tokenBody, err := json.Marshal(map[string]string{"name": "platform-deploy"})
	if err != nil {
		return "", fmt.Errorf("marshal ArgoCD token request: %w", err)
	}
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/account/"+constants.ArgoCDAPIAccount+"/token",
		strings.NewReader(string(tokenBody)))
	if err != nil {
		return "", fmt.Errorf("creating apiKey request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenReq.Header.Set("Authorization", "Bearer "+sessionResult.Token)

	tokenResp, err := c.client.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("generating ArgoCD apiKey: %w", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	if tokenResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ArgoCD apiKey generation failed with status %d (account %s may not exist)",
			tokenResp.StatusCode, constants.ArgoCDAPIAccount)
	}

	var tokenResult struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenResult); err != nil {
		return "", fmt.Errorf("decoding ArgoCD apiKey response: %w", err)
	}

	return tokenResult.Token, nil
}

// ensureRepoCredentialSecret creates or updates a repo credential Secret in the ArgoCD namespace.
// Uses CreateOrUpdate so credential rotation (e.g., Gitea token refresh) is reflected.
func (r *PlatformBootstrapReconciler) ensureRepoCredentialSecret(ctx context.Context, argoNS string, cred loopruntimev1.ArgoCDRepoCredential, giteaToken string) error {
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
