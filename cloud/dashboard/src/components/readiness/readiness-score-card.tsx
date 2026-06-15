import { useEffect, useState } from 'react';
import { Card } from '@loopzero/ui';

/**
 * J-21 Readiness Score plan v1.1 T2.1 — ReadinessScoreCard.
 *
 * KPI big-number 0-100 + delta vs the user's PREVIOUS render persisted in
 * localStorage (EC-11 absorption — server-side snapshot deferred to v2 per
 * ADR D1).
 *
 * Color classes mirror ADR D3 status mapping: ≥90 success / 50-89 warning /
 * <50 destructive. The threshold is announced via aria-label so screen
 * readers don't rely on color (WCAG 1.4.1).
 *
 * When `score` is `null` (any composing hook still loading per EC-6),
 * renders a skeleton with the canonical em-dash placeholder.
 */
export interface ReadinessScoreCardProps {
  /** Canonical score 0-100 from useReadinessSignals; null while loading. */
  score: number | null;
  /** Project name — used as localStorage key for delta-vs-prev computation. */
  projectName: string;
}

const STORAGE_PREFIX = 'loopzero:readiness-score-prev:';

function thresholdClass(score: number): string {
  if (score >= 90) return 'text-success';
  if (score >= 50) return 'text-warning';
  return 'text-destructive';
}

function thresholdLabel(score: number): string {
  if (score >= 90) return 'Production ready';
  if (score >= 50) return 'Needs attention';
  return 'Action required';
}

export function ReadinessScoreCard({
  score,
  projectName,
}: ReadinessScoreCardProps) {
  const [previousScore, setPreviousScore] = useState<number | null>(null);

  useEffect(() => {
    if (score === null) return;
    const key = `${STORAGE_PREFIX}${projectName}`;
    const prevRaw = localStorage.getItem(key);
    const prev = prevRaw === null ? null : Number(prevRaw);
    if (prev !== null && !Number.isNaN(prev)) {
      setPreviousScore(prev);
    }
    // Update stored prev after computing this render's delta — next render
    // will see this score as its "previous".
    localStorage.setItem(key, String(score));
  }, [score, projectName]);

  if (score === null) {
    return (
      <Card>
        <Card.Body>
          <div className="space-y-2">
            <p className="text-label text-muted-foreground uppercase tracking-wide">
              Readiness score
            </p>
            <p className="text-display font-bold text-muted-foreground" aria-label="Score loading">
              —
            </p>
          </div>
        </Card.Body>
      </Card>
    );
  }

  const delta = previousScore !== null ? score - previousScore : null;
  const deltaSign = delta === null ? '' : delta > 0 ? '+' : delta < 0 ? '–' : '';
  const deltaClass =
    delta === null
      ? ''
      : delta > 0
        ? 'text-success'
        : delta < 0
          ? 'text-destructive'
          : 'text-muted-foreground';

  const ariaLabel = `Readiness score ${score} out of 100. ${thresholdLabel(score)}.`;

  return (
    <Card>
      <Card.Body>
        <div className="space-y-2">
          <p className="text-label text-muted-foreground uppercase tracking-wide">
            Readiness score
          </p>
          <div className="flex items-baseline gap-3">
            <p
              className={`text-display font-bold tabular-nums ${thresholdClass(score)}`}
              aria-label={ariaLabel}
            >
              {score}
            </p>
            <span className="text-body-sm text-muted-foreground">/ 100</span>
            {delta !== null && Math.abs(delta) > 0 && (
              <span
                className={`text-body-sm font-mono ${deltaClass}`}
                aria-label={`Delta vs previous render: ${deltaSign}${Math.abs(delta)}`}
              >
                {deltaSign}
                {Math.abs(delta)}
              </span>
            )}
          </div>
          <p className="text-body-sm text-muted-foreground">{thresholdLabel(score)}</p>
        </div>
      </Card.Body>
    </Card>
  );
}
