// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domains

import "context"

// CFZoneResult contains the result of creating a CF zone.
type CFZoneResult struct {
	ZoneID      string
	Nameservers []string
}

// CFDNSRecord mirrors cfzone.DNSRecord for DIP.
type CFDNSRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,omitempty"`
	Priority *int   `json:"priority,omitempty"`
	Proxied  *bool  `json:"proxied,omitempty"`
}

// DomainVerifier checks DNS TXT records for domain ownership.
type DomainVerifier interface {
	VerifyTXT(ctx context.Context, domain, expectedToken string) (bool, error)
}

// DomainValidator validates domain names against platform rules.
type DomainValidator interface {
	Validate(domain string) error
}

// CFZoneManager abstracts CF Zone API operations (DIP -- testable without real CF).
type CFZoneManager interface {
	CreateZone(ctx context.Context, domain string) (*CFZoneResult, error)
	ScanDNSRecords(ctx context.Context, zoneID string) error
	ListDNSRecords(ctx context.Context, zoneID string) ([]CFDNSRecord, error)
	CreateDNSRecord(ctx context.Context, zoneID string, record CFDNSRecord) (*CFDNSRecord, error)
	DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error
	DeleteZone(ctx context.Context, zoneID string) error
}
