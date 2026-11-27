// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package clientip

import (
	"net"
	"net/http/httptest"
	"testing"
)

func TestResolver_IgnoresXForwardedForFromAnUntrustedPeer(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "198.51.100.7:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.99")

	got := NewResolver(nil).IP(r)

	if got.String() != "198.51.100.7" {
		t.Errorf("IP = %s, want the TCP peer 198.51.100.7: the header came from an untrusted client", got)
	}
}

func TestResolver_HonoursTheFirstForwardedEntryFromATrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "173.245.48.10:443"
	r.Header.Set("X-Forwarded-For", "203.0.113.99, 10.0.0.1")

	got := NewResolver(ParseTrustedProxyCIDRs("173.245.48.0/20")).IP(r)

	if got.String() != "203.0.113.99" {
		t.Errorf("IP = %s, want 203.0.113.99", got)
	}
}

func TestResolver_FallsBackToThePeerWhenTheForwardedEntryIsNotAnIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "173.245.48.10:443"
	r.Header.Set("X-Forwarded-For", "not-an-ip")

	got := NewResolver(ParseTrustedProxyCIDRs("173.245.48.0/20")).IP(r)

	if got.String() != "173.245.48.10" {
		t.Errorf("IP = %s, want the peer 173.245.48.10", got)
	}
}

func TestParseTrustedProxyCIDRs_SkipsInvalidEntries(t *testing.T) {
	cidrs := ParseTrustedProxyCIDRs("173.245.48.0/20, 103.21.244.0/22, garbage, ")
	if len(cidrs) != 2 {
		t.Fatalf("want 2 valid CIDRs, got %d", len(cidrs))
	}
	if !cidrs[0].Contains(net.ParseIP("173.245.48.10")) {
		t.Error("first CIDR must contain 173.245.48.10")
	}
}

func TestParseTrustedProxyCIDRs_EmptyTrustsNothing(t *testing.T) {
	if got := ParseTrustedProxyCIDRs("  "); len(got) != 0 {
		t.Errorf("want no CIDRs, got %v", got)
	}
}

func TestBucketKey_KeepsIPv4Verbatim(t *testing.T) {
	if got := BucketKey(net.ParseIP("203.0.113.5")); got != "203.0.113.5" {
		t.Errorf("got %q", got)
	}
}

func TestBucketKey_CollapsesIPv6ToItsSlash64(t *testing.T) {
	if got := BucketKey(net.ParseIP("2001:db8:abcd:0::dead:beef")); got != "2001:db8:abcd::/64" {
		t.Errorf("got %q", got)
	}
}

// An IPv4-mapped IPv6 address must key as the plain IPv4 it is, or a client
// alternates encodings to hold two buckets and double its allowance.
func TestBucketKey_KeysAnIPv4MappedAddressAsPlainIPv4(t *testing.T) {
	if got := BucketKey(net.ParseIP("::ffff:203.0.113.5")); got != "203.0.113.5" {
		t.Errorf("got %q, want 203.0.113.5", got)
	}
}

func TestBucketKey_NilIsEmpty(t *testing.T) {
	if got := BucketKey(nil); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestParsePeerIP(t *testing.T) {
	cases := []struct {
		name, in, want string
		wantNil        bool
	}{
		{"ipv4 host:port", "203.0.113.5:54321", "203.0.113.5", false},
		{"ipv4 bare", "203.0.113.5", "203.0.113.5", false},
		{"ipv6 bracketed", "[2001:db8::1]:443", "2001:db8::1", false},
		{"empty", "", "", true},
		{"garbage", "not-an-ip", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parsePeerIP(c.in)
			if c.wantNil {
				if got != nil {
					t.Errorf("parsePeerIP(%q) = %v, want nil", c.in, got)
				}
				return
			}
			if got == nil || got.String() != c.want {
				t.Errorf("parsePeerIP(%q) = %v, want %s", c.in, got, c.want)
			}
		})
	}
}
