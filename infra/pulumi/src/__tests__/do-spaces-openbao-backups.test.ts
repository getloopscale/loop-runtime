// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, expect, it } from "vitest";
import {
  buildBackupResourceTags,
  buildLifecycleRules,
  validateArgs,
} from "../components/do-spaces-openbao-backups";

describe("do-spaces-openbao-backups component (F1.2)", () => {
  describe("buildBackupResourceTags", () => {
    it("returns canonical tags with project=loopruntime + component=openbao", () => {
      const tags = buildBackupResourceTags("prod");
      expect(tags.project).toBe("loopruntime");
      expect(tags.component).toBe("openbao");
      expect(tags.stack).toBe("prod");
      expect(tags.purpose).toBe("openbao-raft-snapshot-backup");
      expect(tags["managed-by"]).toBe("pulumi");
    });

    it("includes plan reference for audit trail", () => {
      const tags = buildBackupResourceTags("dev");
      expect(tags.plan).toBe("2026-06-02-openbao-adoption-F1.2");
    });

    it("propagates stackName into stack tag", () => {
      expect(buildBackupResourceTags("dev").stack).toBe("dev");
      expect(buildBackupResourceTags("staging").stack).toBe("staging");
    });
  });

  describe("buildLifecycleRules", () => {
    it("returns 3 rules with canonical IDs per Q12", () => {
      const rules = buildLifecycleRules(7, 30, 84);
      expect(rules).toHaveLength(3);
      const ids = rules.map((r) => r.id);
      expect(ids).toContain("openbao-snapshot-hourly");
      expect(ids).toContain("openbao-snapshot-daily");
      expect(ids).toContain("openbao-snapshot-weekly");
    });

    it("uses snapshots/{tier}/ prefix structure", () => {
      const rules = buildLifecycleRules(7, 30, 84);
      const hourly = rules.find((r) => r.id === "openbao-snapshot-hourly");
      const daily = rules.find((r) => r.id === "openbao-snapshot-daily");
      const weekly = rules.find((r) => r.id === "openbao-snapshot-weekly");
      expect(hourly?.prefix).toBe("snapshots/hourly/");
      expect(daily?.prefix).toBe("snapshots/daily/");
      expect(weekly?.prefix).toBe("snapshots/weekly/");
    });

    it("applies retention days as passed", () => {
      const rules = buildLifecycleRules(7, 30, 84);
      expect(rules.find((r) => r.id === "openbao-snapshot-hourly")?.expirationDays).toBe(7);
      expect(rules.find((r) => r.id === "openbao-snapshot-daily")?.expirationDays).toBe(30);
      expect(rules.find((r) => r.id === "openbao-snapshot-weekly")?.expirationDays).toBe(84);
    });

    it("marks all rules as Enabled by default", () => {
      const rules = buildLifecycleRules(7, 30, 84);
      for (const r of rules) {
        expect(r.status).toBe("Enabled");
      }
    });

    it("rejects retention days <= 0", () => {
      expect(() => buildLifecycleRules(0, 30, 84)).toThrow(/must be > 0/);
      expect(() => buildLifecycleRules(7, -1, 84)).toThrow(/must be > 0/);
      expect(() => buildLifecycleRules(7, 30, 0)).toThrow(/must be > 0/);
    });
  });

  describe("validateArgs (EC-13 cross-region invariant)", () => {
    it("accepts default args (nyc3 bucket, sfo3 mgmt)", () => {
      expect(() => validateArgs({ stackName: "prod" })).not.toThrow();
    });

    it("accepts explicit cross-region pair", () => {
      expect(() =>
        validateArgs({
          stackName: "prod",
          bucketRegion: "nyc3",
          mgmtRegion: "sfo3",
        }),
      ).not.toThrow();
      expect(() =>
        validateArgs({
          stackName: "dev",
          bucketRegion: "ams3",
          mgmtRegion: "fra1",
        }),
      ).not.toThrow();
    });

    it("EC-13: rejects same-region (bucket == mgmt) — defeats DR purpose", () => {
      expect(() =>
        validateArgs({
          stackName: "prod",
          bucketRegion: "sfo3",
          mgmtRegion: "sfo3",
        }),
      ).toThrow(/EC-13 violation/);
      expect(() =>
        validateArgs({
          stackName: "drill",
          bucketRegion: "nyc3",
          mgmtRegion: "nyc3",
        }),
      ).toThrow(/EC-13 violation/);
    });

    it("rejects bucketRegion not in DO Spaces allowlist", () => {
      expect(() =>
        validateArgs({
          stackName: "prod",
          bucketRegion: "us-east-1", // AWS region — not DO
        }),
      ).toThrow(/not in DO Spaces allowlist/);
      expect(() =>
        validateArgs({
          stackName: "prod",
          bucketRegion: "tor1", // DO has TOR1 but not Spaces
        }),
      ).toThrow(/not in DO Spaces allowlist/);
    });

    it("accepts all DO Spaces supported regions", () => {
      const validRegions = ["nyc3", "sfo2", "ams3", "sgp1", "fra1", "syd1"];
      for (const r of validRegions) {
        expect(() =>
          validateArgs({
            stackName: "prod",
            bucketRegion: r,
            mgmtRegion: r === "sfo3" ? "nyc3" : "sfo3", // ensure different
          }),
        ).not.toThrow();
      }
    });

    it("rejects empty stackName", () => {
      expect(() => validateArgs({ stackName: "" })).toThrow(/stackName required/);
      expect(() => validateArgs({ stackName: "   " })).toThrow(/stackName required/);
    });
  });
});
