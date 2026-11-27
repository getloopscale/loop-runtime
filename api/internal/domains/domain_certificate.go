package domains

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// tlsStatusIssued is the tls_status of a domain the edge serves a valid certificate for.
const tlsStatusIssued = "issued"

// certificateRereadWindow is how close to its expiry a recorded certificate is read again.
// The edge renews before that, and the next read records the new expiry.
const certificateRereadWindow = 30 * 24 * time.Hour

// certificateReadTimeout bounds one read. The first handshake for a new host can take the
// edge as long as it needs to obtain the certificate.
const certificateReadTimeout = 45 * time.Second

// CertificateProber reads the certificate the edge serves for a host.
type CertificateProber interface {
	// NotAfter is the expiry of the certificate edgeHost serves when asked for serverName.
	// It fails when the edge serves none, or one that is not valid for serverName.
	NotAfter(ctx context.Context, edgeHost, serverName string) (time.Time, error)
}

// WithCertificates lets the checker record which domains have a certificate at the edge.
func (h *DomainHealthChecker) WithCertificates(certificates CertificateProber) {
	h.certificates = certificates
}

// recordCertificates writes tls_status and the expiry of every routed domain the edge
// serves a certificate for. The edge obtains certificates by itself and reports to no one,
// so the API learns the result by asking it.
//
// Asking has a second effect on an edge that obtains certificates on demand: the handshake
// for a newly active domain makes it obtain the certificate now, before the first visitor.
func (h *DomainHealthChecker) recordCertificates(ctx context.Context) {
	if h.certificates == nil || h.cnameTarget == "" {
		return
	}
	serving, err := h.store.FindServing(ctx)
	if err != nil {
		h.logger.Error("domain health: find serving for certificates failed", "error", err)
		return
	}
	now := time.Now()
	for i := range serving {
		if ctx.Err() != nil {
			return
		}
		d := &serving[i]
		if d.IsCFManaged() || !needsCertificateRead(d, now) {
			continue
		}
		readCtx, cancel := context.WithTimeout(ctx, certificateReadTimeout)
		notAfter, err := h.certificates.NotAfter(readCtx, h.cnameTarget, d.Domain)
		cancel()
		if err != nil {
			h.logger.Info("domain health: the edge serves no valid certificate yet", "domain", d.Domain, "error", err)
			continue
		}
		if err := h.store.UpdateTLS(ctx, d.Domain, tlsStatusIssued, &notAfter); err != nil {
			h.logger.Error("domain health: certificate update failed", "error", err, "domain", d.Domain)
		}
	}
}

// needsCertificateRead reports whether what is recorded about d's certificate is missing
// or about to go stale.
func needsCertificateRead(d *ports.CustomDomain, now time.Time) bool {
	if d.TLSStatus == nil || *d.TLSStatus != tlsStatusIssued || d.TLSExpiresAt == nil {
		return true
	}
	return d.TLSExpiresAt.Sub(now) <= certificateRereadWindow
}

// EdgeCertificateProber reads a certificate with a TLS handshake against the edge.
//
// It connects to the edge's own name and sends the customer's host only as the server name
// of the handshake. The customer's name is never resolved, so a domain cannot point the API
// at an address of its owner's choice.
type EdgeCertificateProber struct {
	// Port is the edge's TLS port. Empty means 443.
	Port string
	// Roots are the authorities a certificate must chain to. Nil means the system's.
	Roots *x509.CertPool
}

// NotAfter implements CertificateProber.
func (p EdgeCertificateProber) NotAfter(ctx context.Context, edgeHost, serverName string) (time.Time, error) {
	port := p.Port
	if port == "" {
		port = "443"
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 10 * time.Second},
		Config:    &tls.Config{ServerName: serverName, RootCAs: p.Roots, MinVersion: tls.VersionTLS12},
	}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(edgeHost, port))
	if err != nil {
		return time.Time{}, fmt.Errorf("handshake with %s for %s: %w", edgeHost, serverName, err)
	}
	defer conn.Close()
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return time.Time{}, errors.New("the edge connection is not TLS")
	}
	peers := tlsConn.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		return time.Time{}, fmt.Errorf("%s presented no certificate for %s", edgeHost, serverName)
	}
	return peers[0].NotAfter, nil
}
