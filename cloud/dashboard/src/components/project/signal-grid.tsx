import { Card } from '@loopzero/ui';
import {
  Activity,
  AlertTriangle,
  Bell,
  CheckCircle,
  Clock,
  Cpu,
  GitBranch,
  TrendingUp,
} from 'lucide-react';
import type { SignalName } from '@/hooks/use-status-stream';

/**
 * SignalGrid — Plan J-18 T3.2.
 *
 * 8 SignalPills laid out in a responsive grid (2 cols mobile, 4 cols desktop).
 * Each pill represents one of the 8 canonical signals from T0.2 catalog.
 *
 * Click on a pill emits `onDrillDown(signalName)` so the parent page can open
 * SignalDetailSheet (T3.3) for the 7d histogram + Grafana link.
 *
 * Honest empty state: undefined/null values render as "—" per ADR D1
 * (EC-1/EC-8 absorption — never silently 0).
 *
 * Color-by-state: per-signal tone based on canonical thresholds (uptime,
 * error rate, replicas count, alerts active, status). Renders 8 cells
 * regardless of partial input — defensively shows "—" rather than blanking.
 */
export interface SignalValues {
  status?: 'Healthy' | 'Progressing' | 'Degraded' | 'Unknown' | string | null;
  replicas?: number | null;
  uptime_24h?: number | null;
  p95_latency?: number | null;
  error_rate?: number | null;
  rps?: number | null;
  alerts_active?: number | null;
  last_deploy_time?: string | null;
}

export interface SignalGridProps {
  signals: SignalValues;
  onDrillDown?: (signal: SignalName) => void;
}

type Tone = 'success' | 'warning' | 'destructive' | 'muted';

const TONE_CLASSES: Record<Tone, string> = {
  success: 'text-success',
  warning: 'text-warning',
  destructive: 'text-destructive',
  muted: 'text-muted-foreground',
};

interface PillCfg {
  signal: SignalName;
  label: string;
  icon: typeof Cpu;
  format: (value: unknown) => string;
  tone: (value: unknown) => Tone;
}

function fmtNumber(v: unknown, suffix = ''): string {
  if (v === null || v === undefined) return '—';
  return `${v}${suffix}`;
}

function fmtPercent(v: unknown): string {
  if (typeof v !== 'number') return '—';
  return `${v.toFixed(2)}%`;
}

function fmtMs(v: unknown): string {
  if (typeof v !== 'number') return '—';
  return `${v}ms`;
}

function fmtIsoRelative(v: unknown): string {
  if (typeof v !== 'string' || v === '') return '—';
  // Relative-time render via Intl — simple "Xs ago" / "Xm ago" / "Xh ago".
  // Browser-local timezone display per plan v1.1 T2.5 spec.
  const ts = new Date(v).getTime();
  if (Number.isNaN(ts)) return '—';
  const ageSec = Math.max(0, Math.floor((Date.now() - ts) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}

const SIGNAL_PILLS: PillCfg[] = [
  {
    signal: 'status',
    label: 'Status',
    icon: CheckCircle,
    format: (v) => (typeof v === 'string' && v !== '' ? v : '—'),
    tone: (v) => {
      if (v === 'Healthy') return 'success';
      if (v === 'Progressing') return 'warning';
      if (v === 'Degraded') return 'destructive';
      return 'muted';
    },
  },
  {
    signal: 'replicas',
    label: 'Replicas',
    icon: Cpu,
    format: (v) => fmtNumber(v),
    tone: (v) => {
      if (typeof v !== 'number') return 'muted';
      if (v === 0) return 'destructive';
      return 'success';
    },
  },
  {
    signal: 'uptime_24h',
    label: 'Uptime 24h',
    icon: Activity,
    format: fmtPercent,
    tone: (v) => {
      if (typeof v !== 'number') return 'muted';
      if (v >= 99.9) return 'success';
      if (v >= 99.0) return 'warning';
      return 'destructive';
    },
  },
  {
    signal: 'p95_latency',
    label: 'p95 Latency',
    icon: Clock,
    format: fmtMs,
    tone: (v) => {
      if (typeof v !== 'number') return 'muted';
      if (v >= 1000) return 'destructive';
      if (v >= 500) return 'warning';
      return 'success';
    },
  },
  {
    signal: 'error_rate',
    label: 'Error Rate',
    icon: AlertTriangle,
    format: fmtPercent,
    tone: (v) => {
      if (typeof v !== 'number') return 'muted';
      if (v >= 5) return 'destructive';
      if (v >= 1) return 'warning';
      return 'success';
    },
  },
  {
    signal: 'rps',
    label: 'RPS',
    icon: TrendingUp,
    format: (v) => fmtNumber(v),
    tone: (v) => (typeof v === 'number' ? 'success' : 'muted'),
  },
  {
    signal: 'alerts_active',
    label: 'Alerts',
    icon: Bell,
    format: (v) => fmtNumber(v),
    tone: (v) => {
      if (typeof v !== 'number') return 'muted';
      if (v >= 1) return 'destructive';
      return 'success';
    },
  },
  {
    signal: 'last_deploy_time',
    label: 'Last deploy',
    icon: GitBranch,
    format: fmtIsoRelative,
    tone: (v) => (typeof v === 'string' && v !== '' ? 'muted' : 'muted'),
  },
];

interface SignalPillRenderProps {
  cfg: PillCfg;
  value: unknown;
  onClick?: () => void;
}

function SignalPill({ cfg, value, onClick }: SignalPillRenderProps) {
  const Icon = cfg.icon;
  const tone = cfg.tone(value);
  const formatted = cfg.format(value);
  const clickable = !!onClick;

  return (
    <Card
      role={clickable ? 'button' : 'group'}
      tabIndex={clickable ? 0 : -1}
      aria-label={`${cfg.label}: ${formatted}`}
      onClick={onClick}
      onKeyDown={(e) => {
        if (clickable && (e.key === 'Enter' || e.key === ' ')) {
          e.preventDefault();
          onClick?.();
        }
      }}
      className={`flex items-start gap-3 p-3 ${clickable ? 'cursor-pointer transition-colors hover:bg-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring' : ''}`}
    >
      <Icon className={`size-4 shrink-0 ${TONE_CLASSES[tone]}`} aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="text-label uppercase text-muted-foreground">{cfg.label}</p>
        <p className={`mt-1 font-mono text-body-sm font-medium tabular-nums ${TONE_CLASSES[tone]}`}>
          {formatted}
        </p>
      </div>
    </Card>
  );
}

export function SignalGrid({ signals, onDrillDown }: SignalGridProps) {
  return (
    <div
      className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4"
      role="group"
      aria-label="Project signal grid"
    >
      {SIGNAL_PILLS.map((cfg) => {
        const value = (signals as Record<string, unknown>)[cfg.signal];
        return (
          <SignalPill
            key={cfg.signal}
            cfg={cfg}
            value={value}
            onClick={onDrillDown ? () => onDrillDown(cfg.signal) : undefined}
          />
        );
      })}
    </div>
  );
}
