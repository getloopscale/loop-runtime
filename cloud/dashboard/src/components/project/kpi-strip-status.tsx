import { Card } from '@loopzero/ui';
import { Activity, AlertTriangle, Cpu, Clock, TrendingUp } from 'lucide-react';

/**
 * KPIStripStatus — Plan J-18 T3.1.
 *
 * 5-card horizontal KPI strip for the project Status page (Vercel-style).
 * Each card shows: label, big tabular-nums value, optional delta arrow,
 * optional spark-line (placeholder; chart rendering deferred to T3.3.b).
 *
 * Honest empty state: `null`/`undefined` values render as em-dash "—" not 0
 * (per ADR D1 of plan v1.1 + EC-8 absorption — never pretend 0 means no
 * traffic vs no data).
 *
 * Signals shown (subset of 8 SignalGrid pills):
 *   - Replicas (count, tabular)
 *   - Uptime % over 24h (tabular)
 *   - p95 latency (ms, tabular)
 *   - Error rate % (tabular)
 *   - RPS (rate, tabular)
 *
 * The remaining 3 signals (status, alerts_active, last_deploy_time) are
 * rendered in the SignalGrid (T3.2) as pills rather than KPI cards because
 * they are categorical/timestamp display, not numeric KPIs.
 */
export interface KPIStripStatusProps {
  /** Number of ready replicas; null → "—" honest. */
  replicas?: number | null;
  /** Uptime % over 24h (0..100); null → "—". */
  uptime24h?: number | null;
  /** p95 latency in ms; null → "—". */
  p95LatencyMs?: number | null;
  /** Error rate % over 5min window (0..100); null → "—". */
  errorRate?: number | null;
  /** Requests per second over 1min window; null → "—". */
  rps?: number | null;
}

type Tone = 'success' | 'warning' | 'destructive' | 'muted';

const TONE_CLASSES: Record<Tone, string> = {
  success: 'text-success',
  warning: 'text-warning',
  destructive: 'text-destructive',
  muted: 'text-muted-foreground',
};

interface KPICardProps {
  icon: typeof Cpu;
  label: string;
  value: string;
  tone: Tone;
}

function KPICard({ icon: Icon, label, value, tone }: KPICardProps) {
  return (
    <Card className="flex items-start gap-3 p-4">
      <Icon className={`size-4 shrink-0 ${TONE_CLASSES[tone]}`} aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="text-label uppercase text-muted-foreground">{label}</p>
        <p className={`mt-1 font-mono text-body-lg font-semibold tabular-nums ${TONE_CLASSES[tone]}`}>
          {value}
        </p>
      </div>
    </Card>
  );
}

function formatNullable(value: number | null | undefined, suffix = ''): string {
  if (value === null || value === undefined) return '—';
  return `${value}${suffix}`;
}

function uptimeTone(uptime: number | null | undefined): Tone {
  if (uptime === null || uptime === undefined) return 'muted';
  if (uptime >= 99.9) return 'success';
  if (uptime >= 99.0) return 'warning';
  return 'destructive';
}

function errorRateTone(rate: number | null | undefined): Tone {
  if (rate === null || rate === undefined) return 'muted';
  if (rate >= 5) return 'destructive';
  if (rate >= 1) return 'warning';
  return 'success';
}

function p95Tone(latencyMs: number | null | undefined): Tone {
  if (latencyMs === null || latencyMs === undefined) return 'muted';
  if (latencyMs >= 1000) return 'destructive';
  if (latencyMs >= 500) return 'warning';
  return 'success';
}

export function KPIStripStatus(props: KPIStripStatusProps) {
  const { replicas, uptime24h, p95LatencyMs, errorRate, rps } = props;

  return (
    <div
      className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5"
      role="group"
      aria-label="Project KPI strip"
    >
      <KPICard
        icon={Cpu}
        label="Replicas"
        value={formatNullable(replicas)}
        tone={replicas === null || replicas === undefined ? 'muted' : replicas === 0 ? 'destructive' : 'success'}
      />
      <KPICard
        icon={Activity}
        label="Uptime 24h"
        value={uptime24h === null || uptime24h === undefined ? '—' : `${uptime24h.toFixed(2)}%`}
        tone={uptimeTone(uptime24h)}
      />
      <KPICard
        icon={Clock}
        label="p95 Latency"
        value={p95LatencyMs === null || p95LatencyMs === undefined ? '—' : `${p95LatencyMs}ms`}
        tone={p95Tone(p95LatencyMs)}
      />
      <KPICard
        icon={AlertTriangle}
        label="Error Rate"
        value={errorRate === null || errorRate === undefined ? '—' : `${errorRate.toFixed(2)}%`}
        tone={errorRateTone(errorRate)}
      />
      <KPICard
        icon={TrendingUp}
        label="RPS"
        value={formatNullable(rps)}
        tone={rps === null || rps === undefined ? 'muted' : 'success'}
      />
    </div>
  );
}
