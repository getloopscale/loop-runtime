// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package objectstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	miniocreds "github.com/minio/minio-go/v7/pkg/credentials"
)

// fakeAWS answers STS AssumeRoleWithWebIdentity and S3 PutObject on one
// endpoint, the way a single AWS_ENDPOINT_URL does in front of an emulator.
type fakeAWS struct {
	webIdentityToken string // the token STS received
	roleARN          string
	putAuth          string // Authorization of the S3 PUT
	putSessionToken  string // X-Amz-Security-Token of the S3 PUT
	putPath          string
}

func (f *fakeAWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		if r.Form.Get("Action") != "AssumeRoleWithWebIdentity" {
			http.Error(w, "unexpected STS action "+r.Form.Get("Action"), http.StatusBadRequest)
			return
		}
		f.webIdentityToken = r.Form.Get("WebIdentityToken")
		f.roleARN = r.Form.Get("RoleArn")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<AssumeRoleWithWebIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <AssumeRoleWithWebIdentityResult>
    <Credentials>
      <AccessKeyId>ASIAWEBIDENTITY</AccessKeyId>
      <SecretAccessKey>web-identity-secret</SecretAccessKey>
      <SessionToken>web-identity-session-token</SessionToken>
      <Expiration>2099-01-01T00:00:00Z</Expiration>
    </Credentials>
  </AssumeRoleWithWebIdentityResult>
</AssumeRoleWithWebIdentityResponse>`)
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	f.putAuth = r.Header.Get("Authorization")
	f.putSessionToken = r.Header.Get("X-Amz-Security-Token")
	f.putPath = r.URL.EscapedPath()
	w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
	w.WriteHeader(http.StatusOK)
}

// clearAWSEnv isolates a test from the AWS variables of the machine running it.
func clearAWSEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_ACCESS_KEY", "AWS_SECRET_KEY",
		"AWS_PROFILE", "AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_SESSION_NAME",
		"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3", "AWS_ENDPOINT_URL_STS",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "no-credentials"))
}

func TestAWSStoreSignsWithTheWebIdentityCredentialsWhenNoStaticKeyIsConfigured(t *testing.T) {
	// Arrange: an IRSA pod. No access key anywhere, only the role and its
	// projected token, and one endpoint for every AWS service.
	clearAWSEnv(t)
	fake := &fakeAWS{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("projected-sa-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_ROLE_ARN", "arn:aws:iam::000000000000:role/loop-runtime-staging-core-loop-runtime-api")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", tokenFile)
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ENDPOINT_URL", srv.URL)

	store, err := NewAWSS3Store("")
	if err != nil {
		t.Fatalf("NewAWSS3Store: %v", err)
	}

	// Act
	err = store.Upload(context.Background(), "loop-runtime-staging-builds-000000000000", "tn-1/source/a.bundle", strings.NewReader("payload"))

	// Assert
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if fake.webIdentityToken != "projected-sa-token" || fake.roleARN != "arn:aws:iam::000000000000:role/loop-runtime-staging-core-loop-runtime-api" {
		t.Errorf("STS got token %q role %q, want the projected token and AWS_ROLE_ARN", fake.webIdentityToken, fake.roleARN)
	}
	if !strings.HasPrefix(fake.putAuth, "AWS4-HMAC-SHA256 Credential=ASIAWEBIDENTITY/") || !strings.Contains(fake.putAuth, "/us-east-1/s3/aws4_request") {
		t.Errorf("Authorization = %q, want a SigV4 us-east-1 signature by the web identity key", fake.putAuth)
	}
	if fake.putSessionToken != "web-identity-session-token" {
		t.Errorf("X-Amz-Security-Token = %q, want the web identity session token", fake.putSessionToken)
	}
	if fake.putPath != "/loop-runtime-staging-builds-000000000000/tn-1/source/a.bundle" {
		t.Errorf("path = %q, want a path-style PUT to the endpoint", fake.putPath)
	}
}

// stubProvider is a credential source that either has a key or fails.
type stubProvider struct {
	value miniocreds.Value
	err   error
}

func (p *stubProvider) Retrieve() (miniocreds.Value, error) { return p.value, p.err }
func (p *stubProvider) RetrieveWithCredContext(*miniocreds.CredContext) (miniocreds.Value, error) {
	return p.value, p.err
}
func (p *stubProvider) IsExpired() bool { return false }

func TestCredentialChainFailsLoudlyWhenNoSourceHasCredentials(t *testing.T) {
	// minio-go's own chain falls back to anonymous (unsigned) requests; this
	// one must refuse instead, naming why each source failed.
	chain := &requiredChain{providers: []miniocreds.Provider{
		&stubProvider{err: errors.New("no env keys")},
		&stubProvider{err: errors.New("no web identity")},
	}}

	_, err := chain.Retrieve()

	if err == nil || !strings.Contains(err.Error(), "no env keys") || !strings.Contains(err.Error(), "no web identity") {
		t.Fatalf("err = %v, want an error naming every source", err)
	}
}

func TestCredentialChainUsesTheFirstSourceWithAKey(t *testing.T) {
	chain := &requiredChain{providers: []miniocreds.Provider{
		&stubProvider{err: errors.New("no env keys")},
		&stubProvider{value: miniocreds.Value{AccessKeyID: "ASIA2", SecretAccessKey: "s"}},
		&stubProvider{value: miniocreds.Value{AccessKeyID: "ASIA3", SecretAccessKey: "s"}},
	}}

	v, err := chain.Retrieve()

	if err != nil || v.AccessKeyID != "ASIA2" {
		t.Fatalf("Retrieve = %+v, %v; want the second source's key", v, err)
	}
}

func TestAWSS3EndpointResolution(t *testing.T) {
	cases := []struct {
		name                      string
		explicit, s3, any, region string
		want                      string
	}{
		{name: "explicit endpoint wins", explicit: "https://minio.example:9000", s3: "https://s3.emu", any: "https://emu", want: "https://minio.example:9000"},
		{name: "service endpoint over the global one", s3: "https://s3.emu", any: "https://emu", want: "https://s3.emu"},
		{name: "global endpoint", any: "https://emu:9597", want: "https://emu:9597"},
		{name: "regional AWS endpoint", region: "eu-west-1", want: "https://s3.eu-west-1.amazonaws.com"},
		{name: "us-east-1 when no region is set", want: "https://s3.us-east-1.amazonaws.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearAWSEnv(t)
			t.Setenv("AWS_ENDPOINT_URL_S3", tc.s3)
			t.Setenv("AWS_ENDPOINT_URL", tc.any)
			t.Setenv("AWS_REGION", tc.region)
			if got := awsS3Endpoint(tc.explicit, awsRegion()); got != tc.want {
				t.Errorf("awsS3Endpoint = %q, want %q", got, tc.want)
			}
		})
	}
}
