import { AlertTriangle, Cpu, Rocket, TrendingUp } from 'lucide-react';
import { Card } from '@loopzero/ui';
import type { ProjectStatusDetail } from '@/lib/api/types';

interface ProjectHealthCardProps {
  status: ProjectStatusDetail;
}

interface SignalPillProps {
  icon: typeof Cpu;
  label: string;
  value: string;
  tone: 'success' | 'warning' | 'destructive' | 'muted';
}

const TONE_CLASSES: Record<SignalPillProps['tone'], string> = {
  success: 'text-success',
  warning: 'text-warning',
  destructive: 'text-destructive',
  muted: 'text-muted-foreground',
};

function SignalPill({ icon: Icon, label, value, tone }: SignalPillProps) {
  return (
    <div className="flex items-start gap-3 rounded-md border border-border/60 p-3">
      <Icon className={`size-4 shrink-0 ${TONE_CLASSES[tone]}`} aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="text-label uppercase text-muted-foreground">{label}</p>
        <p className={`mt-1 font-mono text-body-sm font-medium ${TONE_CLASSES[tone]}`}>
          {value}
        </p>
      </div>
    </div>
  );
}

function replicasToneFor(ready: number, desired: number): SignalPillProps['tone'] {
  if (desired === 0) return 'muted';
  if (ready === desired) return 'success';
  if (ready === 0) return 'destructive';
  return 'warning';
}

function deployToneFor(s: string): SignalPillProps['tone'] {
  if (s === 'succeeded') return 'success';
  if (s === 'failed') return 'destructive';
  if (s === 'running' || s === 'queued') return 'warning';
  return 'muted';
}

function trafficToneFor(pct: number): SignalPillProps['tone'] {
  if (pct >= 5) return 'destructive';
  if (pct >= 1) return 'warning';
  return 'success';
}

function alertsToneFor(n: number): SignalPillProps['tone'] {
  if (n >= 1) return 'destructive';
  return 'success';
}

/**
 * ProjectHealthCard — Plan v1.0 T2.2 composite.
 *
 * 4 health signals as pills: replicas, deploy status, traffic 5xx rate,
 * active alerts. Each pill has window context inline ("1h", "ready/desired")
 * so values are never ambiguous.
 */
export function ProjectHealthCard({ status }: ProjectHealthCardProps) {
  // T5.3 LIVE-bug UX fix (2026-06-08, plan 2026-06-08-dashboard-screens-review-fixes):
  // when ALL 4 health signal fields are omitted by the backend (omitempty
  // per status.go), the card previously rendered 4 orphan "—" pills,
  // which sponsor read as "data missing — broken card" rather than the
  // canonical empty state. Caught during sponsor walkthrough.
  //
  // CORRECT detection (refined iter 19): condition on the 4 signal fields
  // alone, NOT on active_release. Two empty-state shapes exist in the wild:
  //   (a) Nothing deployed: active_release === undefined/null  + all 4 signals undefined
  //       → message: "No active release yet — deploy this project to populate"
  //   (b) Release active but metrics not yet collected: active_release
  //       populated + all 4 signals still undefined (Prometheus scrape gap,
  //       new pod just rolled, etc.)
  //       → message: "Release active but metrics are not yet available — try in a moment"
  // The earlier iter 18 commit (9d46d0ec) gated on `!status.active_release`
  // which only handled (a); shape (b) still rendered 4 dashes.
  //
  // Pills still mount when ANY signal is present (partial-data state — a
  // failed build that populated deploy_status but left replicas null is
  // honestly partial and the per-cell "—" IS the canonical signal).
  const allSignalsMissing =
    status.replicas_ready === undefined &&
    status.deploy_status === undefined &&
    status.traffic_5xx_pct_1h === undefined &&
    status.alerts_active === undefined;

  if (allSignalsMissing) {
    const hasActiveRelease = Boolean(status.active_release);
    const message = hasActiveRelease
      ? `Release active in ${status.environment} but metrics are not yet available — try refreshing in a moment.`
      : `No active release in ${status.environment} yet — health signals appear after the first successful deploy.`;
    return (
      <Card>
        <Card.Header>
          <Card.Title>Health</Card.Title>
          <Card.Description>
            {status.project} · {status.environment}
          </Card.Description>
        </Card.Header>
        <Card.Body>
          <p className="text-body-sm text-muted-foreground">{message}</p>
        </Card.Body>
      </Card>
    );
  }

  return (
    <Card>
      <Card.Header>
        <Card.Title>Health</Card.Title>
        <Card.Description>
          {status.project} · {status.environment}
        </Card.Description>
      </Card.Header>
      <Card.Body>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <SignalPill
            icon={Cpu}
            label="Replicas ready"
            value={
              status.replicas_ready !== undefined && status.replicas_desired !== undefined
                ? `${status.replicas_ready} / ${status.replicas_desired}`
                : '—'
            }
            tone={
              status.replicas_ready !== undefined && status.replicas_desired !== undefined
                ? replicasToneFor(status.replicas_ready, status.replicas_desired)
                : 'muted'
            }
          />
          <SignalPill
            icon={Rocket}
            label="Deploy status"
            value={status.deploy_status ?? '—'}
            tone={status.deploy_status ? deployToneFor(status.deploy_status) : 'muted'}
          />
          <SignalPill
            icon={TrendingUp}
            label="5xx — last 1h"
            value={
              status.traffic_5xx_pct_1h !== undefined
                ? `${status.traffic_5xx_pct_1h.toFixed(2)}%`
                : '—'
            }
            tone={
              status.traffic_5xx_pct_1h !== undefined
                ? trafficToneFor(status.traffic_5xx_pct_1h)
                : 'muted'
            }
          />
          <SignalPill
            icon={AlertTriangle}
            label="Active alerts"
            value={status.alerts_active !== undefined ? `${status.alerts_active}` : '—'}
            tone={
              status.alerts_active !== undefined ? alertsToneFor(status.alerts_active) : 'muted'
            }
          />
        </div>
      </Card.Body>
    </Card>
  );
}
