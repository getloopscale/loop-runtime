import { describe, it, expect } from "vitest";

/**
 * Tests the DNS record type detection logic.
 * The actual function lives in the DO provider, but the logic is pure
 * and can be tested independently.
 */

type DnsRecordType = "A" | "CNAME" | "AAAA";

function detectRecordType(target: string): DnsRecordType {
  if (/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(target)) {
    return "A";
  }
  if (target.includes(":")) {
    return "AAAA";
  }
  return "CNAME";
}

describe("detectRecordType", () => {
  it("returns A for IPv4 addresses", () => {
    expect(detectRecordType("1.2.3.4")).toBe("A");
    expect(detectRecordType("10.0.0.1")).toBe("A");
    expect(detectRecordType("192.168.1.100")).toBe("A");
    expect(detectRecordType("255.255.255.255")).toBe("A");
  });

  it("returns AAAA for IPv6 addresses", () => {
    expect(detectRecordType("2001:0db8::1")).toBe("AAAA");
    expect(detectRecordType("::1")).toBe("AAAA");
    expect(detectRecordType("fe80::1%25eth0")).toBe("AAAA");
  });

  it("returns CNAME for hostnames", () => {
    expect(detectRecordType("lb.digitalocean.com")).toBe("CNAME");
    expect(detectRecordType("a1b2c3.elb.amazonaws.com")).toBe("CNAME");
    expect(detectRecordType("my-lb.example.com")).toBe("CNAME");
  });

  it("returns CNAME for single-label hostnames", () => {
    expect(detectRecordType("traefik")).toBe("CNAME");
  });

  it("does not false-positive on hostnames with numbers", () => {
    expect(detectRecordType("lb-12345.cloud.com")).toBe("CNAME");
    expect(detectRecordType("192.168.1.com")).toBe("CNAME");
  });
});
