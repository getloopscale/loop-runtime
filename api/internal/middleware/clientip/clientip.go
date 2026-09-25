// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package clientip derives the address of the client behind a request.
//
// It is the one place that decides whether X-Forwarded-For is believed. The
// header is honoured ONLY when the TCP peer is a trusted proxy; from anyone
// else it is a string the client wrote, and keying anything on it — a rate
// limit, an audit throttle — hands the client the key (EC-3 of the edge-case
// review).
package clientip

import (
	"net"
	"net/http"
	"strings"
)

// Resolver resolves the client IP of a request under a trusted-proxy policy.
// The zero value trusts no proxy: the TCP peer is always the answer.
type Resolver struct {
	trustedProxies []*net.IPNet
}

// NewResolver returns a Resolver that believes X-Forwarded-For only from a
// peer inside one of trustedProxies.
func NewResolver(trustedProxies []*net.IPNet) Resolver {
	return Resolver{trustedProxies: trustedProxies}
}

// IP returns the client IP of r, or nil when the peer address cannot be
// parsed. With a trusted peer and a parseable first X-Forwarded-For entry, that
// entry is the client; otherwise the TCP peer is.
func (res Resolver) IP(r *http.Request) net.IP {
	peer := parsePeerIP(r.RemoteAddr)
	if peer == nil || !res.isTrusted(peer) {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	if forwarded := net.ParseIP(first); forwarded != nil {
		return forwarded
	}
	return peer
}

// BucketKey returns the key an IP is counted under: an IPv4 address verbatim,
// an IPv6 address collapsed to its /64. A single consumer IPv6 client owns a
// whole /64, so per-address keys would hand it 2^64 buckets (EC-13). An
// IPv4-mapped IPv6 address keys as the plain IPv4 it is. Returns "" for nil.
func BucketKey(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// ParseTrustedProxyCIDRs parses a comma-separated CIDR list (for example the
// LOOPZERO_TRUSTED_PROXY_CIDRS environment variable). Invalid entries are
// skipped; the caller logs the resulting count so a misconfiguration shows.
func ParseTrustedProxyCIDRs(s string) []*net.IPNet {
	var out []*net.IPNet
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, cidr, err := net.ParseCIDR(raw); err == nil {
			out = append(out, cidr)
		}
	}
	return out
}

func (res Resolver) isTrusted(ip net.IP) bool {
	for _, cidr := range res.trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// parsePeerIP extracts the IP from r.RemoteAddr ("ip:port", "[ipv6]:port",
// or a bare IP in tests).
func parsePeerIP(remoteAddr string) net.IP {
	if remoteAddr == "" {
		return nil
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	return net.ParseIP(host)
}
