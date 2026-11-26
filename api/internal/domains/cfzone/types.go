// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package cfzone provides a client for the Cloudflare Zone Management API.
// Used by the custom domain feature to create per-domain DNS zones on the
// platform's Cloudflare account, enabling apex domain support with full
// WAF/DDoS protection via CF edge.
//
// Architecture (validated 2026-04-08 with aquim.ai):
//   - loop-runtime domain add empresa.com → CF API creates zone → returns nameservers
//   - User changes nameservers at registrar
//   - CF activates zone → CNAME @ → cname.dev.getloopscale.com (proxied, flattened)
//   - Traffic: CF edge (WAF/DDoS/TLS) → CF Tunnel → Traefik → Pod
package cfzone

// ZoneInfo represents a Cloudflare DNS zone.
type ZoneInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"` // pending, active, moved, deleted
	Nameservers []string `json:"name_servers"`
}

// DNSRecord represents a Cloudflare DNS record.
type DNSRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`               // A, AAAA, CNAME, MX, TXT, etc.
	Name     string `json:"name"`               // FQDN or @ for apex
	Content  string `json:"content"`            // Record value
	TTL      int    `json:"ttl,omitempty"`      // 1 = auto (required when proxied)
	Priority *int   `json:"priority,omitempty"` // MX only
	Proxied  *bool  `json:"proxied,omitempty"`  // CNAME/A only
}

// ScanResult contains DNS records found by CF's automatic scan.
type ScanResult struct {
	TotalRecords int         `json:"recs_added"`
	Records      []DNSRecord `json:"-"` // populated separately via list
}

// --- Cloudflare API response wrappers ---

type cfResponse[T any] struct {
	Success  bool      `json:"success"`
	Result   T         `json:"result"`
	Errors   []cfError `json:"errors"`
	Messages []struct {
		Message string `json:"message"`
	} `json:"messages"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfListResponse[T any] struct {
	Success    bool      `json:"success"`
	Result     []T       `json:"result"`
	Errors     []cfError `json:"errors"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
		TotalCount int `json:"total_count"`
	} `json:"result_info"`
}
