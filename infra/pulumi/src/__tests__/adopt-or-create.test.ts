// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Sprint 8 v3.2 / CHG-001 — regression tests for ADR-0015 adopt-or-create policy.
//
// Each test exercises the documented contract of one lookup function. The
// philosophy is to fail loud when the upstream API shape changes, not to
// re-implement the API call itself (curl shell-out is intentionally non-mockable
// at the network level — we mock the execSync wrapper to return canonical
// upstream payloads).

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

// Hoisted mock state so vi.mock factory can read it before module init.
const mocks = vi.hoisted(() => ({
  execSync: vi.fn(() => ""),
}));

// Mock child_process at module level; ESM namespace is sealed so vi.spyOn
// can't replace exports after import. vi.mock with factory is the supported
// pattern for ESM-mode vitest.
vi.mock("child_process", () => ({
  execSync: mocks.execSync,
}));

import {
  lookupExistingVpc,
  lookupExistingRegistry,
  lookupExistingTunnel,
  lookupExistingKvNamespace,
  lookupExistingDnsRecord,
  lookupExistingWorkerScript,
  lookupExistingWorkerRoute,
  lookupExistingBucket,
  resolveOrphans,
  OrphanLookupResult,
} from "../core/adopt-or-create";

const ENV_BACKUP = { ...process.env };

describe("adopt-or-create — DigitalOcean", () => {
  beforeEach(() => {
    process.env = { ...ENV_BACKUP };
    mocks.execSync.mockReset();
  });

  afterEach(() => {
    process.env = ENV_BACKUP;
  });

  describe("lookupExistingVpc", () => {
    it("returns undefined when DIGITALOCEAN_TOKEN is unset", () => {
      delete process.env.DIGITALOCEAN_TOKEN;
      expect(lookupExistingVpc("any-name")).toBeUndefined();
    });

    it("returns vpc id when API returns matching name", () => {
      process.env.DIGITALOCEAN_TOKEN = "stub-token";
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ vpcs: [{ name: "loopzero-infra-dev-sfo3-net", id: "abc-123" }] }) as never,
      );
      expect(lookupExistingVpc("loopzero-infra-dev-sfo3-net")).toBe("abc-123");
    });

    it("returns undefined when no VPC matches by name", () => {
      process.env.DIGITALOCEAN_TOKEN = "stub-token";
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ vpcs: [{ name: "other-vpc", id: "xyz" }] }) as never,
      );
      expect(lookupExistingVpc("loopzero-infra-dev-sfo3-net")).toBeUndefined();
    });

    it("returns undefined when API call throws (network failure, 401, malformed JSON)", () => {
      process.env.DIGITALOCEAN_TOKEN = "stub-token";
      mocks.execSync.mockImplementationOnce(() => {
        throw new Error("curl: (7) Failed to connect");
      });
      expect(lookupExistingVpc("any-name")).toBeUndefined();
    });
  });

  describe("lookupExistingRegistry (Sprint 8 v3.2 fix)", () => {
    it("returns undefined when DIGITALOCEAN_TOKEN is unset (greenfield account)", () => {
      delete process.env.DIGITALOCEAN_TOKEN;
      expect(lookupExistingRegistry()).toBeUndefined();
    });

    it("returns the registry name when DO has a registry provisioned", () => {
      process.env.DIGITALOCEAN_TOKEN = "stub-token";
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ registry: { name: "loopzero-system", region: "sfo3" } }) as never,
      );
      expect(lookupExistingRegistry()).toBe("loopzero-system");
    });

    it("returns undefined when DO account has NO registry yet (404 wrapped as undefined response)", () => {
      process.env.DIGITALOCEAN_TOKEN = "stub-token";
      mocks.execSync.mockReturnValueOnce(JSON.stringify({}) as never);
      expect(lookupExistingRegistry()).toBeUndefined();
    });

    it("returns undefined when API call fails (curl error, 401)", () => {
      process.env.DIGITALOCEAN_TOKEN = "stub-token";
      mocks.execSync.mockImplementationOnce(() => {
        throw new Error("curl: (22) The requested URL returned 401");
      });
      expect(lookupExistingRegistry()).toBeUndefined();
    });

    it("does NOT filter by name (DO allows ONE registry per account, returns whatever exists)", () => {
      // This is the key contract: lookupExistingRegistry takes no arguments.
      // The function MUST adopt the existing registry regardless of what name
      // the consumer expects, because DO only allows one registry per account.
      // If we filtered by expected name we'd recreate-fail when names drift
      // (the exact bug Sprint 8 v3.2 fixed).
      process.env.DIGITALOCEAN_TOKEN = "stub-token";
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ registry: { name: "totally-different-name" } }) as never,
      );
      expect(lookupExistingRegistry()).toBe("totally-different-name");
    });
  });

  describe("lookupExistingBucket", () => {
    it("returns name when bucket exists (200 OK)", () => {
      mocks.execSync.mockReturnValueOnce("200" as never);
      expect(lookupExistingBucket("loopzero-builds", "sfo3")).toBe("loopzero-builds");
    });

    it("returns name when bucket exists privately (403 Forbidden)", () => {
      // Private buckets return 403, NOT 404. Treat 403 as "exists".
      mocks.execSync.mockReturnValueOnce("403" as never);
      expect(lookupExistingBucket("private-bucket", "sfo3")).toBe("private-bucket");
    });

    it("returns undefined when bucket does not exist (404 Not Found)", () => {
      mocks.execSync.mockReturnValueOnce("404" as never);
      expect(lookupExistingBucket("nonexistent", "sfo3")).toBeUndefined();
    });

    it("returns undefined on curl error (timeout, DNS fail)", () => {
      mocks.execSync.mockImplementationOnce(() => {
        throw new Error("curl: timeout");
      });
      expect(lookupExistingBucket("any", "sfo3")).toBeUndefined();
    });
  });
});

describe("adopt-or-create — Cloudflare", () => {
  beforeEach(() => {
    process.env = { ...ENV_BACKUP };
    process.env.CLOUDFLARE_API_TOKEN = "stub-cf-token";
    mocks.execSync.mockReset();
  });

  afterEach(() => {
    process.env = ENV_BACKUP;
  });

  describe("lookupExistingTunnel", () => {
    it("returns accountId/tunnelId when tunnel exists by name", () => {
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ result: [{ id: "tunnel-uuid-123", name: "loopzero-dev" }] }) as never,
      );
      expect(lookupExistingTunnel("loopzero-dev", "account-456")).toBe("account-456/tunnel-uuid-123");
    });

    it("returns undefined when tunnel does not exist", () => {
      mocks.execSync.mockReturnValueOnce(JSON.stringify({ result: [] }) as never);
      expect(lookupExistingTunnel("loopzero-dev", "account-456")).toBeUndefined();
    });

    it("returns undefined when CLOUDFLARE_API_TOKEN is unset", () => {
      delete process.env.CLOUDFLARE_API_TOKEN;
      expect(lookupExistingTunnel("loopzero-dev", "account-456")).toBeUndefined();
    });
  });

  describe("lookupExistingKvNamespace", () => {
    it("returns accountId/namespaceId when KV namespace exists by title", () => {
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ result: [{ id: "kv-uuid", title: "loopzero-routing-dev" }] }) as never,
      );
      expect(lookupExistingKvNamespace("loopzero-routing-dev", "acc")).toBe("acc/kv-uuid");
    });

    it("returns undefined when title does not match any namespace", () => {
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ result: [{ id: "x", title: "different" }] }) as never,
      );
      expect(lookupExistingKvNamespace("loopzero-routing-dev", "acc")).toBeUndefined();
    });
  });

  describe("lookupExistingDnsRecord", () => {
    it("returns zoneId/recordId when CNAME record exists at name", () => {
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ result: [{ id: "rec-id", name: "*.dev.getloopscale.com", type: "CNAME" }] }) as never,
      );
      expect(lookupExistingDnsRecord("zone", "*.dev.getloopscale.com", "CNAME")).toBe("zone/rec-id");
    });

    it("returns undefined when no record matches", () => {
      mocks.execSync.mockReturnValueOnce(JSON.stringify({ result: [] }) as never);
      expect(lookupExistingDnsRecord("zone", "missing.dev.getloopscale.com", "CNAME")).toBeUndefined();
    });
  });

  describe("lookupExistingWorkerScript", () => {
    it("returns accountId/scriptName when script with matching id exists", () => {
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ result: [{ id: "loopzero-static-delivery-dev" }] }) as never,
      );
      expect(lookupExistingWorkerScript("loopzero-static-delivery-dev", "acc")).toBe(
        "acc/loopzero-static-delivery-dev",
      );
    });

    it("returns undefined when script id does not match any", () => {
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ result: [{ id: "other-script" }] }) as never,
      );
      expect(lookupExistingWorkerScript("loopzero-static-delivery-dev", "acc")).toBeUndefined();
    });
  });

  describe("lookupExistingWorkerRoute", () => {
    it("returns zoneId/routeId when pattern matches existing route", () => {
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ result: [{ id: "route-id", pattern: "*.dev.getloopscale.com/*" }] }) as never,
      );
      expect(lookupExistingWorkerRoute("zone", "*.dev.getloopscale.com/*")).toBe("zone/route-id");
    });

    it("returns undefined when pattern does not match", () => {
      mocks.execSync.mockReturnValueOnce(
        JSON.stringify({ result: [{ id: "x", pattern: "different.dev.getloopscale.com/*" }] }) as never,
      );
      expect(lookupExistingWorkerRoute("zone", "*.dev.getloopscale.com/*")).toBeUndefined();
    });
  });
});

describe("resolveOrphans — orchestrator (Sprint 8 v3.2 contract)", () => {
  beforeEach(() => {
    process.env = { ...ENV_BACKUP };
    mocks.execSync.mockReset();
  });

  afterEach(() => {
    process.env = ENV_BACKUP;
  });

  it("collects all 7 orphan IDs in a single OrphanLookupResult", () => {
    process.env.DIGITALOCEAN_TOKEN = "stub-do";
    process.env.CLOUDFLARE_API_TOKEN = "stub-cf";

    // Each lookup hits a different upstream endpoint — return appropriate canonical shape per call.
    mocks.execSync
      .mockReturnValueOnce(JSON.stringify({ vpcs: [{ name: "loopzero-infra-dev-sfo3-net", id: "vpc-id" }] }) as never)
      .mockReturnValueOnce(JSON.stringify({ registry: { name: "loopzero-system" } }) as never)
      .mockReturnValueOnce(JSON.stringify({ result: [{ id: "tunnel-id", name: "loopzero-tunnel-dev" }] }) as never)
      .mockReturnValueOnce(JSON.stringify({ result: [{ id: "kv-id", title: "loopzero-routing-dev" }] }) as never)
      .mockReturnValueOnce(JSON.stringify({ result: [{ id: "dns-id", name: "*.dev.getloopscale.com", type: "CNAME" }] }) as never)
      .mockReturnValueOnce(JSON.stringify({ result: [{ id: "loopzero-static-delivery-dev" }] }) as never)
      .mockReturnValueOnce(JSON.stringify({ result: [{ id: "route-id", pattern: "*.dev.getloopscale.com/*" }] }) as never);

    const result: OrphanLookupResult = resolveOrphans({
      vpcName: "loopzero-infra-dev-sfo3-net",
      tunnelName: "loopzero-tunnel-dev",
      kvTitle: "loopzero-routing-dev",
      dnsWildcard: "*.dev.getloopscale.com",
      workerScriptName: "loopzero-static-delivery-dev",
      workerRoutePattern: "*.dev.getloopscale.com/*",
      cfAccountId: "acc",
      cfZoneId: "zone",
    });

    // Every key must be populated for adopt-or-create to be useful.
    expect(result.vpcId).toBe("vpc-id");
    expect(result.registryImportId).toBe("loopzero-system"); // Sprint 8 v3.2 contract
    expect(result.tunnelImportId).toBe("acc/tunnel-id");
    expect(result.kvNamespaceImportId).toBe("acc/kv-id");
    expect(result.dnsRecordImportId).toBe("zone/dns-id");
    expect(result.workerScriptImportId).toBe("acc/loopzero-static-delivery-dev");
    expect(result.workerRouteImportId).toBe("zone/route-id");
  });

  it("returns undefined fields when nothing exists in cloud (greenfield)", () => {
    process.env.DIGITALOCEAN_TOKEN = "stub-do";
    process.env.CLOUDFLARE_API_TOKEN = "stub-cf";

    // Every lookup returns "no result" shape.
    mocks.execSync.mockImplementation(
      () => JSON.stringify({ vpcs: [], result: [] }) as never,
    );

    const result = resolveOrphans({
      vpcName: "any",
      tunnelName: "any",
      kvTitle: "any",
      dnsWildcard: "any",
      workerScriptName: "any",
      workerRoutePattern: "any",
      cfAccountId: "acc",
      cfZoneId: "zone",
    });

    // None of the lookups should have hit; all undefined.
    expect(result.vpcId).toBeUndefined();
    expect(result.registryImportId).toBeUndefined();
    expect(result.tunnelImportId).toBeUndefined();
    expect(result.kvNamespaceImportId).toBeUndefined();
    expect(result.dnsRecordImportId).toBeUndefined();
    expect(result.workerScriptImportId).toBeUndefined();
    expect(result.workerRouteImportId).toBeUndefined();
  });
});
