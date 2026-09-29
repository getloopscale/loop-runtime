// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// domain_wrapper.go — sqlc-backed implementation of
// PostgresDomainStore. T2.1 Batch A #9 of plan
// 2026-05-29-sqlc-migration-plan v1.3. FINAL Batch A
// store — consolidates the 3-file legacy split (domain_store.go +
// domain_store_background.go + domain_store_cfzone.go) into one
// wrapper that dispatches all 20+ methods through SqlcBridge.
//
// The 2 prod call sites (api/cmd/setup_stores.go:91,
// api/cmd/controller/setup_services.go:52) compile UNCHANGED —
// struct-literal `&db.PostgresDomainStore{DB: dbAdapter}` preserved
// (struct in domain_types.go).
//
// Invariants preserved:
//   - Create + CreateWithCFZone surface 23505 → ports.ErrDomainExists
//     via isPGUniqueViolation
//   - GetByDomainAndTenant NoRows → (nil, nil)
//   - ListByTenant dynamic filter via sqlc.narg() (replaces legacy
//     fmt.Sprintf + isParameterizedCondition gate; sqlc-emitted SQL
//     never has raw value interpolation by construction)
//   - Single-domain mutations (UpdateStatus, TransitionStatus,
//     UpdateVerified, UpdateTLS, UpdateFailure, Delete,
//     UpdateCNAMECheck, UpdateCFZoneStatus): rows-affected=0 →
//     "domain not found" error (legacy contract)
//   - TransitionStatus rows-affected=0 → typed message "transition
//     failed — domain X not in expected status Y"
//   - ClaimByStatus atomic UPDATE...WHERE IN (SELECT...FOR UPDATE
//     SKIP LOCKED) preserved verbatim (FIND-001 fix)
//   - FindPendingForAutoVerify cell-scoped via JOIN (roadmap 03
//     multi-cell isolation)

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/statemachine"

	"github.com/getloopscale/loopzero/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// =========================================================================
// User-facing CRUD
// =========================================================================

func (s *PostgresDomainStore) Create(ctx context.Context, d *ports.CustomDomain) error {
	bridge := s.bridge()
	q := generated.New()
	if err := q.InsertCustomDomain(ctx, bridge, generated.InsertCustomDomainParams{
		ID:                d.ID,
		TenantID:          d.TenantID,
		ProjectName:       d.ProjectName,
		AppName:           d.AppName,
		Environment:       d.Environment,
		Domain:            d.Domain,
		Status:            d.Status,
		VerificationToken: d.VerificationToken,
		TlsStatus:         d.TLSStatus,
	}); err != nil {
		if db.IsPGUniqueViolation(err) {
			return ports.ErrDomainExists
		}
		return fmt.Errorf("domain store: create: %w", err)
	}
	return nil
}

func (s *PostgresDomainStore) CreateWithCFZone(ctx context.Context, d *ports.CustomDomain) error {
	bridge := s.bridge()
	q := generated.New()
	if err := q.InsertCustomDomainWithCFZone(ctx, bridge, generated.InsertCustomDomainWithCFZoneParams{
		ID:                 d.ID,
		TenantID:           d.TenantID,
		ProjectName:        d.ProjectName,
		AppName:            d.AppName,
		Environment:        d.Environment,
		Domain:             d.Domain,
		Status:             d.Status,
		VerificationToken:  d.VerificationToken,
		TlsStatus:          d.TLSStatus,
		CfZoneID:           d.CFZoneID,
		CfZoneStatus:       dbconv.StringEmptyToNilPtr(d.CFZoneStatus),
		CfNameservers:      d.CFNameservers,
		DnsRecordsImported: d.DNSRecordsImported,
	}); err != nil {
		if db.IsPGUniqueViolation(err) {
			return ports.ErrDomainExists
		}
		return fmt.Errorf("domain store: create with cf zone: %w", err)
	}
	return nil
}

func (s *PostgresDomainStore) GetByDomainAndTenant(ctx context.Context, dom, tenantID string) (*ports.CustomDomain, error) {
	bridge := s.bridge()
	q := generated.New()
	row, err := q.GetCustomDomainByDomainAndTenant(ctx, bridge, generated.GetCustomDomainByDomainAndTenantParams{
		Domain:   dom,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("domain store: query: %w", err)
	}
	return customDomainFromGet(row), nil
}

func (s *PostgresDomainStore) ListByTenant(ctx context.Context, tenantID string, filter ports.DomainListFilter) ([]ports.CustomDomain, int, error) {
	bridge := s.bridge()
	q := generated.New()

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	projectArg := dbconv.StringEmptyToNilPtr(filter.Project)
	appArg := dbconv.StringEmptyToNilPtr(filter.App)
	envArg := dbconv.StringEmptyToNilPtr(filter.Environment)

	total, err := q.CountCustomDomainsByTenant(ctx, bridge, generated.CountCustomDomainsByTenantParams{
		TenantID:    tenantID,
		ProjectName: projectArg,
		AppName:     appArg,
		Environment: envArg,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("domain store: count: %w", err)
	}

	rows, err := q.ListCustomDomainsByTenant(ctx, bridge, generated.ListCustomDomainsByTenantParams{
		TenantID:    tenantID,
		ProjectName: projectArg,
		AppName:     appArg,
		Environment: envArg,
		Lim:         int32(limit),
		Off:         int32(filter.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("domain store: list: %w", err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromList(r))
	}
	return results, int(total), nil
}

func (s *PostgresDomainStore) UpdateStatus(ctx context.Context, dom, status string) error {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.UpdateCustomDomainStatus(ctx, bridge, generated.UpdateCustomDomainStatusParams{
		Status: status,
		Domain: dom,
	})
	if err != nil {
		return fmt.Errorf("domain store: update status: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("domain store: domain not found: %s", dom)
	}
	return nil
}

func (s *PostgresDomainStore) TransitionStatus(ctx context.Context, dom, expectedStatus, newStatus string) error {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.TransitionCustomDomainStatus(ctx, bridge, generated.TransitionCustomDomainStatusParams{
		Status:   newStatus,
		Domain:   dom,
		Status_2: expectedStatus,
	})
	if err != nil {
		return fmt.Errorf("domain store: transition %s→%s: %w", expectedStatus, newStatus, err)
	}
	if rows == 0 {
		return fmt.Errorf("domain store: transition failed — domain %s not in expected status %s", dom, expectedStatus)
	}
	return nil
}

func (s *PostgresDomainStore) UpdateVerified(ctx context.Context, dom string, verifiedAt time.Time) error {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.UpdateCustomDomainVerified(ctx, bridge, generated.UpdateCustomDomainVerifiedParams{
		VerifiedAt: pgtype.Timestamptz{Time: verifiedAt, Valid: true},
		Domain:     dom,
	})
	if err != nil {
		return fmt.Errorf("domain store: update verified: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("domain store: domain not found: %s", dom)
	}
	return nil
}

func (s *PostgresDomainStore) UpdateTLS(ctx context.Context, dom, tlsStatus string, expiresAt *time.Time) error {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.UpdateCustomDomainTLS(ctx, bridge, generated.UpdateCustomDomainTLSParams{
		TlsStatus:    dbconv.StringEmptyToNilPtr(tlsStatus),
		TlsExpiresAt: dbconv.TimePtrToTimestamptz(expiresAt),
		Domain:       dom,
	})
	if err != nil {
		return fmt.Errorf("domain store: update tls: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("domain store: domain not found: %s", dom)
	}
	return nil
}

func (s *PostgresDomainStore) UpdateFailure(ctx context.Context, dom, status, reason string) error {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.UpdateCustomDomainFailure(ctx, bridge, generated.UpdateCustomDomainFailureParams{
		Status:        status,
		FailureReason: dbconv.StringEmptyToNilPtr(reason),
		Domain:        dom,
	})
	if err != nil {
		return fmt.Errorf("domain store: update failure: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("domain store: domain not found: %s", dom)
	}
	return nil
}

func (s *PostgresDomainStore) Delete(ctx context.Context, dom string) error {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.DeleteCustomDomain(ctx, bridge, dom)
	if err != nil {
		return fmt.Errorf("domain store: delete: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("domain store: domain not found: %s", dom)
	}
	return nil
}

func (s *PostgresDomainStore) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	bridge := s.bridge()
	q := generated.New()
	n, err := q.CountActiveCustomDomainsByTenant(ctx, bridge, tenantID)
	if err != nil {
		return 0, fmt.Errorf("domain store: count by tenant: %w", err)
	}
	return int(n), nil
}

// =========================================================================
// Background reaper methods
// =========================================================================

// FindAndClaimVerified claims the verified domains for provisioning. The
// target status is derived from the Domain machine's DomainProvision edge,
// never spelled here.
func (s *PostgresDomainStore) FindAndClaimVerified(ctx context.Context) ([]ports.CustomDomain, error) {
	return s.claimByStatus(ctx, string(statemachine.DomainVerified), string(verifiedClaimTarget()))
}

// FindRemoving claims (FOR UPDATE SKIP LOCKED) the domains being removed
// without moving them: removing -> removing is a claim, not a transition.
func (s *PostgresDomainStore) FindRemoving(ctx context.Context) ([]ports.CustomDomain, error) {
	removing := string(statemachine.DomainRemoving)
	return s.claimByStatus(ctx, removing, removing)
}

// verifiedClaimTarget is where the Domain machine sends a verified domain the
// provisioner claims.
func verifiedClaimTarget() statemachine.State {
	return domainMachine.Transition(statemachine.DomainVerified, statemachine.DomainProvision)
}

// claimByStatus is unexported on purpose: it writes any status string, so the
// only callers are the two claims above, whose statuses come from the machine.
func (s *PostgresDomainStore) claimByStatus(ctx context.Context, fromStatus, toStatus string) ([]ports.CustomDomain, error) {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.ClaimCustomDomainsByStatus(ctx, bridge, generated.ClaimCustomDomainsByStatusParams{
		Status:   toStatus,
		Status_2: fromStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("domain store: claim %s→%s: %w", fromStatus, toStatus, err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromClaim(r))
	}
	return results, nil
}

func (s *PostgresDomainStore) FindPendingForAutoVerify(ctx context.Context, cellID string) ([]ports.CustomDomain, error) {
	// An empty cell means every cell; the query treats '' that way (#450).
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.FindPendingCustomDomainsForAutoVerify(ctx, bridge, cellID)
	if err != nil {
		return nil, fmt.Errorf("domain store: find pending for auto-verify (cell=%s): %w", cellID, err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromFindPending(r))
	}
	return results, nil
}

func (s *PostgresDomainStore) FindActiveForCNAMECheck(ctx context.Context, staleAfter time.Time) ([]ports.CustomDomain, error) {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.FindActiveCustomDomainsForCNAMECheck(ctx, bridge, pgtype.Timestamptz{Time: staleAfter, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("domain store: find active for CNAME check: %w", err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromFindCNAME(r))
	}
	return results, nil
}

func (s *PostgresDomainStore) FindStalePending(ctx context.Context, maxAge time.Duration) ([]ports.CustomDomain, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.FindStalePendingCustomDomains(ctx, bridge, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("domain store: find stale pending: %w", err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromStalePending(r))
	}
	return results, nil
}

func (s *PostgresDomainStore) FindStaleProvisioning(ctx context.Context, maxAge time.Duration) ([]ports.CustomDomain, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.FindStaleProvisioningCustomDomains(ctx, bridge, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("domain store: find stale provisioning: %w", err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromStaleProvisioning(r))
	}
	return results, nil
}

func (s *PostgresDomainStore) FindActiveWithTLSExpiry(ctx context.Context, withinDays int) ([]ports.CustomDomain, error) {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.FindActiveCustomDomainsWithTLSExpiry(ctx, bridge, int32(withinDays))
	if err != nil {
		return nil, fmt.Errorf("domain store: find active with TLS expiry: %w", err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromTLSExpiry(r))
	}
	return results, nil
}

func (s *PostgresDomainStore) UpdateCNAMECheck(ctx context.Context, dom string, checkTime time.Time, consecutiveFailures int) error {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.UpdateCustomDomainCNAMECheck(ctx, bridge, generated.UpdateCustomDomainCNAMECheckParams{
		LastCnameCheck:           pgtype.Timestamptz{Time: checkTime, Valid: true},
		CnameConsecutiveFailures: int32(consecutiveFailures),
		Domain:                   dom,
	})
	if err != nil {
		return fmt.Errorf("domain store: update CNAME check: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("domain store: domain not found: %s", dom)
	}
	return nil
}

func (s *PostgresDomainStore) ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]ports.CustomDomain, error) {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.ListActiveCustomDomainsByApp(ctx, bridge, generated.ListActiveCustomDomainsByAppParams{
		TenantID:    tenantID,
		ProjectName: project,
		AppName:     app,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("domain store: list active by app: %w", err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromActiveByApp(r))
	}
	return results, nil
}

func (s *PostgresDomainStore) findByStatus(ctx context.Context, status string) ([]ports.CustomDomain, error) {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.FindCustomDomainsByStatus(ctx, bridge, status)
	if err != nil {
		return nil, fmt.Errorf("domain store: find by status %s: %w", status, err)
	}
	results := make([]ports.CustomDomain, 0, len(rows))
	for _, r := range rows {
		results = append(results, *customDomainFromFindByStatus(r))
	}
	return results, nil
}

// =========================================================================
// CF Zone methods (single-domain mutation + status-find delegates)
// =========================================================================

func (s *PostgresDomainStore) UpdateCFZoneStatus(ctx context.Context, dom, cfZoneStatus string) error {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.UpdateCustomDomainCFZoneStatus(ctx, bridge, generated.UpdateCustomDomainCFZoneStatusParams{
		CfZoneStatus: dbconv.StringEmptyToNilPtr(cfZoneStatus),
		Domain:       dom,
	})
	if err != nil {
		return fmt.Errorf("domain store: update cf zone status: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("domain store: update cf zone status: domain not found")
	}
	return nil
}

func (s *PostgresDomainStore) FindPendingNS(ctx context.Context) ([]ports.CustomDomain, error) {
	return s.findByStatus(ctx, "pending_ns")
}

func (s *PostgresDomainStore) FindNSActive(ctx context.Context) ([]ports.CustomDomain, error) {
	return s.findByStatus(ctx, "ns_active")
}

// =========================================================================
// Helpers
// =========================================================================

func (s *PostgresDomainStore) bridge() generated.DBTX {
	return db.Generated(s.DB)
}

// makeCustomDomain assembles a *ports.CustomDomain from the 20
// columns shared by every SELECT * variant on custom_domains. All
// per-row-type helpers below funnel into this constructor so the
// conversion logic lives in one place.
func makeCustomDomain(
	id, tenantID, projectName, appName, environment, dom, status, verToken string,
	verifiedAt pgtype.Timestamptz,
	tlsStatus *string,
	tlsExpiresAt pgtype.Timestamptz,
	failureReason *string,
	lastCnameCheck pgtype.Timestamptz,
	cnameConsecutiveFailures int32,
	createdAt, updatedAt pgtype.Timestamptz,
	cfZoneID *string,
	cfZoneStatus *string,
	cfNameservers []string,
	dnsRecordsImported bool,
) *ports.CustomDomain {
	d := &ports.CustomDomain{
		ID:                       id,
		TenantID:                 tenantID,
		ProjectName:              projectName,
		AppName:                  appName,
		Environment:              environment,
		Domain:                   dom,
		Status:                   status,
		VerificationToken:        verToken,
		TLSStatus:                tlsStatus,
		FailureReason:            failureReason,
		CNAMEConsecutiveFailures: int(cnameConsecutiveFailures),
		CFZoneID:                 cfZoneID,
		CFNameservers:            cfNameservers,
		DNSRecordsImported:       dnsRecordsImported,
	}
	if verifiedAt.Valid {
		v := verifiedAt.Time
		d.VerifiedAt = &v
	}
	if tlsExpiresAt.Valid {
		v := tlsExpiresAt.Time
		d.TLSExpiresAt = &v
	}
	if lastCnameCheck.Valid {
		v := lastCnameCheck.Time
		d.LastCNAMECheck = &v
	}
	if createdAt.Valid {
		d.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		d.UpdatedAt = updatedAt.Time
	}
	if cfZoneStatus != nil {
		d.CFZoneStatus = *cfZoneStatus
	}
	return d
}

// Per-row-type adapters that funnel into makeCustomDomain. Each row
// type emitted by sqlc has identical column layout but distinct Go
// types — these adapters are mechanical 1-call helpers.

func customDomainFromGet(r generated.GetCustomDomainByDomainAndTenantRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromList(r generated.ListCustomDomainsByTenantRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromActiveByApp(r generated.ListActiveCustomDomainsByAppRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromClaim(r generated.ClaimCustomDomainsByStatusRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromFindByStatus(r generated.FindCustomDomainsByStatusRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromFindPending(r generated.FindPendingCustomDomainsForAutoVerifyRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromFindCNAME(r generated.FindActiveCustomDomainsForCNAMECheckRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromStalePending(r generated.FindStalePendingCustomDomainsRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromStaleProvisioning(r generated.FindStaleProvisioningCustomDomainsRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}

func customDomainFromTLSExpiry(r generated.FindActiveCustomDomainsWithTLSExpiryRow) *ports.CustomDomain {
	return makeCustomDomain(r.ID, r.TenantID, r.ProjectName, r.AppName, r.Environment, r.Domain, r.Status, r.VerificationToken,
		r.VerifiedAt, r.TlsStatus, r.TlsExpiresAt, r.FailureReason, r.LastCnameCheck, r.CnameConsecutiveFailures,
		r.CreatedAt, r.UpdatedAt, r.CfZoneID, r.CfZoneStatus, r.CfNameservers, r.DnsRecordsImported)
}
