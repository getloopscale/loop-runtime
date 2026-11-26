// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Cloudflare static frontend delivery (Sprint 10).
// Provisions: KV namespace, Worker script, Worker route.
// The Worker intercepts requests on *.{domain}, checks KV for static routing
// entries, and serves assets from DO Spaces. Non-static requests pass through
// to the Cloudflare Tunnel (container apps).

import * as pulumi from "@pulumi/pulumi";
import * as cloudflare from "@pulumi/cloudflare";
import * as fs from "fs";
import * as path from "path";

export interface StaticDeliveryConfig {
  /** Cloudflare account ID */
  readonly accountId: string;
  /** Cloudflare zone ID for the domain (e.g., getloopscale.com) */
  readonly zoneId: string;
  /** Environment name (dev, staging, prod) — used to namespace CF resources */
  readonly environment: string;
  /** Base domain for app hostnames (e.g., "dev.getloopscale.com") */
  readonly baseDomain: string;
  /** DO Spaces endpoint for static assets (e.g., "https://loopruntime-static-assets.sfo3.digitaloceanspaces.com") */
  readonly s3Endpoint: pulumi.Input<string>;
  /** Import IDs for adopt-or-create pattern (orphan resolution). */
  readonly importIds?: {
    kvNamespace?: string;
    workerScript?: string;
    workerRoute?: string;
  };
}

export interface StaticDeliveryOutput {
  /** KV namespace ID — used by API to write routing entries */
  readonly kvNamespaceId: pulumi.Output<string>;
  /** Worker script name */
  readonly workerName: string;
}

/**
 * Provisions the complete Cloudflare static delivery stack:
 * 1. KV namespace for routing table
 * 2. Worker script with KV binding
 * 3. Worker route on *.{baseDomain}/*
 */
export function createStaticDelivery(
  config: StaticDeliveryConfig,
): StaticDeliveryOutput {
  // 1. KV namespace — routing table read by the Worker on every request
  const kvTitle = `loopruntime-routing-${config.environment}`;
  const kvNamespace = new cloudflare.WorkersKvNamespace(`loopruntime-routing-${config.environment}`, {
    accountId: config.accountId,
    title: kvTitle,
  }, config.importIds?.kvNamespace ? { import: config.importIds.kvNamespace } : undefined);

  // 2. Worker script — read template and inject config values
  const workerTemplate = fs.readFileSync(
    path.resolve(__dirname, "../../../../cloudflare/worker.mjs"),
    "utf-8",
  );

  // Audit gap M05 #17 / §6.C.7b — verify the template still carries the two
  // sentinel constants we replace below BEFORE we attempt the replace. If
  // anyone renames `S3_ENDPOINT` or `BASE_DOMAIN` in worker.mjs without
  // updating the regex here, the replace silently no-ops and the deployed
  // worker carries the LITERAL dev defaults to production. That was the
  // exact silent-drift scenario §6.C.7b was carved out to prevent.
  //
  // The worker.mjs file is outside our /change scope (governance-gate
  // blocked the edit that would add an in-worker assertConfigInjected()
  // runtime guard) so we close the loop here on the Pulumi side: detect
  // the missing sentinel at deploy time and fail-fast with a clear error.
  if (!/const S3_ENDPOINT = ".*";/.test(workerTemplate)) {
    throw new Error(
      "audit M05 #17 (§6.C.7b): worker.mjs is missing the S3_ENDPOINT " +
        "constant declaration. The Pulumi regex replace would silently no-op " +
        "and the worker would carry whatever literal lives in worker.mjs. " +
        "Restore `const S3_ENDPOINT = \"...\";` at the top of " +
        "infra/cloudflare/worker.mjs OR update this safeguard to match the " +
        "new constant name.",
    );
  }
  if (!/const BASE_DOMAIN = ".*";/.test(workerTemplate)) {
    throw new Error(
      "audit M05 #17 (§6.C.7b): worker.mjs is missing the BASE_DOMAIN " +
        "constant declaration. Same failure mode as the S3_ENDPOINT check " +
        "above — restore the constant or update the regex.",
    );
  }

  // Replace config constants in the worker template.
  const workerContent = pulumi
    .all([pulumi.output(config.s3Endpoint)])
    .apply(([s3Endpoint]) => {
      // Defensive: refuse to ship a worker where the injected value is
      // empty. Either failure mode (config not set, or upstream resolution
      // returned ""; both have happened in incident history) would leave
      // production routing on an empty hostname and silently 502 every
      // request. Fail at apply-time with a loud error instead.
      if (!s3Endpoint || s3Endpoint.trim() === "") {
        throw new Error(
          "audit M05 #17 (§6.C.7b): config.s3Endpoint resolved to empty " +
            "string. Refusing to render the static-delivery worker with an " +
            "empty S3_ENDPOINT (would silently 502 every static request). " +
            "Verify the Pulumi stack output that feeds this value.",
        );
      }
      if (!config.baseDomain || config.baseDomain.trim() === "") {
        throw new Error(
          "audit M05 #17 (§6.C.7b): config.baseDomain is empty. Refusing " +
            "to render the static-delivery worker — would route every " +
            "request to *.<EMPTY> with no path match.",
        );
      }
      return workerTemplate
        .replace(
          /const S3_ENDPOINT = ".*";/,
          `const S3_ENDPOINT = "${s3Endpoint}";`,
        )
        .replace(
          /const BASE_DOMAIN = ".*";/,
          `const BASE_DOMAIN = "${config.baseDomain}";`,
        );
    });

  const scriptName = `loopruntime-static-delivery-${config.environment}`;

  const workerScript = new cloudflare.WorkersScript(scriptName, {
    accountId: config.accountId,
    scriptName: scriptName,
    content: workerContent,
    compatibilityDate: "2025-01-17",
    bindings: [
      {
        name: "LOOPRUNTIME_ROUTING",
        type: "kv_namespace",
        namespaceId: kvNamespace.id,
      },
    ],
  }, {
    // The CF API rejects WorkersScript updates via Pulumi when using
    // service worker syntax + KV bindings. The Worker is managed via
    // the worker.mjs file and deployed via CF API in the bootstrap step.
    // Pulumi owns the resource for state tracking only.
    ignoreChanges: ["content", "bindings", "compatibilityDate"],
    ...(config.importIds?.workerScript ? { import: config.importIds.workerScript } : {}),
  });

  // 3. Worker route — intercept all requests on *.{baseDomain}/*
  new cloudflare.WorkersRoute(
    `loopruntime-static-delivery-route-${config.environment}`,
    {
      zoneId: config.zoneId,
      pattern: `*.${config.baseDomain}/*`,
      script: scriptName,
    },
    {
      dependsOn: [workerScript],
      ...(config.importIds?.workerRoute ? { import: config.importIds.workerRoute } : {}),
    },
  );

  return {
    kvNamespaceId: kvNamespace.id,
    workerName: scriptName,
  };
}
