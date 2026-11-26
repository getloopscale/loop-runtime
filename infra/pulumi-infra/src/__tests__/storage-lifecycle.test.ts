// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

// SD-15.8 (infrastructure system-design audit, 2026-09-07, as corrected).
//
// The audit first reported that source bundles were never expired. That was
// false: infra/helm/minio/lifecycle-job.yaml expires the loopruntime-builds bucket at
// 30 days, applied by `task infra:install:core`.
//
// What survived the correction is narrower and real. That ILM rule is applied
// to the IN-CLUSTER MinIO by a Taskfile kubectl path. Production object storage
// is DigitalOcean Spaces, created here, and the bucket that actually holds
// production bundles had no lifecycle rule declared in the infrastructure that
// creates it — so retention depended on someone running a task against a
// cluster that is not where the production objects live.
//
// One bundle per commit, up to 500 MB each (SOURCE_MAX_BUNDLE_SIZE), on the
// only store that grows with customer activity.

// tsconfig sets module=commonjs, so import.meta is unavailable here. The test
// runner starts at the package root; resolve from there rather than from the
// test file's location.
const root = resolve(process.cwd(), "src");

function read(rel: string): string {
  return readFileSync(join(root, rel), "utf8");
}

describe("object storage lifecycle", () => {
  it("the storage contract can express an expiry", () => {
    const contract = read("contracts/storage.ts");
    assert.match(
      contract,
      /expireAfterDays\?:\s*number/,
      "ObjectStorageSpec declares no expireAfterDays; a bucket cannot ask for retention it has no field for",
    );
  });

  it("the DigitalOcean provider translates the expiry into a lifecycle rule", () => {
    const provider = read("providers/digitalocean/storage.ts");
    assert.match(
      provider,
      /lifecycleRules/,
      "SpacesBucket is created with versioning and no lifecycleRules; nothing expires production objects",
    );
    assert.match(
      provider,
      /expireAfterDays/,
      "the provider ignores the spec's expiry",
    );
  });

  it("the builds bucket declares a retention window", () => {
    const index = read("index.ts");
    const buildsBlock = index.slice(index.indexOf("const buildsBucket"));
    const decl = buildsBlock.slice(0, buildsBlock.indexOf("});"));
    assert.match(
      decl,
      /expireAfterDays:\s*\d+/,
      "the builds bucket — one 500 MB bundle per commit — declares no expiry, " +
        "so production retention exists only as an ILM rule applied by a Taskfile " +
        "against the in-cluster MinIO, which is not where production objects live",
    );
  });

  it("the artifacts bucket is left unexpired on purpose", () => {
    const index = read("index.ts");
    const block = index.slice(index.indexOf("const storageBucket"));
    const decl = block.slice(0, block.indexOf("});"));
    assert.ok(
      !/expireAfterDays/.test(decl),
      "artifacts is the versioned bucket and holds released artifacts; expiring it " +
        "would delete things a rollback may still need. If this ever changes, it is a " +
        "decision that needs its own record, not a default.",
    );
  });
});
