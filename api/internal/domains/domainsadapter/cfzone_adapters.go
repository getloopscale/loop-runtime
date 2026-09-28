// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainsadapter

import (
	"context"

	domainsvc "github.com/getloopscale/loopzero/api/internal/domains"
	"github.com/getloopscale/loopzero/api/internal/domains/cfzone"
	"github.com/getloopscale/loopzero/api/internal/domains/domainshttp"
)

// CFZoneAdapter bridges cfzone.Client → domains.CFZoneManager.
type CFZoneAdapter struct {
	Client *cfzone.Client
}

func (a *CFZoneAdapter) CreateZone(ctx context.Context, domain string) (*domainshttp.CFZoneResult, error) {
	zone, err := a.Client.CreateZone(ctx, domain)
	if err != nil {
		return nil, err
	}
	return &domainshttp.CFZoneResult{
		ZoneID:      zone.ID,
		Nameservers: zone.Nameservers,
	}, nil
}

func (a *CFZoneAdapter) ScanDNSRecords(ctx context.Context, zoneID string) error {
	return a.Client.ScanDNSRecords(ctx, zoneID)
}

func (a *CFZoneAdapter) ListDNSRecords(ctx context.Context, zoneID string) ([]domainshttp.CFDNSRecord, error) {
	records, err := a.Client.ListDNSRecords(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	result := make([]domainshttp.CFDNSRecord, len(records))
	for i, r := range records {
		result[i] = domainshttp.CFDNSRecord{
			ID:       r.ID,
			Type:     r.Type,
			Name:     r.Name,
			Content:  r.Content,
			TTL:      r.TTL,
			Priority: r.Priority,
			Proxied:  r.Proxied,
		}
	}
	return result, nil
}

func (a *CFZoneAdapter) CreateDNSRecord(ctx context.Context, zoneID string, record domainshttp.CFDNSRecord) (*domainshttp.CFDNSRecord, error) {
	rec, err := a.Client.CreateDNSRecord(ctx, zoneID, cfzone.DNSRecord{
		Type:     record.Type,
		Name:     record.Name,
		Content:  record.Content,
		TTL:      record.TTL,
		Priority: record.Priority,
		Proxied:  record.Proxied,
	})
	if err != nil {
		return nil, err
	}
	return &domainshttp.CFDNSRecord{
		ID:       rec.ID,
		Type:     rec.Type,
		Name:     rec.Name,
		Content:  rec.Content,
		TTL:      rec.TTL,
		Priority: rec.Priority,
		Proxied:  rec.Proxied,
	}, nil
}

func (a *CFZoneAdapter) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	return a.Client.DeleteDNSRecord(ctx, zoneID, recordID)
}

func (a *CFZoneAdapter) DeleteZone(ctx context.Context, zoneID string) error {
	return a.Client.DeleteZone(ctx, zoneID)
}

// CFZoneStatusAdapter bridges cfzone.Client → domainsvc.CFZoneStatusChecker.
type CFZoneStatusAdapter struct {
	Client *cfzone.Client
}

func (a *CFZoneStatusAdapter) GetZoneStatus(ctx context.Context, zoneID string) (string, error) {
	return a.Client.GetZoneStatus(ctx, zoneID)
}

func (a *CFZoneStatusAdapter) TriggerActivationCheck(ctx context.Context, zoneID string) error {
	return a.Client.TriggerActivationCheck(ctx, zoneID)
}

func (a *CFZoneStatusAdapter) DeleteZone(ctx context.Context, zoneID string) error {
	return a.Client.DeleteZone(ctx, zoneID)
}

// The ports these adapters satisfy, asserted at compile time.
var (
	_ domainshttp.CFZoneManager     = (*CFZoneAdapter)(nil)
	_ domainsvc.CFZoneStatusChecker = (*CFZoneStatusAdapter)(nil)
)
