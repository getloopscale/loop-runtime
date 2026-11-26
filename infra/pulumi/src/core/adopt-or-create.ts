// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Adopt-or-create pattern for idempotent destroy+up cycles.
//
// Problem: `pulumi destroy` + `pulumi up` fails when external resources
// survive the destroy (e.g., VPC with orphan LBs, CF tunnels, CF KV namespaces).
// On `pulumi up`, Pulumi tries to CREATE but the provider rejects the duplicate.
//
// Solution: before creating, check if the resource already exists in the provider.
// If it does, return its ID so the caller can pass `{ import: id }` to Pulumi.
// If not, return undefined (normal create path).
//
// Uses execSync+curl for synchronous HTTP in a CommonJS Pulumi program.
// Lookups are fast (~200ms each) and fail-safe (return undefined on any error).

import * as pulumi from "@pulumi/pulumi";
import { execSync } from "child_process";

function httpGet(url: string, token: string): any {
  try {
    const result = execSync(
      `curl -sf -H "Authorization: Bearer ${token}" "${url}"`,
      { encoding: "utf-8", timeout: 10000, stdio: ["pipe", "pipe", "pipe"] },
    );
    return JSON.parse(result);
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e);
    pulumi.log.warn(`[adopt-or-create] lookup failed: ${url.split("?")[0]} — ${msg.split("\n")[0]}`);
    return undefined;
  }
}

// ---------------------------------------------------------------------------
// DigitalOcean
// ---------------------------------------------------------------------------

/** Lookup an existing VPC by name. Returns the VPC ID or undefined. */
export function lookupExistingVpc(name: string): string | undefined {
  const token = process.env.DIGITALOCEAN_TOKEN;
  if (!token) return undefined;
  const data = httpGet("https://api.digitalocean.com/v2/vpcs?per_page=200", token);
  const vpc = data?.vpcs?.find((v: any) => v.name === name);
  return vpc?.id;
}

/**
 * Lookup the existing DigitalOcean Container Registry. Returns the registry
 * name (which doubles as the Pulumi import ID for `digitalocean.ContainerRegistry`)
 * or undefined if no registry is provisioned in the account.
 *
 * DigitalOcean allows ONE registry per account, so we don't filter by name —
 * we return whatever the account already has. This is the canonical adoption
 * source per ADR-0015 (Pre-bootstrap Adopt-or-Create Policy).
 */
export function lookupExistingRegistry(): string | undefined {
  const token = process.env.DIGITALOCEAN_TOKEN;
  if (!token) return undefined;
  const data = httpGet("https://api.digitalocean.com/v2/registry", token);
  return data?.registry?.name;
}

/** Lookup an existing CDN by origin domain. Returns CDN ID or undefined. */
export function lookupExistingCdn(originDomain: string): string | undefined {
  const token = process.env.DIGITALOCEAN_TOKEN;
  if (!token) return undefined;
  const data = httpGet("https://api.digitalocean.com/v2/cdn/endpoints?per_page=50", token);
  const cdn = data?.endpoints?.find((c: any) => c.origin?.includes(originDomain));
  return cdn?.id;
}

/** Lookup an existing Spaces bucket by name. Returns bucket name if exists, undefined otherwise. */
export function lookupExistingBucket(
  name: string,
  region: string,
): string | undefined {
  try {
    // HEAD the bucket endpoint: 200/403 = exists, 404 = not found.
    // NOTE: do NOT use -f flag — curl treats 403 (private bucket) as failure.
    const result = execSync(
      `curl -s -o /dev/null -w "%{http_code}" "https://${region}.digitaloceanspaces.com/${name}"`,
      { encoding: "utf-8", timeout: 10000, stdio: ["pipe", "pipe", "pipe"] },
    );
    const status = parseInt(result.trim(), 10);
    return (status === 200 || status === 403) ? name : undefined;
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e);
    pulumi.log.warn(`[adopt-or-create] bucket lookup failed: ${name} in ${region} — ${msg.split("\n")[0]}`);
    return undefined;
  }
}

// ---------------------------------------------------------------------------
// Cloudflare
// ---------------------------------------------------------------------------

/** Lookup an existing CF tunnel by name. Returns `accountId/tunnelId` or undefined. */
export function lookupExistingTunnel(
  tunnelName: string,
  accountId: string,
): string | undefined {
  const token = process.env.CLOUDFLARE_API_TOKEN;
  if (!token) return undefined;
  const data = httpGet(
    `https://api.cloudflare.com/client/v4/accounts/${accountId}/cfd_tunnel?name=${encodeURIComponent(tunnelName)}&is_deleted=false`,
    token,
  );
  const tunnel = data?.result?.[0];
  return tunnel ? `${accountId}/${tunnel.id}` : undefined;
}

/** Lookup an existing CF KV namespace by title. Returns `accountId/namespaceId` or undefined. */
export function lookupExistingKvNamespace(
  title: string,
  accountId: string,
): string | undefined {
  const token = process.env.CLOUDFLARE_API_TOKEN;
  if (!token) return undefined;
  const data = httpGet(
    `https://api.cloudflare.com/client/v4/accounts/${accountId}/storage/kv/namespaces?per_page=100`,
    token,
  );
  const ns = data?.result?.find((n: any) => n.title === title);
  return ns ? `${accountId}/${ns.id}` : undefined;
}

/** Lookup an existing CF DNS record. Returns `zoneId/recordId` or undefined. */
export function lookupExistingDnsRecord(
  zoneId: string,
  name: string,
  type: string,
): string | undefined {
  const token = process.env.CLOUDFLARE_API_TOKEN;
  if (!token) return undefined;
  const data = httpGet(
    `https://api.cloudflare.com/client/v4/zones/${zoneId}/dns_records?name=${encodeURIComponent(name)}&type=${type}`,
    token,
  );
  const record = data?.result?.[0];
  return record ? `${zoneId}/${record.id}` : undefined;
}

/** Lookup an existing CF Workers script. Returns `accountId/scriptName` or undefined. */
export function lookupExistingWorkerScript(
  scriptName: string,
  accountId: string,
): string | undefined {
  const token = process.env.CLOUDFLARE_API_TOKEN;
  if (!token) return undefined;
  const data = httpGet(
    `https://api.cloudflare.com/client/v4/accounts/${accountId}/workers/scripts`,
    token,
  );
  const script = data?.result?.find((s: any) => s.id === scriptName);
  return script ? `${accountId}/${scriptName}` : undefined;
}

/** Lookup an existing CF Workers route by pattern. Returns `zoneId/routeId` or undefined. */
export function lookupExistingWorkerRoute(
  zoneId: string,
  pattern: string,
): string | undefined {
  const token = process.env.CLOUDFLARE_API_TOKEN;
  if (!token) return undefined;
  const data = httpGet(
    `https://api.cloudflare.com/client/v4/zones/${zoneId}/workers/routes`,
    token,
  );
  const route = data?.result?.find((r: any) => r.pattern === pattern);
  return route ? `${zoneId}/${route.id}` : undefined;
}

// ---------------------------------------------------------------------------
// Orchestrator — collects all lookups in one call
// ---------------------------------------------------------------------------

export interface OrphanLookupResult {
  vpcId?: string;
  registryImportId?: string;
  tunnelImportId?: string;
  kvNamespaceImportId?: string;
  dnsRecordImportId?: string;
  workerScriptImportId?: string;
  workerRouteImportId?: string;
}

/**
 * Runs all adopt-or-create lookups synchronously at program start.
 * Returns import IDs for any pre-existing resources (orphans from partial destroy).
 * Returns undefined values for resources that don't exist (normal create path).
 *
 * Call this ONCE at the top of index.ts before declaring any Pulumi resources.
 */
export function resolveOrphans(opts: {
  vpcName: string;
  tunnelName: string;
  kvTitle: string;
  dnsWildcard: string;
  workerScriptName: string;
  workerRoutePattern: string;
  cfAccountId: string;
  cfZoneId: string;
}): OrphanLookupResult {
  return {
    vpcId: lookupExistingVpc(opts.vpcName),
    registryImportId: lookupExistingRegistry(),
    tunnelImportId: lookupExistingTunnel(opts.tunnelName, opts.cfAccountId),
    kvNamespaceImportId: lookupExistingKvNamespace(opts.kvTitle, opts.cfAccountId),
    dnsRecordImportId: lookupExistingDnsRecord(opts.cfZoneId, opts.dnsWildcard, "CNAME"),
    workerScriptImportId: lookupExistingWorkerScript(opts.workerScriptName, opts.cfAccountId),
    workerRouteImportId: lookupExistingWorkerRoute(opts.cfZoneId, opts.workerRoutePattern),
  };
}
