import { Card, Tooltip } from '@loopzero/ui';
import { Link } from 'react-router-dom';
import { SIGNALS, isKnownRoute } from '@/lib/readiness/signals';
import { STATUS_VISUALS } from '@/lib/readiness/status-icons';
import type { ReadinessSignal } from '@/hooks/use-readiness-signals';

/**
 * J-21 Readiness Score plan v1.1 T2.2 — ReadinessChecklist.
 *
 * Renders 8 canonical signals as a vertical list with:
 *   - status icon + aria-label per ADR D3 (Pass / Warn / Fail / Unknown)
 *   - signal label + one-line description from `signals.ts`
 *   - "Fix it" link OR "Coming soon" tooltip (EC-7 — checks KNOWN_ROUTES
 *     allowlist; unknown route renders Tooltip instead of broken Link)
 *   - Unknown status rows do NOT render any actionable link (user can't
 *     fix what we can't measure)
 *
 * Order follows the SIGNALS array; consumers don't pass row props,
 * only the resolved status keyed by signal name + the projectName for
 * URL substitution.
 */
export interface ReadinessChecklistProps {
  signals: Record<string, ReadinessSignal>;
  projectName: string;
}

export function ReadinessChecklist({ signals, projectName }: ReadinessChecklistProps) {
  return (
    <Card>
      <Card.Body>
        <ul className="divide-y divide-border" aria-label="Readiness checklist">
          {SIGNALS.map((def) => {
            const sig = signals[def.name];
            const visual = STATUS_VISUALS[sig?.status ?? 'unknown'];
            const Icon = visual.icon;
            const isUnknown = sig?.status === 'unknown';
            const resolvedPath = def.fixPath.replace(':name', projectName);
            const known = isKnownRoute(def.fixPath);
            return (
              <li key={def.name} className="py-3 flex items-start gap-3">
                <Icon
                  className={`size-5 mt-0.5 shrink-0 ${visual.colorClass}`}
                  aria-label={visual.accessibleText}
                />
                <div className="flex-1 min-w-0">
                  <div className="flex items-baseline justify-between gap-3">
                    <p className="text-body font-medium text-foreground">{def.label}</p>
                    {!isUnknown && (known ? (
                      <Link
                        to={resolvedPath}
                        className="text-body-sm text-primary hover:underline shrink-0"
                      >
                        Fix it
                      </Link>
                    ) : (
                      <Tooltip label="Coming soon — this surface ships in a later release.">
                        <span className="text-body-sm text-muted-foreground cursor-help shrink-0">
                          Coming soon
                        </span>
                      </Tooltip>
                    ))}
                  </div>
                  <p className="text-body-sm text-muted-foreground">{def.description}</p>
                </div>
              </li>
            );
          })}
        </ul>
      </Card.Body>
    </Card>
  );
}
