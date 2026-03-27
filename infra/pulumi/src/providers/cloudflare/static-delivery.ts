// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
  /** Cloudflare zone ID for the domain (e.g., loopzero.example) */
  readonly zoneId: string;
  /** Base domain for app hostnames (e.g., "dev.loopzero.example") */
  readonly baseDomain: string;
  /** DO Spaces endpoint for static assets (e.g., "https://loopzero-static-assets.sfo3.digitaloceanspaces.com") */
  readonly s3Endpoint: pulumi.Input<string>;
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
  const kvNamespace = new cloudflare.WorkersKvNamespace("loopzero-routing", {
    accountId: config.accountId,
    title: "loopzero-routing",
  }, {
    // Import existing KV namespace created before Pulumi management.
    // Safe to remove after first successful `pulumi up`.
    import: "362767409c4de7fda7990c10103efdd9/28992ed1178f4cbea95c2550784c69b8",
  });

  // 2. Worker script — read template and inject config values
  const workerTemplate = fs.readFileSync(
    path.resolve(__dirname, "../../../../cloudflare/worker.mjs"),
    "utf-8",
  );

  // Replace config constants in the worker template
  const workerContent = pulumi
    .all([pulumi.output(config.s3Endpoint)])
    .apply(([s3Endpoint]) => {
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

  const scriptName = "loopzero-static-delivery";

  const workerScript = new cloudflare.WorkerScript(scriptName, {
    accountId: config.accountId,
    scriptName: scriptName,
    content: workerContent,
    compatibilityDate: "2025-01-17",
    bindings: [
      {
        name: "LOOPZERO_ROUTING",
        type: "kv_namespace",
        namespaceId: kvNamespace.id,
      },
    ],
  }, {
    // Import existing Worker script created before Pulumi management.
    // Safe to remove after first successful `pulumi up`.
    import: `362767409c4de7fda7990c10103efdd9/loopzero-static-delivery`,
    // The CF API rejects WorkerScript updates via Pulumi when using
    // service worker syntax + KV bindings. The Worker is managed via
    // the worker.mjs file and deployed via CF API in the bootstrap step.
    // Pulumi owns the resource for state tracking only.
    ignoreChanges: ["content", "bindings", "compatibilityDate"],
  });

  // 3. Worker route — intercept all requests on *.{baseDomain}/*
  new cloudflare.WorkersRoute(
    "loopzero-static-delivery-route",
    {
      zoneId: config.zoneId,
      pattern: `*.${config.baseDomain}/*`,
      script: scriptName,
    },
    {
      dependsOn: [workerScript],
      // Import existing route created before Pulumi management.
      import: "f5456560bf4d4e65ac5899f608b8b180",
    },
  );

  return {
    kvNamespaceId: kvNamespace.id,
    workerName: scriptName,
  };
}
