// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Custom domain validation rules. MEET-2026-04-05-033 §6.4. The DNS TXT
// verifier that used to live here is infrastructure and moved to
// api/internal/adapters.

package ports

import (
	"fmt"
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

// domainRule is one independent check on a normalized (trimmed, lowercased)
// domain name. It returns nil when the name passes.
type domainRule func(v *Validator, d string) error

// domainRules run in order; the first failure is the one reported, so cheap
// syntactic checks come before the policy ones.
var domainRules = []domainRule{
	requirePresent,
	requireMaxLength,
	rejectTrailingDot,
	rejectProtocol,
	rejectWildcard,
	rejectPort,
	requireFQDN,
	rejectPunycode,
	rejectReserved,
	rejectPlatformZone,
	rejectBlockedSuffix,
}

// Validate checks if a domain name is valid for custom domain registration.
func (v *Validator) Validate(domainInput string) error {
	d := strings.TrimSpace(strings.ToLower(domainInput))
	for _, rule := range domainRules {
		if err := rule(v, d); err != nil {
			return err
		}
	}
	return nil
}

func requirePresent(_ *Validator, d string) error {
	if d == "" {
		return fmt.Errorf("domain name is required")
	}
	return nil
}

func requireMaxLength(_ *Validator, d string) error {
	if len(d) > 253 {
		return fmt.Errorf("domain name exceeds maximum length of 253 characters")
	}
	return nil
}

func rejectTrailingDot(_ *Validator, d string) error {
	if strings.HasSuffix(d, ".") {
		return fmt.Errorf("domain name must not end with a dot")
	}
	return nil
}

func rejectProtocol(_ *Validator, d string) error {
	if strings.Contains(d, "://") {
		return fmt.Errorf("domain name must not include a protocol (e.g., https://). Use just the hostname")
	}
	return nil
}

func rejectWildcard(_ *Validator, d string) error {
	if strings.Contains(d, "*") {
		return fmt.Errorf("wildcard domains are not supported. Use an exact hostname")
	}
	return nil
}

func rejectPort(_ *Validator, d string) error {
	if strings.Contains(d, ":") {
		return fmt.Errorf("domain name must not include a port number")
	}
	return nil
}

func requireFQDN(_ *Validator, d string) error {
	if !fqdnRegex.MatchString(d) {
		return fmt.Errorf("invalid domain format. Must be a valid hostname (e.g., api.mycompany.com)")
	}
	return nil
}

// rejectPunycode — SEC-008: punycode labels ("xn--") are structurally valid
// ASCII, so the FQDN regex accepts them; they are refused to prevent homograph
// attacks (xn--pi-8cd.mycompany.com renders as аpi.mycompany.com).
func rejectPunycode(_ *Validator, d string) error {
	for _, label := range strings.Split(d, ".") {
		if strings.HasPrefix(label, "xn--") {
			return fmt.Errorf("internationalized (punycode) domain names are not supported. Use ASCII domain names only")
		}
	}
	return nil
}

// rejectReserved refuses RFC 2606 and other reserved names.
func rejectReserved(_ *Validator, d string) error {
	for _, reserved := range reservedDomains {
		if d == reserved || strings.HasSuffix(d, "."+reserved) {
			return fmt.Errorf("'%s' is a reserved domain and cannot be registered", d)
		}
	}
	return nil
}

// rejectPlatformZone — SEC-001: blocks the platform domain, its parent zone,
// and every subdomain of both. With BaseDomain="dev.getloopscale.com" the blocked
// trees are dev.getloopscale.com and getloopscale.com.
func rejectPlatformZone(v *Validator, d string) error {
	if v.BaseDomain == "" {
		return nil
	}
	if d == v.BaseDomain || strings.HasSuffix(d, "."+v.BaseDomain) {
		return fmt.Errorf("cannot register a domain under the platform domain (%s)", v.BaseDomain)
	}
	if parts := strings.SplitN(v.BaseDomain, ".", 2); len(parts) == 2 {
		parentZone := parts[1]
		if d == parentZone || strings.HasSuffix(d, "."+parentZone) {
			return fmt.Errorf("cannot register a domain under the platform zone (%s)", parentZone)
		}
	}
	return nil
}

func rejectBlockedSuffix(v *Validator, d string) error {
	for _, suffix := range v.BlockedSuffixes {
		if d == suffix || strings.HasSuffix(d, "."+suffix) {
			return fmt.Errorf("domain '%s' is blocked by platform policy", d)
		}
	}
	return nil
}
