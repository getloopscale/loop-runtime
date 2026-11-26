// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/gitea"
	"github.com/getloopscale/loop-runtime/operators/internal/reconcileloop"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// reconcileGiteaPhase handles the ConfiguringGitea phase.
// Steps: check reachability, validate/create admin token, ensure orgs exist.
func (r *PlatformBootstrapReconciler) reconcileGiteaPhase(ctx context.Context, pb, base *loopzerov1.PlatformBootstrap, maxRetries int) (ctrl.Result, error) {
	ctx, span := reconcileloop.StartChildSpan(ctx, "bootstrap.gitea.configure")
	defer span.End()
	logger := log.FromContext(ctx)

	giteaClient := r.giteaClient(pb.Spec.Gitea.InternalURL)

	// Step 1: Check Gitea reachable.
	if err := giteaClient.CheckVersion(ctx); err != nil {
		logger.Info("gitea unreachable", "url", pb.Spec.Gitea.InternalURL, "error", err)
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("Gitea unreachable: %v", err))
	}

	// Step 2: Get admin credentials from Secret.
	adminSecret, err := r.getSecret(ctx, pb.Spec.Gitea.AdminSecretRef.Name, pb.Namespace)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("admin secret not found: %v", err))
	}

	username, err := secretString(adminSecret, constants.KeyUsername)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("admin secret missing key %q", constants.KeyUsername))
	}
	password, err := secretString(adminSecret, constants.KeyPassword)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("admin secret missing key %q", constants.KeyPassword))
	}

	// Step 3: Check if token already exists in platform-tokens Secret.
	platformSecret, err := r.getSecret(ctx, constants.SecretPlatformTokens, pb.Namespace)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("platform tokens secret not found: %v", err))
	}

	existingToken, _ := secretString(platformSecret, constants.KeyGiteaToken)
	if existingToken != "" {
		// Validate the existing token.
		if err := giteaClient.ValidateToken(ctx, existingToken); err == nil {
			logger.Info("existing Gitea token is valid, skipping token creation")
			// Token valid, proceed to ensure orgs.
			if err := r.ensureGiteaOrgs(ctx, giteaClient, existingToken, pb.Spec.Gitea.Organizations); err != nil {
				return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("failed to ensure orgs: %v", err))
			}

			r.setCondition(pb, conditions.TypeGiteaReady, metav1.ConditionTrue, conditions.ReasonReady, "Gitea configured")
			return r.transitionToNextPhase(ctx, pb, base, statemachine.PBPEventGiteaReady)
		}
		logger.Info("existing Gitea token is invalid, will create new one")
	}

	// Step 4: Create a new admin token via Gitea API.
	tokenName := pb.Spec.Gitea.TokenName
	if tokenName == "" {
		tokenName = constants.DefaultGiteaTokenName
	}

	token, err := r.createGiteaToken(ctx, giteaClient, username, password, tokenName)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("failed to create Gitea token: %v", err))
	}

	// Step 5: Store token in platform-tokens Secret.
	if err := r.updateSecretData(ctx, platformSecret, constants.KeyGiteaToken, token); err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("failed to store Gitea token: %v", err))
	}

	// Step 6: Ensure organizations exist.
	if err := r.ensureGiteaOrgs(ctx, giteaClient, token, pb.Spec.Gitea.Organizations); err != nil {
		return r.requeueWithRetry(ctx, pb, base, maxRetries, fmt.Sprintf("failed to ensure orgs: %v", err))
	}

	r.setCondition(pb, conditions.TypeGiteaReady, metav1.ConditionTrue, conditions.ReasonReady, "Gitea configured")
	return r.transitionToNextPhase(ctx, pb, base, statemachine.PBPEventGiteaReady)
}

// createGiteaToken creates a new admin API token. It first checks if a token
// with the given name already exists and deletes it (idempotent).
func (r *PlatformBootstrapReconciler) createGiteaToken(ctx context.Context, giteaClient gitea.BootstrapClient, username, password, tokenName string) (string, error) {
	// Check if token already exists by listing tokens.
	tokens, err := giteaClient.ListTokens(ctx, username, password)
	if err != nil {
		// CODE-012: Log the error instead of silently proceeding to CreateToken.
		// ListTokens failure means we cannot check for duplicates -- the CreateToken
		// call may fail with a conflict if a token with this name already exists.
		log.FromContext(ctx).Info("ListTokens failed, proceeding to create", "error", err, "tokenName", tokenName)
	}
	if err == nil {
		for _, t := range tokens {
			if t.Name == tokenName {
				// Token exists but we cannot retrieve its value from the list.
				// Delete the old one first for idempotency.
				if delErr := giteaClient.DeleteToken(ctx, username, password, t.ID); delErr != nil {
					return "", fmt.Errorf("deleting existing token %q (id=%d): %w", tokenName, t.ID, delErr)
				}
				break
			}
		}
	}

	// Create the token.
	token, err := giteaClient.CreateToken(ctx, username, password, tokenName)
	if err != nil {
		return "", fmt.Errorf("creating Gitea token: %w", err)
	}
	return token, nil
}

// ensureGiteaOrgs creates Gitea organizations if they don't exist.
func (r *PlatformBootstrapReconciler) ensureGiteaOrgs(ctx context.Context, giteaClient gitea.BootstrapClient, token string, orgs []string) error {
	for _, org := range orgs {
		if err := giteaClient.EnsureOrg(ctx, token, org); err != nil {
			return fmt.Errorf("ensuring org %s: %w", org, err)
		}
	}
	return nil
}
