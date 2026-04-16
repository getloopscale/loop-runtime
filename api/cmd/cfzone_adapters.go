// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/cfzone"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// cfZoneAdapter bridges cfzone.Client → routes.CFZoneManager.
type cfZoneAdapter struct {
	client *cfzone.Client
}

func (a *cfZoneAdapter) CreateZone(ctx context.Context, domain string) (*routes.CFZoneResult, error) {
	zone, err := a.client.CreateZone(ctx, domain)
	if err != nil {
		return nil, err
	}
	return &routes.CFZoneResult{
		ZoneID:      zone.ID,
		Nameservers: zone.Nameservers,
	}, nil
}

func (a *cfZoneAdapter) ScanDNSRecords(ctx context.Context, zoneID string) error {
	return a.client.ScanDNSRecords(ctx, zoneID)
}

func (a *cfZoneAdapter) ListDNSRecords(ctx context.Context, zoneID string) ([]routes.CFDNSRecord, error) {
	records, err := a.client.ListDNSRecords(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	result := make([]routes.CFDNSRecord, len(records))
	for i, r := range records {
		result[i] = routes.CFDNSRecord{
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

func (a *cfZoneAdapter) CreateDNSRecord(ctx context.Context, zoneID string, record routes.CFDNSRecord) (*routes.CFDNSRecord, error) {
	rec, err := a.client.CreateDNSRecord(ctx, zoneID, cfzone.DNSRecord{
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
	return &routes.CFDNSRecord{
		ID:       rec.ID,
		Type:     rec.Type,
		Name:     rec.Name,
		Content:  rec.Content,
		TTL:      rec.TTL,
		Priority: rec.Priority,
		Proxied:  rec.Proxied,
	}, nil
}

func (a *cfZoneAdapter) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	return a.client.DeleteDNSRecord(ctx, zoneID, recordID)
}

func (a *cfZoneAdapter) DeleteZone(ctx context.Context, zoneID string) error {
	return a.client.DeleteZone(ctx, zoneID)
}

// cfZoneStatusAdapter bridges cfzone.Client → services.CFZoneStatusChecker.
type cfZoneStatusAdapter struct {
	client *cfzone.Client
}

func (a *cfZoneStatusAdapter) GetZoneStatus(ctx context.Context, zoneID string) (string, error) {
	return a.client.GetZoneStatus(ctx, zoneID)
}

func (a *cfZoneStatusAdapter) TriggerActivationCheck(ctx context.Context, zoneID string) error {
	return a.client.TriggerActivationCheck(ctx, zoneID)
}

func (a *cfZoneStatusAdapter) DeleteZone(ctx context.Context, zoneID string) error {
	return a.client.DeleteZone(ctx, zoneID)
}
