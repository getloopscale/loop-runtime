// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, expect, it } from "vitest";
import {
  buildResourceTags,
  renderUnsealPolicyDocument,
  validateArgs,
} from "../components/aws-kms-openbao";

describe("aws-kms-openbao component (F1.1)", () => {
  describe("buildResourceTags", () => {
    it("returns canonical tag map with Project=loop-runtime + Component=openbao", () => {
      const tags = buildResourceTags("prod");
      expect(tags.Project).toBe("loop-runtime");
      expect(tags.Component).toBe("openbao");
      expect(tags.Stack).toBe("prod");
      expect(tags.Purpose).toBe("openbao-auto-unseal");
      expect(tags.ManagedBy).toBe("pulumi");
    });

    it("propagates stackName into Stack tag for audit per-stack", () => {
      expect(buildResourceTags("dev").Stack).toBe("dev");
      expect(buildResourceTags("staging").Stack).toBe("staging");
      expect(buildResourceTags("drill").Stack).toBe("drill");
    });

    it("includes plan reference for audit trail", () => {
      const tags = buildResourceTags("prod");
      expect(tags.Plan).toBe("2026-06-02-openbao-adoption-F1.1");
    });
  });

  describe("renderUnsealPolicyDocument", () => {
    const fakeArn = "arn:aws:kms:us-east-1:776217504929:key/abc-123";

    it("returns valid JSON with IAM Version 2012-10-17", () => {
      const json = renderUnsealPolicyDocument(fakeArn);
      const parsed = JSON.parse(json);
      expect(parsed.Version).toBe("2012-10-17");
    });

    it("has single statement Sid=OpenBaoAutoUnseal Effect=Allow", () => {
      const parsed = JSON.parse(renderUnsealPolicyDocument(fakeArn));
      expect(parsed.Statement).toHaveLength(1);
      expect(parsed.Statement[0].Sid).toBe("OpenBaoAutoUnseal");
      expect(parsed.Statement[0].Effect).toBe("Allow");
    });

    it("grants exactly 3 KMS actions (Encrypt + Decrypt + DescribeKey)", () => {
      const parsed = JSON.parse(renderUnsealPolicyDocument(fakeArn));
      const actions = parsed.Statement[0].Action;
      expect(actions).toHaveLength(3);
      expect(actions).toContain("kms:Encrypt");
      expect(actions).toContain("kms:Decrypt");
      expect(actions).toContain("kms:DescribeKey");
    });

    it("scopes Resource to the specific KMS key ARN (no wildcard)", () => {
      const parsed = JSON.parse(renderUnsealPolicyDocument(fakeArn));
      expect(parsed.Statement[0].Resource).toBe(fakeArn);
      expect(parsed.Statement[0].Resource).not.toBe("*");
    });

    it("rejects empty or non-string ARN", () => {
      expect(() => renderUnsealPolicyDocument("")).toThrow(/non-empty/);
      // @ts-expect-error — runtime validation test
      expect(() => renderUnsealPolicyDocument(null)).toThrow(/non-empty/);
      // @ts-expect-error — runtime validation test
      expect(() => renderUnsealPolicyDocument(undefined)).toThrow(/non-empty/);
    });

    it("does not include any action outside the OpenBao unseal scope", () => {
      const parsed = JSON.parse(renderUnsealPolicyDocument(fakeArn));
      const actions: string[] = parsed.Statement[0].Action;
      // Must NOT include broader KMS actions that would expand blast
      // radius beyond auto-unseal:
      const forbidden = [
        "kms:*",
        "kms:CreateKey",
        "kms:ScheduleKeyDeletion",
        "kms:DeleteAlias",
        "kms:Sign",
        "kms:GenerateDataKey",
      ];
      for (const f of forbidden) {
        expect(actions).not.toContain(f);
      }
    });
  });

  describe("validateArgs", () => {
    it("accepts valid args with default region", () => {
      expect(() => validateArgs({ stackName: "prod" })).not.toThrow();
    });

    it("accepts explicit us-east-1 region (Q2-F1 default)", () => {
      expect(() =>
        validateArgs({ stackName: "prod", region: "us-east-1" }),
      ).not.toThrow();
    });

    it("accepts other allowed regions (us-west, eu, sa)", () => {
      const allowed = ["us-west-2", "eu-west-1", "sa-east-1"];
      for (const region of allowed) {
        expect(() =>
          validateArgs({ stackName: "prod", region }),
        ).not.toThrow();
      }
    });

    it("rejects empty stackName", () => {
      expect(() => validateArgs({ stackName: "" })).toThrow(/stackName required/);
      expect(() => validateArgs({ stackName: "   " })).toThrow(/stackName required/);
    });

    it("rejects region not in allowlist", () => {
      expect(() =>
        validateArgs({ stackName: "prod", region: "us-gov-east-1" }),
      ).toThrow(/not in allowlist/);
      expect(() =>
        validateArgs({ stackName: "prod", region: "cn-north-1" }),
      ).toThrow(/not in allowlist/);
    });
  });
});
