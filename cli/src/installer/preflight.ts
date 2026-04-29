// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP — T5.1
// Preflight checks executed BEFORE provisioning. Detects problems early
// (missing tokens, quota issues, tools, DNS access) so we don't waste 15-90 min
// on a doomed `pulumi up` run.

import { execSync } from 'node:child_process';

export type PreflightSeverity = 'BLOCK' | 'WARN' | 'INFO';

export interface PreflightCheck {
  name: string;
  severity: PreflightSeverity;
  status: 'pass' | 'fail' | 'warn' | 'info' | 'skipped';
  detail?: string;
  remediation?: string;
}

export interface PreflightOptions {
  /** Cloud provider being used (digitalocean | aws | k3d) — controls DO/CF checks */
  provider?: 'digitalocean' | 'aws' | 'k3d';
  /** Cloudflare account/zone IDs to verify against */
  cfAccountId?: string;
  cfZoneId?: string;
  /** Minimum DO droplet quota required (default 12) */
  dropletQuotaMin?: number;
}

export interface PreflightResult {
  ok: boolean;
  checks: PreflightCheck[];
  blockers: PreflightCheck[];
  warnings: PreflightCheck[];
}

function tryExec(cmd: string): { ok: boolean; stdout: string; stderr: string } {
  try {
    const stdout = execSync(cmd, { encoding: 'utf-8', timeout: 10_000, stdio: ['ignore', 'pipe', 'pipe'] });
    return { ok: true, stdout: stdout.toString(), stderr: '' };
  } catch (err: unknown) {
    const e = err as { stdout?: Buffer; stderr?: Buffer };
    return { ok: false, stdout: e.stdout?.toString() ?? '', stderr: e.stderr?.toString() ?? String(err) };
  }
}

function check(name: string, severity: PreflightSeverity, run: () => Omit<PreflightCheck, 'name' | 'severity'>): PreflightCheck {
  try {
    return { name, severity, ...run() };
  } catch (err) {
    return { name, severity, status: 'fail', detail: `unexpected error: ${err}` };
  }
}

export function runPreflight(opts: PreflightOptions = {}): PreflightResult {
  const checks: PreflightCheck[] = [];

  // 1. Tooling — always required
  checks.push(check('helmfile installed (v1.0+)', 'BLOCK', () => {
    const r = tryExec('helmfile --version');
    return r.ok
      ? { status: 'pass', detail: r.stdout.trim() }
      : { status: 'fail', detail: 'helmfile not found', remediation: 'https://helmfile.readthedocs.io/en/latest/#installation' };
  }));

  checks.push(check('helm installed (v3.14+)', 'BLOCK', () => {
    const r = tryExec('helm version --short');
    return r.ok
      ? { status: 'pass', detail: r.stdout.trim() }
      : { status: 'fail', detail: 'helm not found', remediation: 'https://helm.sh/docs/intro/install/' };
  }));

  checks.push(check('kubectl installed', 'BLOCK', () => {
    const r = tryExec('kubectl version --client -o yaml');
    return r.ok ? { status: 'pass' } : { status: 'fail', detail: 'kubectl not found', remediation: 'https://kubernetes.io/docs/tasks/tools/' };
  }));

  checks.push(check('cosign installed', 'WARN', () => {
    const r = tryExec('cosign version');
    return r.ok
      ? { status: 'pass', detail: r.stdout.split('\n')[0] }
      : { status: 'warn', detail: 'cosign not found — required only for image signing', remediation: 'https://docs.sigstore.dev/cosign/installation/' };
  }));

  checks.push(check('disk space', 'WARN', () => {
    const r = tryExec("df -BG --output=avail . | tail -n1 | tr -d ' G'");
    if (!r.ok) return { status: 'warn', detail: 'unable to check disk' };
    const free = parseInt(r.stdout.trim(), 10);
    return free >= 5
      ? { status: 'pass', detail: `${free}G free` }
      : { status: 'warn', detail: `only ${free}G free (recommended: 5G+)` };
  }));

  // 2. Provider-specific
  if (opts.provider === 'digitalocean') {
    checks.push(check('pulumi installed', 'BLOCK', () => {
      const r = tryExec('pulumi version');
      return r.ok
        ? { status: 'pass', detail: r.stdout.trim() }
        : { status: 'fail', detail: 'pulumi not found', remediation: 'https://www.pulumi.com/docs/install/' };
    }));

    checks.push(check('doctl installed', 'BLOCK', () => {
      const r = tryExec('doctl version');
      return r.ok
        ? { status: 'pass' }
        : { status: 'fail', detail: 'doctl not found', remediation: 'https://docs.digitalocean.com/reference/doctl/how-to/install/' };
    }));

    checks.push(check('DO API token valid', 'BLOCK', () => {
      const r = tryExec('doctl account get --no-header -o json');
      return r.ok
        ? { status: 'pass' }
        : { status: 'fail', detail: 'DO API token invalid or missing', remediation: 'doctl auth init' };
    }));

    checks.push(check('DO droplet quota', 'BLOCK', () => {
      const min = opts.dropletQuotaMin ?? 12;
      const r = tryExec("doctl account get -o json | jq -r '.[0].droplet_limit'");
      if (!r.ok) return { status: 'fail', detail: 'unable to query quota' };
      const limit = parseInt(r.stdout.trim(), 10);
      return limit >= min
        ? { status: 'pass', detail: `${limit} droplets (>= ${min})` }
        : { status: 'fail', detail: `${limit} droplets < ${min}`, remediation: 'Request quota bump in DO console' };
    }));

    if (opts.cfAccountId) {
      checks.push(check('CF API token valid', 'BLOCK', () => {
        const token = process.env.CLOUDFLARE_API_TOKEN ?? '';
        if (!token) return { status: 'fail', detail: 'CLOUDFLARE_API_TOKEN env var not set' };
        const r = tryExec(`curl -sf -H 'Authorization: Bearer ${token}' https://api.cloudflare.com/client/v4/user/tokens/verify`);
        return r.ok && r.stdout.includes('"success":true')
          ? { status: 'pass' }
          : { status: 'fail', detail: 'CF token invalid', remediation: 'Recreate token at https://dash.cloudflare.com/profile/api-tokens' };
      }));

      if (opts.cfZoneId) {
        checks.push(check('CF zone access', 'BLOCK', () => {
          const token = process.env.CLOUDFLARE_API_TOKEN ?? '';
          const r = tryExec(`curl -sf -H 'Authorization: Bearer ${token}' https://api.cloudflare.com/client/v4/zones/${opts.cfZoneId}`);
          return r.ok && r.stdout.includes('"success":true')
            ? { status: 'pass' }
            : { status: 'fail', detail: 'cannot access CF zone with current token' };
        }));
      }
    }
  }

  // 3. Adopt-or-create info (non-blocking)
  if (opts.provider === 'digitalocean') {
    checks.push(check('existing DO resources', 'INFO', () => {
      const r = tryExec("doctl kubernetes cluster list --no-header --format Name,Region 2>/dev/null | wc -l");
      const count = r.ok ? parseInt(r.stdout.trim(), 10) : 0;
      return count > 0
        ? { status: 'info', detail: `${count} existing DOKS clusters detected — adopt-or-create will reuse where possible` }
        : { status: 'info', detail: 'no existing DOKS clusters — fresh provisioning' };
    }));
  }

  const blockers = checks.filter((c) => c.severity === 'BLOCK' && c.status === 'fail');
  const warnings = checks.filter((c) => c.status === 'warn');
  return {
    ok: blockers.length === 0,
    checks,
    blockers,
    warnings,
  };
}

/**
 * Pretty-print preflight results to a write callback.
 */
export function formatPreflightResult(result: PreflightResult, writeTo: (msg: string) => void): void {
  for (const c of result.checks) {
    let icon = '✓';
    if (c.status === 'fail') icon = '✗';
    else if (c.status === 'warn') icon = '!';
    else if (c.status === 'info') icon = 'ℹ';
    else if (c.status === 'skipped') icon = '-';
    const label = `[${c.severity}]`.padEnd(8);
    const detail = c.detail ? ` — ${c.detail}` : '';
    writeTo(`  ${icon} ${label} ${c.name}${detail}`);
    if (c.status === 'fail' && c.remediation) {
      writeTo(`         remediation: ${c.remediation}`);
    }
  }
  if (result.blockers.length > 0) {
    writeTo('');
    writeTo(`Preflight FAILED — ${result.blockers.length} blocking issue(s) above.`);
  } else if (result.warnings.length > 0) {
    writeTo('');
    writeTo(`Preflight passed with ${result.warnings.length} warning(s).`);
  }
}
