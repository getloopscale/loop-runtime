// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/middleware"
)

// createWithCFZone implements the v2 domain creation flow:
// 1. Create CF zone for the domain
// 2. Scan existing DNS records (email preservation)
// 3. Add CNAME @ → cname.dev.loopzero.example (proxied, CF flattening)
// 4. Return nameservers for user to configure at registrar
func (h *DomainHandler) createWithCFZone(w http.ResponseWriter, r *http.Request, ctx context.Context, uc *middleware.UserContext, req domainCreateRequest) {
	// Step 1: Create CF zone
	zone, err := h.cfZone.CreateZone(ctx, req.Domain)
	if err != nil {
		slog.ErrorContext(ctx, "cf zone create failed", "domain", req.Domain, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Failed to configure domain. Please try again.",
			"code":  "CF_ZONE_ERROR",
		})
		return
	}

	// Step 2: Scan existing DNS records (email, DKIM, etc.)
	if scanErr := h.cfZone.ScanDNSRecords(ctx, zone.ZoneID); scanErr != nil {
		slog.WarnContext(ctx, "cf dns scan failed (non-fatal)", "domain", req.Domain, "error", scanErr)
	}

	// Step 3: Remove conflicting apex records (A/AAAA/CNAME at root)
	// The DNS scan may import existing A records that conflict with our CNAME.
	if scannedRecords, listErr := h.cfZone.ListDNSRecords(ctx, zone.ZoneID); listErr == nil {
		for _, rec := range scannedRecords {
			// Only remove apex records that conflict with our CNAME
			if (rec.Type == "A" || rec.Type == "AAAA" || rec.Type == "CNAME") &&
				(rec.Name == req.Domain || rec.Name == "@") {
				slog.InfoContext(ctx, "cf removing conflicting apex record", "domain", req.Domain, "type", rec.Type, "content", rec.Content)
				_ = h.cfZone.DeleteDNSRecord(ctx, zone.ZoneID, rec.ID)
			}
		}
	}

	// Step 4: Add routing CNAME (proxied → CF edge → tunnel → Traefik)
	proxied := true
	_, cnameErr := h.cfZone.CreateDNSRecord(ctx, zone.ZoneID, CFDNSRecord{
		Type:    "CNAME",
		Name:    req.Domain,
		Content: h.cnameTarget,
		TTL:     1, // auto (required when proxied)
		Proxied: &proxied,
	})
	if cnameErr != nil {
		slog.ErrorContext(ctx, "cf cname create failed", "domain", req.Domain, "error", cnameErr)
		// Rollback: delete the zone
		_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Failed to configure domain routing. Please try again.",
			"code":  "CF_ZONE_ERROR",
		})
		return
	}

	// Step 5: List imported records, deduplicate (CF scan sometimes imports duplicates with different quoting)
	var dnsRecords []CFDNSRecord
	var recordCount int
	if records, listErr := h.cfZone.ListDNSRecords(ctx, zone.ZoneID); listErr == nil {
		seen := make(map[string]bool)
		for _, rec := range records {
			// Normalize: strip surrounding quotes for dedup key
			content := strings.Trim(rec.Content, "\"")
			key := rec.Type + ":" + rec.Name + ":" + content
			if seen[key] {
				// Duplicate — delete from CF zone
				slog.InfoContext(ctx, "cf removing duplicate record", "domain", req.Domain, "type", rec.Type, "name", rec.Name)
				_ = h.cfZone.DeleteDNSRecord(ctx, zone.ZoneID, rec.ID)
				continue
			}
			seen[key] = true
			dnsRecords = append(dnsRecords, rec)
		}
		recordCount = len(dnsRecords)
	}

	// Step 6: Save to DB
	zoneID := zone.ZoneID
	domain := &CustomDomain{
		ID:                 GenerateDomainID(),
		TenantID:           uc.TenantID,
		ProjectName:        req.Project,
		AppName:            req.App,
		Environment:        req.Environment,
		Domain:             req.Domain,
		Status:             "pending_ns",
		VerificationToken:  "", // not needed for v2
		CFZoneID:           &zoneID,
		CFZoneStatus:       "pending",
		CFNameservers:      zone.Nameservers,
		DNSRecordsImported: recordCount > 1, // >1 because CNAME always exists
	}

	if err := h.store.CreateWithCFZone(ctx, domain); err != nil {
		if errors.Is(err, ErrDomainExists) {
			// Rollback zone
			_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "domain already registered",
				"code":  "DOMAIN_ALREADY_EXISTS",
			})
			return
		}
		slog.ErrorContext(ctx, "domain create failed", "error", err)
		_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register domain"})
		return
	}

	metrics.CustomDomainsTotal.WithLabelValues(uc.TenantID, "pending_ns").Inc()

	writeJSON(w, http.StatusCreated, domainCreateV2Response{
		OK:                 true,
		ID:                 domain.ID,
		Domain:             domain.Domain,
		Project:            domain.ProjectName,
		App:                domain.AppName,
		Environment:        domain.Environment,
		Status:             domain.Status,
		Nameservers:        zone.Nameservers,
		DNSRecordsImported: recordCount,
		DNSRecords:         dnsRecords,
		ActionRequired:     "Change your nameservers at your DNS provider to activate this domain.",
	})
}
