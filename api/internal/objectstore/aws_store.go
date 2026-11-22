// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package objectstore

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	miniocreds "github.com/minio/minio-go/v7/pkg/credentials"
)

// credentialsTimeout bounds one call to a credential source (STS, the
// container credentials endpoint, instance metadata), so a source that never
// answers cannot hang an upload.
const credentialsTimeout = 10 * time.Second

// NewAWSS3Store builds a store for Amazon S3 that holds no key of its own: it
// signs with the AWS credential chain, in the AWS SDK order (the AWS_ACCESS_KEY_ID
// environment pair, the shared credentials file, then the role the platform
// gives the pod: IRSA web identity, the container credentials endpoint, or
// instance metadata).
//
// endpoint may be empty. The S3 endpoint is then AWS_ENDPOINT_URL_S3, else
// AWS_ENDPOINT_URL, else https://s3.<region>.amazonaws.com, and the web identity
// exchange goes to AWS_ENDPOINT_URL_STS or AWS_ENDPOINT_URL when either is set,
// the same variables the AWS SDKs read. The signing region is AWS_REGION, else
// AWS_DEFAULT_REGION, else us-east-1.
func NewAWSS3Store(endpoint string) (*S3Store, error) {
	region := awsRegion()
	// Virtual-hosted style on amazonaws.com, path style on any other host (an
	// emulator or a gateway addressed by IP), as minio-go decides for "auto".
	return newS3Store(awsS3Endpoint(endpoint, region), region, minio.BucketLookupAuto, awsCredentialChain(region))
}

func awsRegion() string {
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return signingRegion
}

func awsS3Endpoint(explicit, region string) string {
	for _, v := range []string{explicit, os.Getenv("AWS_ENDPOINT_URL_S3"), os.Getenv("AWS_ENDPOINT_URL")} {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return "https://s3." + region + ".amazonaws.com"
}

func awsCredentialChain(region string) *miniocreds.Credentials {
	iam := &miniocreds.IAM{Region: region, Client: &http.Client{Timeout: credentialsTimeout}}
	// minio-go sends the web identity exchange to the regional STS endpoint and
	// does not read the AWS endpoint variables. Endpoint is also the instance
	// metadata address on other paths, so it is set only for web identity.
	if os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != "" {
		for _, k := range []string{"AWS_ENDPOINT_URL_STS", "AWS_ENDPOINT_URL"} {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				iam.Endpoint = v
				break
			}
		}
	}
	return miniocreds.New(&requiredChain{
		providers: []miniocreds.Provider{&miniocreds.EnvAWS{}, &miniocreds.FileAWSCredentials{}, iam},
	})
}

// requiredChain returns the first credentials a source has. minio-go's own
// chain falls back to anonymous, unsigned requests when every source fails;
// this one fails instead, naming why each source had nothing, so a pod
// without a role sees its cause and not an AccessDenied from S3.
type requiredChain struct {
	providers []miniocreds.Provider
	current   miniocreds.Provider
}

func (c *requiredChain) Retrieve() (miniocreds.Value, error) {
	return c.RetrieveWithCredContext(nil)
}

func (c *requiredChain) RetrieveWithCredContext(cc *miniocreds.CredContext) (miniocreds.Value, error) {
	var errs []error
	for _, p := range c.providers {
		v, err := p.RetrieveWithCredContext(cc)
		if err == nil && v.AccessKeyID != "" && v.SecretAccessKey != "" {
			c.current = p
			return v, nil
		}
		if err == nil {
			err = fmt.Errorf("%T: no credentials", p)
		}
		errs = append(errs, err)
	}
	c.current = nil
	return miniocreds.Value{}, fmt.Errorf("objectstore: no AWS credentials in the chain: %w", errors.Join(errs...))
}

func (c *requiredChain) IsExpired() bool {
	return c.current == nil || c.current.IsExpired()
}

// compile-time check: the chain is a minio-go credential provider.
var _ miniocreds.Provider = (*requiredChain)(nil)

// Endpoint is the URL the store sends requests to, for startup logs.
func (s *S3Store) Endpoint() string { return s.client.EndpointURL().String() }
