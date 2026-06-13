import { Link } from 'react-router-dom';
import { AlertTriangle, CheckCircle2, CircleDashed, XCircle } from 'lucide-react';
import { Badge, Card } from '@loopzero/ui';

/**
 * ReadinessChecklist (Sprint 1 T1.2) — production-fitness signals visualization.
 *
 * Accepts ReadinessSignal[] and renders each as a row with status icon +
 * title + description + optional Fix CTA. Score badge counts ok signals.
 *
 * EC-16: description uses `truncate` + title attribute as tooltip fallback.
 * EC-15: Fix CTAs land on global pages; pre-filter por project é v2 scope
 * se sponsor solicitar.
 */

export type SignalStatus = 'ok' | 'warning' | 'fail' | 'unknown';

export interface ReadinessSignal {
  id: string;
  title: string;
  description: string;
  status: SignalStatus;
  /** Route to fix the issue (e.g., '/domains'). Lands on global page; user filters manually. */
  fixHref?: string;
  /** CTA label (e.g., "Add domain"). Required if fixHref set. */
  fixLabel?: string;
}

interface ReadinessChecklistProps {
  signals: ReadinessSignal[];
}

const STATUS_ICON = {
  ok: CheckCircle2,
  warning: AlertTriangle,
  fail: XCircle,
  unknown: CircleDashed,
} as const;

const STATUS_COLOR = {
  ok: 'text-success',
  warning: 'text-warning',
  fail: 'text-destructive',
  unknown: 'text-muted-foreground',
} as const;

const STATUS_BADGE_TONE = {
  ok: 'success',
  warning: 'warning',
  fail: 'destructive',
  unknown: 'default',
} as const;

export function ReadinessChecklist({ signals }: ReadinessChecklistProps) {
  const okCount = signals.filter((s) => s.status === 'ok').length;

  return (
    <Card>
      <Card.Header>
        <div className="flex items-center justify-between">
          <Card.Title>Production signals</Card.Title>
          <Badge variant={okCount === signals.length && signals.length > 0 ? 'success' : 'default'}>
            {okCount}/{signals.length} ready
          </Badge>
        </div>
      </Card.Header>
      <Card.Body className="p-0">
        <ul className="divide-y divide-border/40">
          {signals.map((signal) => {
            const Icon = STATUS_ICON[signal.status];
            const iconColor = STATUS_COLOR[signal.status];
            const tone = STATUS_BADGE_TONE[signal.status];
            return (
              <li key={signal.id} className="flex items-center gap-3 px-4 py-3">
                <Icon
                  className={`size-5 shrink-0 ${iconColor}`}
                  aria-label={signal.status}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-body-sm font-medium text-foreground">
                      {signal.title}
                    </span>
                    <Badge variant={tone} className="text-label uppercase">
                      {signal.status}
                    </Badge>
                  </div>
                  {/* EC-16: truncate + title attribute fallback for long descriptions */}
                  <p
                    className="text-label text-muted-foreground truncate"
                    title={signal.description}
                  >
                    {signal.description}
                  </p>
                </div>
                {signal.fixHref && signal.fixLabel && (
                  <Link
                    to={signal.fixHref}
                    className="text-label text-primary hover:underline shrink-0"
                  >
                    {signal.fixLabel}
                  </Link>
                )}
              </li>
            );
          })}
        </ul>
      </Card.Body>
    </Card>
  );
}
