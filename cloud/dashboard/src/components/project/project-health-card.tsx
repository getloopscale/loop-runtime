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
            value={`${status.replicas_ready} / ${status.replicas_desired}`}
            tone={replicasToneFor(status.replicas_ready, status.replicas_desired)}
          />
          <SignalPill
            icon={Rocket}
            label="Deploy status"
            value={status.deploy_status}
            tone={deployToneFor(status.deploy_status)}
          />
          <SignalPill
            icon={TrendingUp}
            label="5xx — last 1h"
            value={`${status.traffic_5xx_pct_1h.toFixed(2)}%`}
            tone={trafficToneFor(status.traffic_5xx_pct_1h)}
          />
          <SignalPill
            icon={AlertTriangle}
            label="Active alerts"
            value={`${status.alerts_active}`}
            tone={alertsToneFor(status.alerts_active)}
          />
        </div>
      </Card.Body>
    </Card>
  );
}
