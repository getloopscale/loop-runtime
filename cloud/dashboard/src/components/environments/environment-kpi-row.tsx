import { Activity, Clock, Layers } from 'lucide-react';

interface EnvironmentKPIRowProps {
  /** Number of replicas currently ready in this env. undefined → empty. */
  replicas?: number | null;
  /** Uptime % over last 24h. undefined → empty. */
  uptime?: number | null;
  /** ISO timestamp of last successful deploy. undefined → empty. */
  lastDeploy?: string | null;
  /** Short commit SHA of last deploy, linkable to /deployments/<sha>. */
  lastDeploySha?: string | null;
}

/**
 * EnvironmentKPIRow — Plan J-10 v1.1 T2.4 (NEW).
 *
 * Inline KPI strip for an environment row in the /environments table.
 * Three signals v1.0 per blueprint § 2.3 + ADR D4:
 *   - replicas (count, tabular)
 *   - uptime % over 24h (tabular)
 *   - last_deploy timestamp (relative) + sha link
 *
 * Empty state: each cell shows "—" honest when value is undefined/null
 * (not 0 — blueprint § 2.3 "honest empty" pattern: avoids users misreading
 * 0 as "no traffic" vs "no data").
 *
 * Future signals (J-18 will cover): error_rate, build_duration, cost.
 *
 * SSE multiplex (D4): the values come from a page-level SSE stream
 * filtered per env_id — single connection per origin (browser EventSource
 * limit 6 on HTTP/1.1). This component is dumb: just renders props.
 */
export function EnvironmentKPIRow({
  replicas,
  uptime,
  lastDeploy,
  lastDeploySha,
}: EnvironmentKPIRowProps) {
  return (
    <div className="flex items-center gap-4 text-label">
      <span
        className="inline-flex items-center gap-1 tabular-nums text-muted-foreground"
        title="Ready replicas"
      >
        <Layers className="size-3" aria-hidden="true" />
        <span>{formatReplicas(replicas)}</span>
      </span>
      <span
        className="inline-flex items-center gap-1 tabular-nums text-muted-foreground"
        title="Uptime over last 24h"
      >
        <Activity className="size-3" aria-hidden="true" />
        <span>{formatUptime(uptime)}</span>
      </span>
      <span
        className="inline-flex items-center gap-1 tabular-nums text-muted-foreground"
        title={
          lastDeploy
            ? `Last deploy: ${new Date(lastDeploy).toLocaleString()}`
            : 'No deploys yet'
        }
      >
        <Clock className="size-3" aria-hidden="true" />
        <span>{formatLastDeploy(lastDeploy, lastDeploySha)}</span>
      </span>
    </div>
  );
}

function formatReplicas(replicas?: number | null): string {
  if (replicas === undefined || replicas === null) return '—';
  return `${replicas} replica${replicas !== 1 ? 's' : ''}`;
}

function formatUptime(uptime?: number | null): string {
  if (uptime === undefined || uptime === null) return '—';
  if (uptime >= 100) return '100%';
  return `${uptime.toFixed(2)}%`;
}

function formatLastDeploy(
  lastDeploy?: string | null,
  sha?: string | null,
): string {
  if (!lastDeploy) return '—';
  const ts = new Date(lastDeploy).getTime();
  if (Number.isNaN(ts)) return '—';
  const deltaMs = Date.now() - ts;
  const rel = relativeFromMs(deltaMs);
  return sha ? `${rel} (${sha.slice(0, 7)})` : rel;
}

function relativeFromMs(ms: number): string {
  if (ms < 0) return 'in the future';
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  return `${day}d ago`;
}
