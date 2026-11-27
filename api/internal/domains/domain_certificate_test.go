package domains

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

type fakeCertificates struct {
	notAfter map[string]time.Time
	asked    []string
	edge     string
}

func (f *fakeCertificates) NotAfter(_ context.Context, edgeHost, serverName string) (time.Time, error) {
	f.edge = edgeHost
	f.asked = append(f.asked, serverName)
	at, ok := f.notAfter[serverName]
	if !ok {
		return time.Time{}, errors.New("no certificate for " + serverName)
	}
	return at, nil
}

func certificateChecker(store *fakeDomainHealthStore, certs CertificateProber) *DomainHealthChecker {
	h := NewDomainHealthChecker(store, slog.New(slog.NewTextHandler(io.Discard, nil)), DomainHealthCheckerConfig{
		CNAMETarget: "cname.edge.example",
	})
	h.WithCertificates(certs)
	return h
}

func ptr[T any](v T) *T { return &v }

// An active domain served a valid certificate and the API went on answering
// tls_status "pending": nothing ever wrote the field
// (getloopscale/loop-runtime#211).
func TestRecordCertificates_WritesWhatTheEdgeServes(t *testing.T) {
	expires := time.Now().Add(80 * 24 * time.Hour).UTC().Truncate(time.Second)
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{
		{Domain: "app.acme.com", Status: "active", TLSStatus: ptr("pending")},
	}
	certs := &fakeCertificates{notAfter: map[string]time.Time{"app.acme.com": expires}}

	certificateChecker(store, certs).recordCertificates(context.Background())

	got, ok := store.tls["app.acme.com"]
	if !ok {
		t.Fatal("the certificate of app.acme.com was not recorded")
	}
	if got.status != "issued" || !got.expiresAt.Equal(expires) {
		t.Errorf("recorded %q until %v, want issued until %v", got.status, got.expiresAt, expires)
	}
	if certs.edge != "cname.edge.example" {
		t.Errorf("asked %q, want the edge the domains point to", certs.edge)
	}
}

func TestRecordCertificates_LeavesADomainWithoutCertificatePending(t *testing.T) {
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{
		{Domain: "new.acme.com", Status: "active", TLSStatus: ptr("pending")},
	}

	certificateChecker(store, &fakeCertificates{}).recordCertificates(context.Background())

	if len(store.tls) != 0 {
		t.Errorf("recorded %v for a domain the edge serves no certificate for", store.tls)
	}
}

func TestRecordCertificates_ReadsAgainOnlyNearTheExpiry(t *testing.T) {
	far := time.Now().Add(60 * 24 * time.Hour)
	near := time.Now().Add(10 * 24 * time.Hour)
	renewed := time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{
		{Domain: "settled.acme.com", Status: "active", TLSStatus: ptr("issued"), TLSExpiresAt: &far},
		{Domain: "renewing.acme.com", Status: "active", TLSStatus: ptr("issued"), TLSExpiresAt: &near},
	}
	certs := &fakeCertificates{notAfter: map[string]time.Time{"renewing.acme.com": renewed}}

	certificateChecker(store, certs).recordCertificates(context.Background())

	if len(certs.asked) != 1 || certs.asked[0] != "renewing.acme.com" {
		t.Errorf("asked for %v, want only the certificate near its expiry", certs.asked)
	}
	if got := store.tls["renewing.acme.com"]; !got.expiresAt.Equal(renewed) {
		t.Errorf("recorded expiry %v, want the renewed %v", got.expiresAt, renewed)
	}
}

func TestRecordCertificates_DoesNothingWithoutAnEdgeToAsk(t *testing.T) {
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{{Domain: "app.acme.com", Status: "active"}}
	certs := &fakeCertificates{notAfter: map[string]time.Time{"app.acme.com": time.Now().Add(time.Hour)}}
	h := NewDomainHealthChecker(store, slog.New(slog.NewTextHandler(io.Discard, nil)), DomainHealthCheckerConfig{})
	h.WithCertificates(certs)

	h.recordCertificates(context.Background())

	if len(certs.asked) != 0 {
		t.Errorf("asked for %v with no edge configured", certs.asked)
	}
}

// The probe connects to the edge and names the customer's host in the handshake. It never
// resolves the customer's name, so a domain cannot steer the API to an address of its choice.
func TestEdgeCertificateProber_ReadsTheCertificateTheEdgeServesForAHost(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	prober := EdgeCertificateProber{Port: port, Roots: roots}

	notAfter, err := prober.NotAfter(context.Background(), host, "example.com")
	if err != nil {
		t.Fatalf("NotAfter: %v", err)
	}
	if !notAfter.Equal(server.Certificate().NotAfter) {
		t.Errorf("NotAfter = %v, want %v", notAfter, server.Certificate().NotAfter)
	}

	if _, err := prober.NotAfter(context.Background(), host, "other.invalid"); err == nil {
		t.Error("a certificate that does not cover the host was accepted")
	}
}
