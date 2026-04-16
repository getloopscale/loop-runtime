// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { local } from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";
import { DnsRecordSpec, DnsRecordOutput, DnsRecordType } from "../../contracts/dns";
import { IProviderDns } from "../../contracts/provider";

function detectRecordType(target: string): DnsRecordType {
  if (/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(target)) {
    return "A";
  }
  if (target.includes(":")) {
    return "AAAA";
  }
  return "CNAME";
}

const UPSERT_DNS_RECORD = String.raw`node <<'NODE'
const https = require("https");

const required = (name) => {
  const value = process.env[name];
  if (!value) {
    console.error("Missing required environment variable: " + name);
    process.exit(1);
  }
  return value;
};

const token = required("CF_API_TOKEN");
const zoneId = required("CF_ZONE_ID");
const recordName = required("CF_RECORD_NAME");
const recordFqdn = required("CF_RECORD_FQDN");
const recordType = required("CF_RECORD_TYPE");
const recordContent = required("CF_RECORD_CONTENT");
const ttl = Number(required("CF_RECORD_TTL"));
const proxied = required("CF_RECORD_PROXIED") === "true";

const api = (method, path, body) => new Promise((resolve, reject) => {
  const payload = body ? JSON.stringify(body) : undefined;
  const req = https.request({
    hostname: "api.cloudflare.com",
    path,
    method,
    headers: {
      "Authorization": "Bearer " + token,
      "Content-Type": "application/json",
      ...(payload ? { "Content-Length": Buffer.byteLength(payload) } : {}),
    },
  }, (res) => {
    let data = "";
    res.setEncoding("utf8");
    res.on("data", (chunk) => { data += chunk; });
    res.on("end", () => {
      let parsed;
      try {
        parsed = data ? JSON.parse(data) : {};
      } catch (err) {
        reject(new Error("Cloudflare returned non-JSON response (" + res.statusCode + "): " + data));
        return;
      }
      if (!parsed.success) {
        reject(new Error("Cloudflare API " + method + " " + path + " failed (" + res.statusCode + "): " + JSON.stringify(parsed.errors || parsed)));
        return;
      }
      resolve(parsed);
    });
  });
  req.on("error", reject);
  if (payload) req.write(payload);
  req.end();
});

(async () => {
  const query = "/client/v4/zones/" + zoneId + "/dns_records?name=" + encodeURIComponent(recordFqdn) + "&type=" + encodeURIComponent(recordType);
  const existing = await api("GET", query);
  const record = existing.result && existing.result[0];
  const body = {
    type: recordType,
    name: recordName,
    content: recordContent,
    ttl,
    proxied,
  };
  const result = record
    ? await api("PUT", "/client/v4/zones/" + zoneId + "/dns_records/" + record.id, body)
    : await api("POST", "/client/v4/zones/" + zoneId + "/dns_records", body);
  process.stdout.write(result.result.id);
})().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
NODE`;

const DELETE_DNS_RECORD = String.raw`node <<'NODE'
const https = require("https");

const required = (name) => {
  const value = process.env[name];
  if (!value) {
    console.error("Missing required environment variable: " + name);
    process.exit(1);
  }
  return value;
};

const token = required("CF_API_TOKEN");
const zoneId = required("CF_ZONE_ID");
const recordName = required("CF_RECORD_NAME");
const recordFqdn = required("CF_RECORD_FQDN");
const recordType = required("CF_RECORD_TYPE");

const api = (method, path) => new Promise((resolve, reject) => {
  const req = https.request({
    hostname: "api.cloudflare.com",
    path,
    method,
    headers: {
      "Authorization": "Bearer " + token,
      "Content-Type": "application/json",
    },
  }, (res) => {
    let data = "";
    res.setEncoding("utf8");
    res.on("data", (chunk) => { data += chunk; });
    res.on("end", () => {
      let parsed;
      try {
        parsed = data ? JSON.parse(data) : {};
      } catch (err) {
        reject(new Error("Cloudflare returned non-JSON response (" + res.statusCode + "): " + data));
        return;
      }
      if (!parsed.success && res.statusCode !== 404) {
        reject(new Error("Cloudflare API " + method + " " + path + " failed (" + res.statusCode + "): " + JSON.stringify(parsed.errors || parsed)));
        return;
      }
      resolve(parsed);
    });
  });
  req.on("error", reject);
  req.end();
});

(async () => {
  const previousId = process.env.PULUMI_COMMAND_STDOUT || "";
  const recordId = previousId.trim();
  if (recordId) {
    await api("DELETE", "/client/v4/zones/" + zoneId + "/dns_records/" + recordId);
    return;
  }

  const query = "/client/v4/zones/" + zoneId + "/dns_records?name=" + encodeURIComponent(recordFqdn) + "&type=" + encodeURIComponent(recordType);
  const existing = await api("GET", query);
  const record = existing.result && existing.result[0];
  if (record) {
    await api("DELETE", "/client/v4/zones/" + zoneId + "/dns_records/" + record.id);
  }
})().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
NODE`;

/**
 * Creates DNS records in Cloudflare.
 *
 * Used when the domain's authoritative nameservers are Cloudflare (not the
 * cloud provider). The zoneId must be provided at construction time.
 *
 * Records are created with proxy DISABLED (DNS-only / grey cloud) so that
 * Traefik handles TLS termination with Let's Encrypt certificates.
 */
export class CloudflareDnsProvider implements IProviderDns {
  constructor(private readonly zoneId: pulumi.Input<string>) {}

  createDnsRecord(spec: DnsRecordSpec): DnsRecordOutput {
    const resolvedType: pulumi.Output<DnsRecordType> = spec.recordType
      ? pulumi.output(spec.recordType)
      : pulumi.output(spec.target).apply(detectRecordType);

    const fqdn = `${spec.recordName}.${spec.zone}`;

    const cloudflareConfig = new pulumi.Config("cloudflare");
    const apiToken = process.env.CLOUDFLARE_API_TOKEN
      ? pulumi.secret(process.env.CLOUDFLARE_API_TOKEN)
      : cloudflareConfig.requireSecret("apiToken");

    // Work around @pulumi/cloudflare v6.x / Terraform provider v5.x returning
    // Cloudflare error 9000 for otherwise-valid A record payloads. The direct
    // API call is idempotent: it updates an existing record or creates it.
    new local.Command(
      `cf-${spec.recordName}.${spec.zone}`,
      {
        create: UPSERT_DNS_RECORD,
        delete: DELETE_DNS_RECORD,
        environment: {
          CF_API_TOKEN: apiToken,
          CF_ZONE_ID: this.zoneId,
          CF_RECORD_NAME: spec.recordName,
          CF_RECORD_FQDN: fqdn,
          CF_RECORD_TYPE: resolvedType,
          CF_RECORD_CONTENT: spec.target,
          CF_RECORD_TTL: pulumi.output(spec.ttl ?? 300).apply(String),
          // DNS-only (grey cloud) — Traefik terminates TLS, not Cloudflare.
          CF_RECORD_PROXIED: "false",
        },
        triggers: [
          this.zoneId,
          spec.recordName,
          resolvedType,
          spec.target,
          spec.ttl ?? 300,
          false,
        ],
        logging: "stderr",
      },
      {
        additionalSecretOutputs: ["environment"],
      },
    );

    return {
      fqdn: pulumi.output(fqdn),
    };
  }
}
