import { ExternalLink } from 'lucide-react';
import { Button, Sheet } from '@loopzero/ui';

import { useMetricsRange } from '@/hooks/use-metrics-range';
import { buildGrafanaDeepLink } from '@/lib/grafana-deep-link';
import type { SignalName } from '@/hooks/use-status-stream';

/**
 * SignalDetailSheet — Plan J-18 T3.3.
 *
 * Right-side drill-down sheet rendered when a user clicks a SignalPill in
 * SignalGrid. Shows:
 *   1. Signal label + range selector (current: 7d only; future: 24h/7d/30d toggle)
 *   2. 7d data table OR honest "Coletando dados..." when EC-8 collecting state
 *   3. "Open in Grafana" deep-link (T2.3 buildGrafanaDeepLink — disabled
 *      when grafanaBaseURL not configured per EC-13)
 *
 * Chart rendering deferred to T3.3.b follow-up (Recharts integration);
 * scaffold renders 7d data points as a simple table so dashboard ships
 * functional even without chart lib added. Per ADR D3 honest collecting
 * state — UI explicit when data not yet available.
 *
 * EC-7 absorption: when route changes mid-sheet-open, parent (T4.2) closes
 * sheet via useEffect cleanup — this component is dumb display.
 */
export interface SignalDetailSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  signal: SignalName | null;
  projectName: string;
  env: string;
  /** Optional Grafana base URL from statusResponse.metrics_range_url. */
  grafanaBaseURL?: string | null;
}

const SIGNAL_LABELS: Record<SignalName, string> = {
  status: 'Status',
  replicas: 'Replicas',
  uptime_24h: 'Uptime 24h',
  p95_latency: 'p95 Latency',
  error_rate: 'Error Rate',
  rps: 'RPS',
  alerts_active: 'Alerts Active',
  last_deploy_time: 'Last Deploy',
};

export function SignalDetailSheet({
  open,
  onOpenChange,
  signal,
  projectName,
  env,
  grafanaBaseURL,
}: SignalDetailSheetProps) {
  const { data, status, collectingHint } = useMetricsRange(projectName, env, {
    signal: signal ?? '',
    range: '7d',
  });

  const grafanaURL = buildGrafanaDeepLink({
    grafanaBaseURL,
    projectName,
    env,
    signal: signal ?? undefined,
  });

  const label = signal ? SIGNAL_LABELS[signal] : 'Signal';

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <Sheet.Content side="right" className="w-full sm:max-w-md">
        <Sheet.Header className="border-b border-border px-6 py-5">
          <Sheet.Title>{label}</Sheet.Title>
          <Sheet.Description>
            Last 7 days · {projectName} · {env}
          </Sheet.Description>
        </Sheet.Header>

        <Sheet.Body className="px-6 py-5">
          {status === 'collecting' && (
            <div
              role="status"
              aria-live="polite"
              className="rounded-md border border-border bg-muted/30 p-4 text-body-sm text-muted-foreground"
            >
              {collectingHint ?? 'Coletando dados...'}
            </div>
          )}

          {status === 'prometheus_unreachable' && (
            <div
              role="status"
              className="rounded-md border border-destructive/40 bg-destructive/10 p-4 text-body-sm text-destructive"
            >
              Metrics service unreachable. Try again in a moment.
            </div>
          )}

          {status === 'ok' && data.length > 0 && (
            <div className="overflow-auto">
              <p className="mb-2 text-label uppercase text-muted-foreground">
                {data.length} data points
              </p>
              <table
                role="table"
                aria-label={`${label} datapoints over 7 days`}
                className="w-full text-body-sm font-mono tabular-nums"
              >
                <thead>
                  <tr className="border-b border-border text-left text-muted-foreground">
                    <th className="py-2 pr-4">Timestamp</th>
                    <th className="py-2 text-right">Value</th>
                  </tr>
                </thead>
                <tbody>
                  {data.slice(-20).map((p) => (
                    <tr key={`${p.t}-${p.v}`} className="border-b border-border/40">
                      <td className="py-2 pr-4">{new Date(p.t * 1000).toLocaleString()}</td>
                      <td className="py-2 text-right">{p.v}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {data.length > 20 && (
                <p className="mt-2 text-label text-muted-foreground">
                  Showing last 20 of {data.length} datapoints. Chart view coming soon.
                </p>
              )}
            </div>
          )}
        </Sheet.Body>

        <Sheet.Footer className="border-t border-border px-6 py-4">
          {grafanaURL ? (
            <Button
              type="button"
              variant="tertiary"
              size="sm"
              onClick={() => window.open(grafanaURL, '_blank', 'noopener,noreferrer')}
            >
              <ExternalLink aria-hidden="true" className="size-4" />
              <span>Open in Grafana</span>
            </Button>
          ) : (
            <Button
              type="button"
              variant="tertiary"
              size="sm"
              disabled
              aria-label="Grafana deep-link not configured"
            >
              <ExternalLink aria-hidden="true" className="size-4" />
              <span>Open in Grafana</span>
            </Button>
          )}
        </Sheet.Footer>
      </Sheet.Content>
    </Sheet>
  );
}
