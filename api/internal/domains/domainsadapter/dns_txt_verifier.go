// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domainsadapter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/domains"
	"github.com/getloopscale/loop-runtime/api/internal/domains/domainshttp"
)

// txtResolver is the one DNS call DNSTXTVerifier makes; *net.Resolver has it.
type txtResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// DNSTXTVerifier implements domains.DomainVerifier with a DNS TXT lookup of
// _loopscale-verify.{domain}. A nil Resolver uses net.DefaultResolver.
type DNSTXTVerifier struct {
	Resolver txtResolver
}

// VerifyTXT reports whether the expected token is published at
// _loopscale-verify.{domain}. A record that does not exist is (false, nil); any
// other lookup failure is an error.
func (v *DNSTXTVerifier) VerifyTXT(ctx context.Context, domain, expectedToken string) (bool, error) {
	resolver := v.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	txtName := domains.VerificationTXTName(domain)
	records, err := resolver.LookupTXT(ctx, txtName)
	if err != nil {
		if isDNSNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("DNS lookup for %s: %w", txtName, err)
	}
	for _, record := range records {
		if record == expectedToken {
			return true, nil
		}
	}
	return false, nil
}

// isDNSNotFound reports whether a lookup error means the record does not exist.
func isDNSNotFound(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsNotFound
	}
	return strings.Contains(err.Error(), "no such host")
}

var _ domainshttp.DomainVerifier = (*DNSTXTVerifier)(nil)
