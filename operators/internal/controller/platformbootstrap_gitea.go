package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// reconcileGiteaPhase handles the ConfiguringGitea phase.
// Steps: check reachability, validate/create admin token, ensure orgs exist.
func (r *PlatformBootstrapReconciler) reconcileGiteaPhase(ctx context.Context, pb *loopzerov1.PlatformBootstrap, maxRetries int) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	giteaURL := pb.Spec.Gitea.InternalURL

	// Step 1: Check Gitea reachable.
	if err := r.checkGiteaReachable(ctx, giteaURL); err != nil {
		logger.Info("gitea unreachable", "url", giteaURL, "error", err)
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("Gitea unreachable: %v", err))
	}

	// Step 2: Get admin credentials from Secret.
	adminSecret, err := r.getSecret(ctx, pb.Spec.Gitea.AdminSecretRef.Name, pb.Namespace)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("admin secret not found: %v", err))
	}

	username := string(adminSecret.Data["username"])
	password := string(adminSecret.Data["password"])

	// Step 3: Check if token already exists in platform-tokens Secret.
	platformSecret, err := r.getSecret(ctx, "loopzero-platform-tokens", pb.Namespace)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("platform tokens secret not found: %v", err))
	}

	existingToken := string(platformSecret.Data["GITEA_TOKEN"])
	if existingToken != "" {
		// Validate the existing token.
		if err := r.validateGiteaToken(ctx, giteaURL, existingToken); err == nil {
			logger.Info("existing Gitea token is valid, skipping token creation")
			// Token valid, proceed to ensure orgs.
			if err := r.ensureGiteaOrgs(ctx, giteaURL, existingToken, pb.Spec.Gitea.Organizations); err != nil {
				return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to ensure orgs: %v", err))
			}

			r.setCondition(pb, conditions.TypeGiteaReady, metav1.ConditionTrue, conditions.ReasonReady, "Gitea configured")
			return r.transitionToNextPhase(ctx, pb, statemachine.PlatformBootstrapGiteaReady, "Gitea configuration complete")
		}
		logger.Info("existing Gitea token is invalid, will create new one")
	}

	// Step 4: Create a new admin token via Gitea API.
	tokenName := pb.Spec.Gitea.TokenName
	if tokenName == "" {
		tokenName = "platform-bootstrap-token"
	}

	token, err := r.createGiteaToken(ctx, giteaURL, username, password, tokenName)
	if err != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to create Gitea token: %v", err))
	}

	// Step 5: Store token in platform-tokens Secret.
	if err := r.updateSecretData(ctx, platformSecret, "GITEA_TOKEN", token); err != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to store Gitea token: %v", err))
	}

	// Step 6: Ensure organizations exist.
	if err := r.ensureGiteaOrgs(ctx, giteaURL, token, pb.Spec.Gitea.Organizations); err != nil {
		return r.requeueWithRetry(ctx, pb, maxRetries, fmt.Sprintf("failed to ensure orgs: %v", err))
	}

	r.setCondition(pb, conditions.TypeGiteaReady, metav1.ConditionTrue, conditions.ReasonReady, "Gitea configured")
	return r.transitionToNextPhase(ctx, pb, statemachine.PlatformBootstrapGiteaReady, "Gitea configuration complete")
}

// checkGiteaReachable checks if Gitea is reachable by calling GET /api/v1/version.
func (r *PlatformBootstrapReconciler) checkGiteaReachable(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/version", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("connecting to Gitea: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from Gitea version endpoint", resp.StatusCode)
	}
	return nil
}

// validateGiteaToken validates a Gitea token by calling GET /api/v1/user.
func (r *PlatformBootstrapReconciler) validateGiteaToken(ctx context.Context, baseURL, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token)
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token validation failed with status %d", resp.StatusCode)
	}
	return nil
}

// createGiteaToken creates a new admin API token. It first checks if a token
// with the given name already exists (idempotent).
func (r *PlatformBootstrapReconciler) createGiteaToken(ctx context.Context, baseURL, username, password, tokenName string) (string, error) {
	// Check if token already exists by listing tokens.
	listURL := fmt.Sprintf("%s/api/v1/users/%s/tokens", baseURL, username)
	listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating list tokens request: %w", err)
	}
	listReq.SetBasicAuth(username, password)
	listResp, err := r.httpClient().Do(listReq)
	if err != nil {
		return "", fmt.Errorf("listing tokens: %w", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode == http.StatusOK {
		var tokens []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(listResp.Body).Decode(&tokens); err == nil {
			for _, t := range tokens {
				if t.Name == tokenName {
					// Token exists but we cannot retrieve its value from the list.
					// We need to create a new one. Delete the old one first for idempotency.
					deleteURL := fmt.Sprintf("%s/api/v1/users/%s/tokens/%d", baseURL, username, t.ID)
					delReq, _ := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
					delReq.SetBasicAuth(username, password)
					delResp, delErr := r.httpClient().Do(delReq)
					if delErr == nil {
						_ = delResp.Body.Close()
					}
					break
				}
			}
		}
	}

	// Create the token.
	createURL := fmt.Sprintf("%s/api/v1/users/%s/tokens", baseURL, username)
	body := fmt.Sprintf(`{"name":"%s"}`, tokenName)
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}
	createReq.SetBasicAuth(username, password)
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := r.httpClient().Do(createReq)
	if err != nil {
		return "", fmt.Errorf("creating token: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(createResp.Body)
		return "", fmt.Errorf("token creation failed with status %d: %s", createResp.StatusCode, string(respBody))
	}

	var result struct {
		SHA1 string `json:"sha1"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	return result.SHA1, nil
}

// ensureGiteaOrgs creates Gitea organizations if they don't exist.
func (r *PlatformBootstrapReconciler) ensureGiteaOrgs(ctx context.Context, baseURL, token string, orgs []string) error {
	for _, org := range orgs {
		// Check if org exists.
		checkURL := fmt.Sprintf("%s/api/v1/orgs/%s", baseURL, org)
		checkReq, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
		if err != nil {
			return fmt.Errorf("creating org check request: %w", err)
		}
		checkReq.Header.Set("Authorization", "token "+token)
		checkResp, err := r.httpClient().Do(checkReq)
		if err != nil {
			return fmt.Errorf("checking org %s: %w", org, err)
		}
		_ = checkResp.Body.Close()

		if checkResp.StatusCode == http.StatusOK {
			continue // Org exists.
		}

		// Create org.
		createURL := baseURL + "/api/v1/orgs"
		body := fmt.Sprintf(`{"username":"%s","visibility":"private"}`, org)
		createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, strings.NewReader(body))
		if err != nil {
			return fmt.Errorf("creating org request: %w", err)
		}
		createReq.Header.Set("Authorization", "token "+token)
		createReq.Header.Set("Content-Type", "application/json")

		createResp, err := r.httpClient().Do(createReq)
		if err != nil {
			return fmt.Errorf("creating org %s: %w", org, err)
		}
		_ = createResp.Body.Close()

		// 201 Created or 422 already exists are both acceptable.
		if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusUnprocessableEntity {
			return fmt.Errorf("unexpected status %d when creating org %s", createResp.StatusCode, org)
		}
	}
	return nil
}
