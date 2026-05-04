// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goJWT "github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Test helpers: RSA key generation and JWKS server
// ---------------------------------------------------------------------------

func generateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

// jwksJSON builds a JWKS response for the given kid→public-key pairs.
func jwksJSON(keys map[string]*rsa.PublicKey) []byte {
	type jwkEntry struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
		Use string `json:"use"`
	}
	var entries []jwkEntry
	for kid, pub := range keys {
		entries = append(entries, jwkEntry{
			Kid: kid,
			Kty: "RSA",
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			Use: "sig",
		})
	}
	data, _ := json.Marshal(map[string]interface{}{"keys": entries})
	return data
}

// serveJWKS starts an httptest server that returns the given JWKS payload.
func serveJWKS(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// signRSAToken creates a signed JWT with the given claims and key.
func signRSAToken(t *testing.T, key *rsa.PrivateKey, kid string, claims goJWT.Claims) string {
	t.Helper()
	token := goJWT.NewWithClaims(goJWT.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// ---------------------------------------------------------------------------
// RSA Validator tests
// ---------------------------------------------------------------------------

func TestValidator_ValidToken_ReturnsUser(t *testing.T) {
	key := generateRSAKey(t)
	kid := "test-kid-1"

	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{kid: &key.PublicKey}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0 // force refresh every time

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-42",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
		Email:    "alice@example.com",
		Name:     "Alice",
		TenantID: "tn-acme",
		Role:     "admin",
	}

	tokenStr := signRSAToken(t, key, kid, claims)
	got, err := v.Validate(tokenStr)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if got.Subject != "user-42" {
		t.Errorf("Subject = %q, want user-42", got.Subject)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", got.Email)
	}
	if got.Name != "Alice" {
		t.Errorf("Name = %q, want Alice", got.Name)
	}
	if got.TenantID != "tn-acme" {
		t.Errorf("TenantID = %q, want tn-acme", got.TenantID)
	}
	if got.Role != "admin" {
		t.Errorf("Role = %q, want admin", got.Role)
	}
}

func TestValidator_ExpiredToken_ReturnsError(t *testing.T) {
	key := generateRSAKey(t)
	kid := "test-kid-exp"

	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{kid: &key.PublicKey}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	past := time.Now().Add(-2 * time.Hour)
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(past),
			ExpiresAt: goJWT.NewNumericDate(past.Add(1 * time.Hour)), // expired 1h ago
		},
	}

	tokenStr := signRSAToken(t, key, kid, claims)
	_, err := v.Validate(tokenStr)
	if err == nil {
		t.Error("expired RSA token should be rejected")
	}
}

func TestValidator_WrongAudience_ReturnsError(t *testing.T) {
	key := generateRSAKey(t)
	kid := "test-kid-aud"

	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{kid: &key.PublicKey}))

	v := NewValidator(srv.URL, "test-issuer", "correct-audience")
	v.ttl = 0

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"wrong-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}

	tokenStr := signRSAToken(t, key, kid, claims)
	_, err := v.Validate(tokenStr)
	if err == nil {
		t.Error("token with wrong audience should be rejected")
	}
}

func TestValidator_WrongIssuer_ReturnsError(t *testing.T) {
	key := generateRSAKey(t)
	kid := "test-kid-iss"

	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{kid: &key.PublicKey}))

	v := NewValidator(srv.URL, "correct-issuer", "test-audience")
	v.ttl = 0

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "wrong-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}

	tokenStr := signRSAToken(t, key, kid, claims)
	_, err := v.Validate(tokenStr)
	if err == nil {
		t.Error("token with wrong issuer should be rejected")
	}
}

func TestValidator_InvalidSignature_ReturnsError(t *testing.T) {
	signingKey := generateRSAKey(t)
	differentKey := generateRSAKey(t)
	kid := "test-kid-sig"

	// JWKS serves differentKey's public key, but token is signed by signingKey
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{kid: &differentKey.PublicKey}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}

	tokenStr := signRSAToken(t, signingKey, kid, claims)
	_, err := v.Validate(tokenStr)
	if err == nil {
		t.Error("token signed with wrong key should be rejected")
	}
}

func TestValidator_MissingSubject_StillValid(t *testing.T) {
	// JWT spec does not require sub — Validator does not enforce it either.
	// The token is still valid; sub is just empty.
	key := generateRSAKey(t)
	kid := "test-kid-nosub"

	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{kid: &key.PublicKey}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			// Subject intentionally omitted
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}

	tokenStr := signRSAToken(t, key, kid, claims)
	got, err := v.Validate(tokenStr)
	if err != nil {
		t.Fatalf("token without sub should still be valid: %v", err)
	}
	if got.Subject != "" {
		t.Errorf("Subject = %q, want empty", got.Subject)
	}
}

func TestValidator_RotatedKey_AcceptsBothKids(t *testing.T) {
	oldKey := generateRSAKey(t)
	newKey := generateRSAKey(t)

	// JWKS serves BOTH keys
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{
		"old-kid": &oldKey.PublicKey,
		"new-kid": &newKey.PublicKey,
	}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	now := time.Now()

	// Token signed with old key
	oldClaims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-old",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}
	oldToken := signRSAToken(t, oldKey, "old-kid", oldClaims)

	// Token signed with new key
	newClaims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-new",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}
	newToken := signRSAToken(t, newKey, "new-kid", newClaims)

	// Both must be accepted
	gotOld, err := v.Validate(oldToken)
	if err != nil {
		t.Fatalf("old kid token rejected: %v", err)
	}
	if gotOld.Subject != "user-old" {
		t.Errorf("old token Subject = %q, want user-old", gotOld.Subject)
	}

	gotNew, err := v.Validate(newToken)
	if err != nil {
		t.Fatalf("new kid token rejected: %v", err)
	}
	if gotNew.Subject != "user-new" {
		t.Errorf("new token Subject = %q, want user-new", gotNew.Subject)
	}
}

func TestValidator_UnknownKid_ReturnsError(t *testing.T) {
	key := generateRSAKey(t)

	// JWKS serves a key with kid "known-kid"
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{
		"known-kid": &key.PublicKey,
	}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}

	// Sign with known key but use unknown kid header
	tokenStr := signRSAToken(t, key, "unknown-kid", claims)
	_, err := v.Validate(tokenStr)
	if err == nil {
		t.Error("token with unknown kid should be rejected")
	}
}

func TestValidator_NonRSASigningMethod_ReturnsError(t *testing.T) {
	// Sign with HMAC — Validator only accepts RSA
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}
	token := goJWT.NewWithClaims(goJWT.SigningMethodHS256, claims)
	token.Header["kid"] = "some-kid"
	signed, err := token.SignedString([]byte("some-secret-32-chars-minimum!!!!"))
	if err != nil {
		t.Fatalf("sign HMAC token: %v", err)
	}

	key := generateRSAKey(t)
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{"some-kid": &key.PublicKey}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	_, err = v.Validate(signed)
	if err == nil {
		t.Error("HMAC-signed token should be rejected by RSA validator")
	}
}

func TestValidator_JWKSFetchError_ReturnsError(t *testing.T) {
	// JWKS server returns 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	key := generateRSAKey(t)
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}
	tokenStr := signRSAToken(t, key, "any-kid", claims)

	_, err := v.Validate(tokenStr)
	if err == nil {
		t.Error("should fail when JWKS server returns 500")
	}
}

func TestValidator_MissingExpirationRequired(t *testing.T) {
	key := generateRSAKey(t)
	kid := "test-kid-noexp"

	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{kid: &key.PublicKey}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	// Token without expiration
	claims := goJWT.MapClaims{
		"sub": "user-1",
		"iss": "test-issuer",
		"aud": []string{"test-audience"},
		"iat": time.Now().Unix(),
		// no "exp"
	}
	token := goJWT.NewWithClaims(goJWT.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	_, err = v.Validate(signed)
	if err == nil {
		t.Error("token without expiration should be rejected (WithExpirationRequired)")
	}
}

func TestValidator_ExtClaims_Populated(t *testing.T) {
	key := generateRSAKey(t)
	kid := "test-kid-ext"

	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{kid: &key.PublicKey}))

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0

	now := time.Now()
	// Use MapClaims to include nested ext
	claims := goJWT.MapClaims{
		"sub": "user-ext",
		"iss": "test-issuer",
		"aud": []string{"test-audience"},
		"iat": now.Unix(),
		"exp": now.Add(1 * time.Hour).Unix(),
		"ext": map[string]interface{}{
			"email":     "ext@example.com",
			"name":      "Ext User",
			"tenant_id": "tn-ext",
		},
	}
	token := goJWT.NewWithClaims(goJWT.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	got, err := v.Validate(signed)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if got.Ext.Email != "ext@example.com" {
		t.Errorf("Ext.Email = %q, want ext@example.com", got.Ext.Email)
	}
	if got.Ext.Name != "Ext User" {
		t.Errorf("Ext.Name = %q, want Ext User", got.Ext.Name)
	}
	if got.Ext.TenantID != "tn-ext" {
		t.Errorf("Ext.TenantID = %q, want tn-ext", got.Ext.TenantID)
	}
}

func TestValidator_JWKSCaching_DoesNotRefetchWithinTTL(t *testing.T) {
	key := generateRSAKey(t)
	kid := "test-kid-cache"

	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON(map[string]*rsa.PublicKey{kid: &key.PublicKey}))
	}))
	t.Cleanup(srv.Close)

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 10 * time.Minute // long TTL

	now := time.Now()
	makeClaims := func(sub string) *Claims {
		return &Claims{
			RegisteredClaims: goJWT.RegisteredClaims{
				Subject:   sub,
				Issuer:    "test-issuer",
				Audience:  goJWT.ClaimStrings{"test-audience"},
				IssuedAt:  goJWT.NewNumericDate(now),
				ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
			},
		}
	}

	// First validation triggers JWKS fetch
	token1 := signRSAToken(t, key, kid, makeClaims("user-1"))
	if _, err := v.Validate(token1); err != nil {
		t.Fatalf("first validation failed: %v", err)
	}

	firstFetch := fetchCount

	// Second validation should use cache
	token2 := signRSAToken(t, key, kid, makeClaims("user-2"))
	if _, err := v.Validate(token2); err != nil {
		t.Fatalf("second validation failed: %v", err)
	}

	if fetchCount > firstFetch {
		t.Errorf("JWKS fetched %d times, expected cache hit on second call", fetchCount)
	}
}

func TestValidator_JWKSRefreshOnUnknownKid(t *testing.T) {
	key1 := generateRSAKey(t)
	key2 := generateRSAKey(t)

	// Server initially only knows kid1, then learns kid2 after refresh
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		keys := map[string]*rsa.PublicKey{"kid-1": &key1.PublicKey}
		if callCount > 1 {
			keys["kid-2"] = &key2.PublicKey
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON(keys))
	}))
	t.Cleanup(srv.Close)

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	v.ttl = 0 // force refresh

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-rotated",
			Issuer:    "test-issuer",
			Audience:  goJWT.ClaimStrings{"test-audience"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}

	// Token signed with key2/kid-2 — first fetch won't have it, refresh will
	token := signRSAToken(t, key2, "kid-2", claims)
	got, err := v.Validate(token)
	if err != nil {
		t.Fatalf("should succeed after JWKS refresh: %v", err)
	}
	if got.Subject != "user-rotated" {
		t.Errorf("Subject = %q, want user-rotated", got.Subject)
	}
}

func TestParseRSAPublicKey_ValidInput(t *testing.T) {
	key := generateRSAKey(t)
	nStr := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	eStr := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())

	pub, err := parseRSAPublicKey(nStr, eStr)
	if err != nil {
		t.Fatalf("parseRSAPublicKey failed: %v", err)
	}
	if pub.N.Cmp(key.N) != 0 {
		t.Error("N mismatch")
	}
	if pub.E != key.E {
		t.Errorf("E = %d, want %d", pub.E, key.E)
	}
}

func TestParseRSAPublicKey_InvalidN(t *testing.T) {
	_, err := parseRSAPublicKey("!!!invalid-base64!!!", "AQAB")
	if err == nil {
		t.Error("invalid N should return error")
	}
}

func TestParseRSAPublicKey_InvalidE(t *testing.T) {
	key := generateRSAKey(t)
	nStr := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	_, err := parseRSAPublicKey(nStr, "!!!invalid!!!")
	if err == nil {
		t.Error("invalid E should return error")
	}
}

func TestComputeKID_Deterministic(t *testing.T) {
	secret := []byte("my-secret-key")
	kid1 := computeKID(secret)
	kid2 := computeKID(secret)
	if kid1 != kid2 {
		t.Errorf("computeKID not deterministic: %q != %q", kid1, kid2)
	}
	if len(kid1) != 8 {
		t.Errorf("kid length = %d, want 8", len(kid1))
	}
}

func TestComputeKID_DifferentSecrets_DifferentKIDs(t *testing.T) {
	kid1 := computeKID([]byte("secret-a"))
	kid2 := computeKID([]byte("secret-b"))
	if kid1 == kid2 {
		t.Error("different secrets should produce different KIDs")
	}
}

// ---------------------------------------------------------------------------
// Session token tests (existing + extensions)
// ---------------------------------------------------------------------------

func TestSessionTokenIssuer_IssueAndValidate(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)

	token, expiresIn, err := issuer.Issue("user-123", "test@test.com", "Test User", "tn-acme")
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if expiresIn != 3600 {
		t.Errorf("expiresIn = %d, want 3600", expiresIn)
	}

	claims, err := issuer.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("Subject = %q, want user-123", claims.Subject)
	}
	if claims.Email != "test@test.com" {
		t.Errorf("Email = %q, want test@test.com", claims.Email)
	}
	if claims.Name != "Test User" {
		t.Errorf("Name = %q, want Test User", claims.Name)
	}
	if claims.TenantID != "tn-acme" {
		t.Errorf("TenantID = %q, want tn-acme", claims.TenantID)
	}
}

func TestSessionTokenIssuer_WrongSecret(t *testing.T) {
	issuerA := NewSessionTokenIssuer("secret-aaaaaaaaaaaaaaaaaaaaaaaaa", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	issuerB := NewSessionTokenIssuer("secret-bbbbbbbbbbbbbbbbbbbbbbbbb", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)

	token, _, _ := issuerA.Issue("user-1", "a@test.com", "A", "tn")

	_, err := issuerB.ValidateSession(token)
	if err == nil {
		t.Error("token signed with wrong secret should be rejected")
	}
}

func TestSessionTokenIssuer_ExpiredToken(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", -1*time.Second)

	token, _, err := issuer.Issue("user-1", "a@test.com", "A", "tn")
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	_, err = issuer.ValidateSession(token)
	if err == nil {
		t.Error("expired token should be rejected")
	}
}

func TestSessionTokenIssuer_WrongIssuer(t *testing.T) {
	issuerA := NewSessionTokenIssuer("same-secret-32-chars-minimum!!!!", "issuer-a", "loopzero-cloud", 1*time.Hour)
	issuerB := NewSessionTokenIssuer("same-secret-32-chars-minimum!!!!", "issuer-b", "loopzero-cloud", 1*time.Hour)

	token, _, _ := issuerA.Issue("user-1", "a@test.com", "A", "tn")

	_, err := issuerB.ValidateSession(token)
	if err == nil {
		t.Error("token with wrong issuer should be rejected")
	}
}

func TestSessionTokenIssuer_ValidatesAudience(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)

	token, _, _ := issuer.Issue("user-1", "a@test.com", "A", "tn")

	claims, err := issuer.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}

	if len(claims.Audience) == 0 || claims.Audience[0] != "loopzero-cloud" {
		t.Errorf("Audience = %v, want [loopzero-cloud]", claims.Audience)
	}
}

// B3 test: cross-audience token MUST be rejected.
func TestSessionTokenIssuer_CrossAudienceRejected(t *testing.T) {
	cloudIssuer := NewSessionTokenIssuer("shared-secret-32-chars-minimum!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	engineValidator := NewSessionTokenIssuer("shared-secret-32-chars-minimum!!", "loopzero-cloud", "loopzero-engine", 1*time.Hour)

	token, _, _ := cloudIssuer.Issue("user-1", "a@test.com", "A", "tn-acme")

	_, err := engineValidator.ValidateSession(token)
	if err == nil {
		t.Error("token with audience 'loopzero-cloud' MUST be rejected by validator expecting 'loopzero-engine'")
	}
}

// B4 test: token with no audience must be rejected.
func TestSessionTokenIssuer_NoAudienceRejected(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "", 1*time.Hour)
	token, _, _ := issuer.Issue("user-1", "a@test.com", "A", "tn")

	validator := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	_, err := validator.ValidateSession(token)
	if err == nil {
		t.Error("token with empty audience MUST be rejected by validator expecting 'loopzero-cloud'")
	}
}

// §2.2: Token signed with old secret is accepted during rotation window.
func TestSessionTokenIssuer_DualKeyRotation_OldSecretAccepted(t *testing.T) {
	oldSecret := "old-secret-32-chars-minimum!!!!!"
	newSecret := "new-secret-32-chars-minimum!!!!!"

	oldIssuer := NewSessionTokenIssuer(oldSecret, "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	token, _, _ := oldIssuer.Issue("user-1", "a@test.com", "A", "tn-acme")

	newValidator := NewSessionTokenIssuer(newSecret, "loopzero-cloud", "loopzero-cloud", 1*time.Hour).
		WithPreviousSecret(oldSecret)

	claims, err := newValidator.ValidateSession(token)
	if err != nil {
		t.Fatalf("token signed with old secret should be accepted during rotation: %v", err)
	}
	if claims.TenantID != "tn-acme" {
		t.Errorf("TenantID = %q, want tn-acme", claims.TenantID)
	}
}

// §2.2: Token signed with unknown secret is rejected.
func TestSessionTokenIssuer_DualKeyRotation_UnknownSecretRejected(t *testing.T) {
	unknownSecret := "unknown-secret-32-chars-minimum!"
	currentSecret := "current-secret-32-chars-minimum!"
	previousSecret := "previous-secret-32-chars-minimum"

	unknownIssuer := NewSessionTokenIssuer(unknownSecret, "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	token, _, _ := unknownIssuer.Issue("user-1", "a@test.com", "A", "tn")

	validator := NewSessionTokenIssuer(currentSecret, "loopzero-cloud", "loopzero-cloud", 1*time.Hour).
		WithPreviousSecret(previousSecret)

	_, err := validator.ValidateSession(token)
	if err == nil {
		t.Error("token signed with unknown secret MUST be rejected")
	}
}

// §2.2: Issued tokens include kid header.
func TestSessionTokenIssuer_KIDHeader(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	token, _, _ := issuer.Issue("user-1", "a@test.com", "A", "tn")

	parser := goJWT.NewParser()
	parsed, _, err := parser.ParseUnverified(token, &SessionClaims{})
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}
	kid, ok := parsed.Header["kid"].(string)
	if !ok || kid == "" {
		t.Error("issued token must include kid header")
	}
	if len(kid) != 8 {
		t.Errorf("kid length = %d, want 8 hex chars", len(kid))
	}
}

func TestSessionTokenIssuer_WithPreviousSecret_EmptyStringIgnored(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour).
		WithPreviousSecret("")

	if issuer.previousSecret != nil {
		t.Error("empty previousSecret should remain nil")
	}
}

func TestSessionTokenIssuer_ValidateSession_NonHMACRejected(t *testing.T) {
	// Create an RSA-signed token and try to validate with session (HMAC) validator
	key := generateRSAKey(t)
	now := time.Now()
	claims := &SessionClaims{
		RegisteredClaims: goJWT.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "loopzero-cloud",
			Audience:  goJWT.ClaimStrings{"loopzero-cloud"},
			IssuedAt:  goJWT.NewNumericDate(now),
			ExpiresAt: goJWT.NewNumericDate(now.Add(1 * time.Hour)),
		},
	}
	token := goJWT.NewWithClaims(goJWT.SigningMethodRS256, claims)
	token.Header["kid"] = "rsa-kid"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign RSA token: %v", err)
	}

	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	_, err = issuer.ValidateSession(signed)
	if err == nil {
		t.Error("RSA-signed token should be rejected by HMAC session validator")
	}
}

func TestValidator_MalformedToken_ReturnsError(t *testing.T) {
	key := generateRSAKey(t)
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{"kid": &key.PublicKey}))
	v := NewValidator(srv.URL, "test-issuer", "test-audience")

	_, err := v.Validate("not.a.valid.jwt.token")
	if err == nil {
		t.Error("malformed token should be rejected")
	}
}

func TestValidator_EmptyToken_ReturnsError(t *testing.T) {
	key := generateRSAKey(t)
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{"kid": &key.PublicKey}))
	v := NewValidator(srv.URL, "test-issuer", "test-audience")

	_, err := v.Validate("")
	if err == nil {
		t.Error("empty token should be rejected")
	}
}

func TestNewValidator_DefaultFields(t *testing.T) {
	v := NewValidator("https://example.com/.well-known/jwks.json", "iss", "aud")
	if v.jwksURL != "https://example.com/.well-known/jwks.json" {
		t.Errorf("jwksURL = %q", v.jwksURL)
	}
	if v.issuer != "iss" {
		t.Errorf("issuer = %q", v.issuer)
	}
	if v.audience != "aud" {
		t.Errorf("audience = %q", v.audience)
	}
	if v.ttl != 5*time.Minute {
		t.Errorf("ttl = %v, want 5m", v.ttl)
	}
	if v.keys == nil {
		t.Error("keys map should be initialized")
	}
}

func TestFetchJWKS_InvalidJSON_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, "not json")
	}))
	t.Cleanup(srv.Close)

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	err := v.fetchJWKS()
	if err == nil {
		t.Error("invalid JSON should return error")
	}
}

func TestFetchJWKS_SkipsNonRSAKeys(t *testing.T) {
	// Return a JWKS with a non-RSA key type
	payload := `{"keys":[{"kid":"ec-key","kty":"EC","use":"sig","crv":"P-256","x":"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU","y":"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, payload)
	}))
	t.Cleanup(srv.Close)

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	err := v.fetchJWKS()
	if err != nil {
		t.Fatalf("fetchJWKS failed: %v", err)
	}
	if len(v.keys) != 0 {
		t.Errorf("expected 0 RSA keys, got %d", len(v.keys))
	}
}

// ---------------------------------------------------------------------------
// T1.2 — RFC 8725bis hardening per ADR-0029.
// ---------------------------------------------------------------------------

// TestValidate_RejectsNoneAlgorithm covers the classic "none" bypass.
// An attacker submits a token with alg=none and no signature; the
// validator MUST reject it because RS256 is the only allowed access alg.
func TestValidate_RejectsNoneAlgorithm(t *testing.T) {
	key := generateRSAKey(t)
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{"k1": &key.PublicKey}))

	// Build an unsigned JWT manually: header.payload.''
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT","kid":"k1"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"test-issuer","aud":"test-audience","exp":99999999999}`))
	tokenStr := header + "." + payload + "."

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	if _, err := v.Validate(tokenStr); err == nil {
		t.Fatal("expected validator to reject alg=none, got nil error")
	}
}

// TestValidate_RejectsHS256WithPublicKeyAsSecret covers the algorithm
// confusion attack: attacker signs with HS256 using the RS256 public key
// as the HMAC secret. The validator MUST reject because access tokens
// are restricted to RS256.
func TestValidate_RejectsHS256WithPublicKeyAsSecret(t *testing.T) {
	key := generateRSAKey(t)
	pubKeyBytes := jwksJSON(map[string]*rsa.PublicKey{"k1": &key.PublicKey})
	srv := serveJWKS(t, pubKeyBytes)

	// Sign HS256 with the JWKS payload as the HMAC secret. Real attacks
	// use the public key bytes; using the JWKS payload is a stable proxy.
	claims := goJWT.MapClaims{
		"iss": "test-issuer",
		"aud": "test-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	tk := goJWT.NewWithClaims(goJWT.SigningMethodHS256, claims)
	tk.Header["kid"] = "k1"
	signed, err := tk.SignedString(pubKeyBytes)
	if err != nil {
		t.Fatalf("sign HS256: %v", err)
	}

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	if _, err := v.Validate(signed); err == nil {
		t.Fatal("expected validator to reject HS256 access token, got nil error")
	}
}

// TestValidate_RejectsUnexpectedTypHeader covers cross-JWT confusion
// (RFC 8725bis §2.6). A token with typ="evil" MUST be rejected.
func TestValidate_RejectsUnexpectedTypHeader(t *testing.T) {
	key := generateRSAKey(t)
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{"k1": &key.PublicKey}))

	tk := goJWT.NewWithClaims(goJWT.SigningMethodRS256, goJWT.MapClaims{
		"iss": "test-issuer",
		"aud": "test-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tk.Header["kid"] = "k1"
	tk.Header["typ"] = "evil"
	signed, err := tk.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	if _, err := v.Validate(signed); err == nil {
		t.Fatal("expected validator to reject typ=evil, got nil error")
	}
}

// TestValidate_AcceptsCanonicalTypValues confirms backward compat —
// "JWT" and "at+jwt" remain accepted.
func TestValidate_AcceptsCanonicalTypValues(t *testing.T) {
	for _, typ := range []string{"JWT", "at+jwt"} {
		t.Run(typ, func(t *testing.T) {
			key := generateRSAKey(t)
			srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{"k1": &key.PublicKey}))

			tk := goJWT.NewWithClaims(goJWT.SigningMethodRS256, goJWT.MapClaims{
				"iss": "test-issuer",
				"aud": "test-audience",
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			tk.Header["kid"] = "k1"
			tk.Header["typ"] = typ
			signed, err := tk.SignedString(key)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}

			v := NewValidator(srv.URL, "test-issuer", "test-audience")
			if _, err := v.Validate(signed); err != nil {
				t.Errorf("expected typ=%q to be accepted, got error: %v", typ, err)
			}
		})
	}
}

// TestValidate_AcceptsTokenWithoutTypHeader covers backward compat —
// RFC 7519 doesn't require typ; absent header MUST be accepted.
func TestValidate_AcceptsTokenWithoutTypHeader(t *testing.T) {
	key := generateRSAKey(t)
	srv := serveJWKS(t, jwksJSON(map[string]*rsa.PublicKey{"k1": &key.PublicKey}))

	tk := goJWT.NewWithClaims(goJWT.SigningMethodRS256, goJWT.MapClaims{
		"iss": "test-issuer",
		"aud": "test-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tk.Header["kid"] = "k1"
	delete(tk.Header, "typ")
	signed, err := tk.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	v := NewValidator(srv.URL, "test-issuer", "test-audience")
	if _, err := v.Validate(signed); err != nil {
		t.Errorf("expected token without typ to be accepted, got: %v", err)
	}
}

// TestValidateSession_RejectsRS256Token confirms session validator
// (HS256-only) rejects access tokens (RS256). Defense against
// substitution attack.
func TestValidateSession_RejectsRS256Token(t *testing.T) {
	rsaKey := generateRSAKey(t)
	tk := goJWT.NewWithClaims(goJWT.SigningMethodRS256, goJWT.MapClaims{
		"sub": "user-1",
		"iss": "test-issuer",
		"aud": "test-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tk.SignedString(rsaKey)
	if err != nil {
		t.Fatalf("sign RS256: %v", err)
	}

	issuer := NewSessionTokenIssuer("hmac-secret-32-bytes-padding-here", "test-issuer", "test-audience", time.Hour)
	if _, err := issuer.ValidateSession(signed); err == nil {
		t.Fatal("expected session validator to reject RS256 token, got nil error")
	}
}
