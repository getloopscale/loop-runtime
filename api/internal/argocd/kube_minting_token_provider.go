// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/loopzero/loopzero/api/internal/metrics"
)

// KubeMintingTokenProvider is a self-healing ArgoCD token provider:
//
//   1. Reads the current token from a K8s Secret (default: loopzero-api-tokens.argocd-token).
//   2. Inspects the token's JWT exp claim and refreshes proactively when
//      the token has < ProactiveRefreshThreshold time until expiry.
//   3. On reactive auth-failure signal, re-mints the token immediately.
//   4. Mints new tokens via ArgoCD's POST /api/v1/account/{account}/token
//      using credentials from a separate Secret (default:
//      argocd-refresh-credentials with keys argocd-username + argocd-password
//      OR argocd-account + argocd-account-token).
//   5. Persists the new token back to the same K8s Secret AND updates the
//      in-memory atomic value used by SyncClient on its hot path.
//   6. Exposes Prometheus metrics:
//        loopzero_argocd_token_age_seconds        — gauge, sec since last mint
//        loopzero_argocd_token_refreshes_total    — counter, labels: trigger,
//                                                outcome ∈ {success,error}
//
// Goroutine model: a single background loop ticks every PollInterval and
// performs the proactive check; reactive refreshes flow through a 1-buffered
// channel so concurrent OnAuthFailure() calls coalesce into one mint cycle
// (single-flight). All public state reads are lock-free (atomic.Value).
//
// FAANG-grade requirements driven by 2026-04-30 NEW-15: ArgoCD admin session
// tokens expire after 24h; loopzero-api had no automatic refresh path so a
// single 24h+ uptime window broke project delete + every post-deploy sync
// silently. This provider eliminates the operational burden of manual
// `task k3d:heal` H11 by self-healing in-process.
type KubeMintingTokenProvider struct {
	cfg KubeMintingConfig
	log *slog.Logger

	token        atomic.Value // string
	tokenIssued  atomic.Int64 // unix seconds; 0 means "unknown"
	refreshMu    sync.Mutex   // serializes mint operations (single-flight)
	refreshSig   chan struct{}
	stopCh       chan struct{}
	httpClient   *http.Client
	stopOnce     sync.Once
}

// KubeMintingConfig wires the dependencies for KubeMintingTokenProvider.
type KubeMintingConfig struct {
	// ArgoCDBaseURL — used to mint new tokens. Required.
	// e.g. http://argocd-server.argocd.svc.cluster.local:80
	ArgoCDBaseURL string

	// AccountName — ArgoCD account whose token is minted. Default: "loopzero-api".
	// Must have apiKey capability configured in argocd-cm.
	AccountName string

	// Insecure — skip TLS verification when minting (local k3d only).
	Insecure bool

	// HTTPClient — optional shared client. When nil, one is created with the
	// Insecure flag honored. Timeout is forced to 15s to keep the goroutine
	// from blocking the metrics gauge update on a hung ArgoCD.
	HTTPClient *http.Client

	// KubeClient — used to read refresh credentials Secret + write the
	// minted token back to the storage Secret. Required.
	KubeClient kubernetes.Interface

	// Namespace where both Secrets live. Default: "loopzero-system".
	Namespace string

	// TokenSecretName — Secret holding the active token. Default:
	// "loopzero-api-tokens". Updated in place on every mint.
	TokenSecretName string

	// TokenSecretKey — data key in TokenSecretName. Default: "argocd-token".
	TokenSecretKey string

	// CredsSecretName — Secret holding the refresh credentials.
	// Default: "argocd-refresh-credentials". Looked up keys (in order):
	//   "argocd-username" + "argocd-password"     — admin login flow
	//   "argocd-account-token" (already-minted   — fallback bootstrap value
	//                          static apiKey)
	CredsSecretName string

	// ProactiveRefreshThreshold — refresh proactively when exp - now is below
	// this. Default: 6h. Tokens with exp=0 (apiKey accounts) have no proactive
	// refresh; only reactive on auth failure.
	ProactiveRefreshThreshold time.Duration

	// PollInterval — proactive check cadence. Default: 5min.
	PollInterval time.Duration

	// Logger — slog logger; defaults to slog.Default().
	Logger *slog.Logger
}

// NewKubeMintingTokenProvider constructs a provider and seeds the in-memory
// token from the storage Secret. It does NOT mint on construction; the
// caller must invoke Start(ctx) to begin the polling loop. If the token in
// the Secret is invalid (expired or empty), Start will mint immediately.
func NewKubeMintingTokenProvider(cfg KubeMintingConfig) (*KubeMintingTokenProvider, error) {
	// Audit gap M02 #13 fix: typed LoopZeroError for constructor validation.
	if cfg.ArgoCDBaseURL == "" {
		return nil, loopzeroerrors.ErrMissingField.Wrap("argocd: ArgoCDBaseURL required")
	}
	if cfg.KubeClient == nil {
		return nil, loopzeroerrors.ErrMissingField.Wrap("argocd: KubeClient required")
	}
	if cfg.AccountName == "" {
		cfg.AccountName = "loopzero-api"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "loopzero-system"
	}
	if cfg.TokenSecretName == "" {
		cfg.TokenSecretName = "loopzero-api-tokens"
	}
	if cfg.TokenSecretKey == "" {
		cfg.TokenSecretKey = "argocd-token"
	}
	if cfg.CredsSecretName == "" {
		cfg.CredsSecretName = "argocd-refresh-credentials"
	}
	if cfg.ProactiveRefreshThreshold <= 0 {
		cfg.ProactiveRefreshThreshold = 6 * time.Hour
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure}, //nolint:gosec // local k3d only
			},
		}
	}

	p := &KubeMintingTokenProvider{
		cfg:        cfg,
		log:        cfg.Logger.With("component", "argocd-token-provider"),
		refreshSig: make(chan struct{}, 1),
		stopCh:     make(chan struct{}),
		httpClient: httpClient,
	}

	// Seed from storage Secret if present. A missing/empty token is non-fatal;
	// Start() will mint on first tick.
	if seeded, issuedAt, err := p.readSecretToken(context.Background()); err == nil && seeded != "" {
		p.token.Store(seeded)
		p.tokenIssued.Store(issuedAt)
		updateAgeMetric(issuedAt)
		p.log.Info("seeded argocd token from K8s Secret",
			"secret", cfg.TokenSecretName, "issued_at_unix", issuedAt)
	}

	return p, nil
}

// Token returns the current cached token (lock-free atomic read).
func (p *KubeMintingTokenProvider) Token() string {
	v := p.token.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// Refresh implements TokenProvider.Refresh — synchronous mint + persist.
// Single-flight: concurrent callers wait for the in-flight mint to finish.
// Used by the SyncClient retry path on auth failure.
func (p *KubeMintingTokenProvider) Refresh() error {
	return p.mintAndPersist(context.Background(), "manual")
}

// OnAuthFailure signals the background loop that the current token failed
// authentication and a fresh mint is required. Non-blocking: if a refresh
// is already queued, this is a no-op (the in-flight one will satisfy us).
func (p *KubeMintingTokenProvider) OnAuthFailure() {
	select {
	case p.refreshSig <- struct{}{}:
	default:
		// already queued
	}
}

// Start begins the background polling + reactive refresh loop. Returns
// when ctx is cancelled OR Stop() is called. Must be invoked exactly once.
func (p *KubeMintingTokenProvider) Start(ctx context.Context) {
	go p.run(ctx)
}

func (p *KubeMintingTokenProvider) run(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	// First check happens immediately so an empty/expired seed is handled
	// at startup without waiting for the first tick.
	p.checkAndMaybeRefresh(ctx, "startup")

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.checkAndMaybeRefresh(ctx, "proactive")
		case <-p.refreshSig:
			if err := p.mintAndPersist(ctx, "auth_failure"); err != nil {
				p.log.Error("reactive token refresh failed", "error", err)
			}
		}
	}
}

// Stop ends the background loop. Idempotent.
func (p *KubeMintingTokenProvider) Stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
}

// checkAndMaybeRefresh inspects the JWT exp; mints if expired or near expiry.
func (p *KubeMintingTokenProvider) checkAndMaybeRefresh(ctx context.Context, trigger string) {
	tok := p.Token()
	if tok == "" {
		// No token at all — must mint to bootstrap.
		if err := p.mintAndPersist(ctx, trigger+"_empty"); err != nil {
			p.log.Error("bootstrap token mint failed", "error", err)
		}
		return
	}
	exp := jwtExp(tok)
	if exp == 0 {
		// No exp claim → non-expiring (apiKey-style); just refresh metric.
		updateAgeMetric(p.tokenIssued.Load())
		return
	}
	now := time.Now().Unix()
	remaining := time.Duration(exp-now) * time.Second
	if remaining < p.cfg.ProactiveRefreshThreshold {
		p.log.Info("argocd token nearing expiry, refreshing proactively",
			"remaining_seconds", int(remaining.Seconds()),
			"threshold_seconds", int(p.cfg.ProactiveRefreshThreshold.Seconds()))
		if err := p.mintAndPersist(ctx, trigger); err != nil {
			p.log.Error("proactive token refresh failed", "error", err)
		}
		return
	}
	updateAgeMetric(p.tokenIssued.Load())
}

// mintAndPersist mints a new token via the ArgoCD API and stores it in:
//   1. the K8s Secret cfg.TokenSecretName (data[cfg.TokenSecretKey])
//   2. the in-memory atomic.Value
// Single-flight via refreshMu — concurrent callers serialize.
func (p *KubeMintingTokenProvider) mintAndPersist(ctx context.Context, trigger string) error {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()

	// Double-check after acquiring the lock: another goroutine may have
	// already minted while we were waiting.
	if cur := p.Token(); cur != "" && jwtIsValid(cur, p.cfg.ProactiveRefreshThreshold) {
		// Skip — refreshed by a concurrent caller while we waited.
		metrics.ArgoCDTokenRefreshesTotal.WithLabelValues(trigger, "skipped").Inc()
		return nil
	}

	creds, err := p.readCredentials(ctx)
	if err != nil {
		metrics.ArgoCDTokenRefreshesTotal.WithLabelValues(trigger, "error").Inc()
		return fmt.Errorf("read refresh credentials: %w", err)
	}

	newToken, err := p.mintToken(ctx, creds)
	if err != nil {
		metrics.ArgoCDTokenRefreshesTotal.WithLabelValues(trigger, "error").Inc()
		return fmt.Errorf("mint argocd token: %w", err)
	}
	if newToken == "" {
		metrics.ArgoCDTokenRefreshesTotal.WithLabelValues(trigger, "error").Inc()
		// Audit gap M02 #13 fix: typed LoopZeroError so this surfaces with a
		// stable code in token-refresh failure metrics + audit logs.
		return loopzeroerrors.ErrInternalError.Wrap("argocd minted empty token")
	}

	if err := p.writeSecretToken(ctx, newToken); err != nil {
		metrics.ArgoCDTokenRefreshesTotal.WithLabelValues(trigger, "error").Inc()
		return fmt.Errorf("persist token to Secret: %w", err)
	}

	now := time.Now().Unix()
	p.token.Store(newToken)
	p.tokenIssued.Store(now)
	updateAgeMetric(now)
	metrics.ArgoCDTokenRefreshesTotal.WithLabelValues(trigger, "success").Inc()
	p.log.Info("argocd token refreshed",
		"trigger", trigger,
		"prefix", safePrefix(newToken),
		"persisted_to_secret", p.cfg.TokenSecretName)
	return nil
}

type refreshCreds struct {
	username     string
	password     string
	bearerToken  string // pre-existing apiKey token used to mint a new one
}

func (p *KubeMintingTokenProvider) readCredentials(ctx context.Context) (refreshCreds, error) {
	sec, err := p.cfg.KubeClient.CoreV1().Secrets(p.cfg.Namespace).Get(
		ctx, p.cfg.CredsSecretName, metav1.GetOptions{},
	)
	if err != nil {
		return refreshCreds{}, fmt.Errorf("get %s/%s: %w",
			p.cfg.Namespace, p.cfg.CredsSecretName, err)
	}
	creds := refreshCreds{
		username:    string(sec.Data["argocd-username"]),
		password:    string(sec.Data["argocd-password"]),
		bearerToken: string(sec.Data["argocd-account-token"]),
	}
	if creds.bearerToken == "" && (creds.username == "" || creds.password == "") {
		return refreshCreds{}, fmt.Errorf("secret %s missing both bearer token and user/password",
			p.cfg.CredsSecretName)
	}
	return creds, nil
}

// mintToken obtains a session token via either:
//   (a) admin /api/v1/session login with username+password, OR
//   (b) reusing a pre-existing apiKey bearer token directly,
// and then exchanges that for a new loopzero-api account token via
// /api/v1/account/{AccountName}/token.
func (p *KubeMintingTokenProvider) mintToken(ctx context.Context, creds refreshCreds) (string, error) {
	bearer := creds.bearerToken
	if bearer == "" {
		// Username/password → session token
		sess, err := p.argocdLogin(ctx, creds.username, creds.password)
		if err != nil {
			return "", fmt.Errorf("argocd login: %w", err)
		}
		bearer = sess
	}

	url := fmt.Sprintf("%s/api/v1/account/%s/token", p.cfg.ArgoCDBaseURL, p.cfg.AccountName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return "", fmt.Errorf("build mint request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call mint endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := readSnippet(resp)
		return "", fmt.Errorf("mint http %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode mint response: %w", err)
	}
	return strings.TrimSpace(out.Token), nil
}

func (p *KubeMintingTokenProvider) argocdLogin(ctx context.Context, user, pass string) (string, error) {
	url := p.cfg.ArgoCDBaseURL + "/api/v1/session"
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, user, pass)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := readSnippet(resp)
		return "", fmt.Errorf("login http %d: %s", resp.StatusCode, snippet)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Token), nil
}

// readSecretToken returns (token, issuedAtUnix, error). issuedAt is taken
// from the token's iat JWT claim; 0 if the claim is missing.
func (p *KubeMintingTokenProvider) readSecretToken(ctx context.Context) (string, int64, error) {
	sec, err := p.cfg.KubeClient.CoreV1().Secrets(p.cfg.Namespace).Get(
		ctx, p.cfg.TokenSecretName, metav1.GetOptions{},
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return "", 0, nil
		}
		return "", 0, err
	}
	tok := strings.TrimSpace(string(sec.Data[p.cfg.TokenSecretKey]))
	return tok, jwtIat(tok), nil
}

// writeSecretToken updates Secret in place via Update (creates if missing).
func (p *KubeMintingTokenProvider) writeSecretToken(ctx context.Context, token string) error {
	sec, err := p.cfg.KubeClient.CoreV1().Secrets(p.cfg.Namespace).Get(
		ctx, p.cfg.TokenSecretName, metav1.GetOptions{},
	)
	if k8serrors.IsNotFound(err) {
		sec = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      p.cfg.TokenSecretName,
				Namespace: p.cfg.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{p.cfg.TokenSecretKey: []byte(token)},
		}
		_, err = p.cfg.KubeClient.CoreV1().Secrets(p.cfg.Namespace).Create(
			ctx, sec, metav1.CreateOptions{},
		)
		return err
	}
	if err != nil {
		return err
	}
	if sec.Data == nil {
		sec.Data = map[string][]byte{}
	}
	sec.Data[p.cfg.TokenSecretKey] = []byte(token)
	_, err = p.cfg.KubeClient.CoreV1().Secrets(p.cfg.Namespace).Update(
		ctx, sec, metav1.UpdateOptions{},
	)
	return err
}

// updateAgeMetric refreshes loopzero_argocd_token_age_seconds.
// issuedAt = 0 → age unknown; the metric is set to NaN-substitute -1
// (matches Prometheus convention "stale" handled by alerts).
func updateAgeMetric(issuedAt int64) {
	if issuedAt <= 0 {
		metrics.ArgoCDTokenAgeSeconds.Set(-1)
		return
	}
	metrics.ArgoCDTokenAgeSeconds.Set(float64(time.Now().Unix() - issuedAt))
}

// jwtExp parses the JWT exp claim. Returns 0 if missing/unparseable.
func jwtExp(token string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Try standard padding (some encoders pad)
		payload, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return 0
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0
	}
	return claims.Exp
}

// jwtIat extracts the iat claim (issued-at).
func jwtIat(token string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return 0
		}
	}
	var claims struct {
		Iat int64 `json:"iat"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0
	}
	return claims.Iat
}

// jwtIsValid returns true when the token has either exp=0 (non-expiring) OR
// exp - now > threshold. Used by the single-flight double-check guard.
func jwtIsValid(token string, threshold time.Duration) bool {
	exp := jwtExp(token)
	if exp == 0 {
		return true
	}
	return time.Until(time.Unix(exp, 0)) > threshold
}

// readSnippet drains up to 256 bytes from a response body for error messages.
func readSnippet(resp *http.Response) (string, error) {
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	return strings.TrimSpace(string(buf[:n])), nil
}
