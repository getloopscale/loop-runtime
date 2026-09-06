// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package ports provides custom domain validation and DNS verification logic.
// MEET-2026-04-05-033 §6.4
package ports

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// fqdnRegex validates a fully qualified domain name.
var fqdnRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)

// reservedDomains cannot be registered (RFC 2606 + common).
var reservedDomains = []string{
	"localhost",
	"example.com",
	"example.net",
	"example.org",
	"test",
	"invalid",
	"local",
	"internal",
}

// DefaultInfraSubdomains are the standard platform service subdomains.
var DefaultInfraSubdomains = []string{
	"api", "argocd", "git", "grafana", "cloud", "auth", "monitoring", "cname",
}

// Validator validates custom domain names against platform rules.
type Validator struct {
	BaseDomain      string
	BlockedSuffixes []string
	InfraSubdomains []string
}

// NewValidator creates a Validator with standard defaults.
func NewValidator(baseDomain string, extraBlocked []string) *Validator {
	return &Validator{
		BaseDomain:      baseDomain,
		BlockedSuffixes: extraBlocked,
		InfraSubdomains: DefaultInfraSubdomains,
	}
}

// Validate checks if a domain name is valid for custom domain registration.
func (v *Validator) Validate(domainInput string) error {
	d := strings.TrimSpace(strings.ToLower(domainInput))

	if d == "" {
		return fmt.Errorf("domain name is required")
	}
	if len(d) > 253 {
		return fmt.Errorf("domain name exceeds maximum length of 253 characters")
	}
	if strings.HasSuffix(d, ".") {
		return fmt.Errorf("domain name must not end with a dot")
	}
	if strings.Contains(d, "://") {
		return fmt.Errorf("domain name must not include a protocol (e.g., https://). Use just the hostname")
	}
	if strings.Contains(d, "*") {
		return fmt.Errorf("wildcard domains are not supported. Use an exact hostname")
	}
	if strings.Contains(d, ":") {
		return fmt.Errorf("domain name must not include a port number")
	}
	if !fqdnRegex.MatchString(d) {
		return fmt.Errorf("invalid domain format. Must be a valid hostname (e.g., api.mycompany.com)")
	}

	// SEC-008 fix: reject punycode/IDN domains (homograph attack prevention).
	// Punycode labels start with "xn--" and are structurally valid ASCII, so the
	// FQDN regex accepts them. We reject them explicitly to prevent homograph attacks
	// (e.g., xn--pi-8cd.mycompany.com renders as аpi.mycompany.com in browsers).
	for _, label := range strings.Split(d, ".") {
		if strings.HasPrefix(label, "xn--") {
			return fmt.Errorf("internationalized (punycode) domain names are not supported. Use ASCII domain names only")
		}
	}

	// Reserved domains (RFC 2606)
	for _, reserved := range reservedDomains {
		if d == reserved || strings.HasSuffix(d, "."+reserved) {
			return fmt.Errorf("'%s' is a reserved domain and cannot be registered", d)
		}
	}

	// Platform domain protection — block the entire platform zone tree.
	// SEC-001 fix: blocks baseDomain, parent zone, AND all subdomains of both.
	// Example with BaseDomain="dev.loopzero.example":
	//   Blocked: dev.loopzero.example, *.dev.loopzero.example, loopzero.example, *.loopzero.example
	if v.BaseDomain != "" {
		if d == v.BaseDomain || strings.HasSuffix(d, "."+v.BaseDomain) {
			return fmt.Errorf("cannot register a domain under the platform domain (%s)", v.BaseDomain)
		}
		// Also block the parent zone and all its subdomains
		parts := strings.SplitN(v.BaseDomain, ".", 2)
		if len(parts) == 2 {
			parentZone := parts[1]
			if d == parentZone || strings.HasSuffix(d, "."+parentZone) {
				return fmt.Errorf("cannot register a domain under the platform zone (%s)", parentZone)
			}
		}
	}

	// Extra blocked suffixes
	for _, suffix := range v.BlockedSuffixes {
		if d == suffix || strings.HasSuffix(d, "."+suffix) {
			return fmt.Errorf("domain '%s' is blocked by platform policy", d)
		}
	}

	return nil
}

// DNSVerifier implements domains.DomainVerifier using net.LookupTXT.
type DNSVerifier struct{}

// VerifyTXT checks if the expected TXT record exists for _loopzero-verify.{domain}.
func (v *DNSVerifier) VerifyTXT(ctx context.Context, d, expectedToken string) (bool, error) {
	txtName := "_loopzero-verify." + d
	records, err := net.DefaultResolver.LookupTXT(ctx, txtName)
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

// isDNSNotFound checks if a DNS error indicates the record doesn't exist.
func isDNSNotFound(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsNotFound
	}
	return strings.Contains(err.Error(), "no such host")
}
