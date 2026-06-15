import { Label } from '@loopzero/ui';

/**
 * Plan caps for replicas — mirrors pkg/enforcement.PlanLimiter expectations
 * + matches the canonical "Vercel-style tier ceiling" pattern. Backend
 * remains authoritative (REPLICAS_EXCEED_PLAN at the API boundary); these
 * are UX-only ceilings to keep the slider from offering values the
 * server will reject.
 *
 * Source of truth: CLAUDE.md "Pricing" — Starter 2, Pro 5, Team 10,
 * Enterprise unlimited (we cap at 50 for the slider to stay usable).
 */
const REPLICAS_CAP_BY_PLAN: Record<string, number> = {
  starter: 2,
  pro: 5,
  team: 10,
  enterprise: 50,
};

const REPLICAS_MIN = 0;

export interface ScaleSliderProps {
  /** Current replicas (slider value). */
  value: number;
  /** Plan tier — drives the upper cap. Unknown tiers fall back to Starter. */
  planTier: string;
  /** Slider change callback. */
  onChange: (replicas: number) => void;
  /** Disable while a mutation is in flight. */
  disabled?: boolean;
}

/**
 * ScaleSlider — J-20 plan v1.1 T3.1.
 *
 * Native `<input type=range>` with plan-tier-bounded max. Shows current
 * value next to the label and the tier ceiling at the right edge so the
 * user understands the boundary they're operating against.
 *
 * Server stays authoritative (REPLICAS_EXCEED_PLAN at 422 if bypassed) —
 * this is UX-only. The slider also disables 0 when the tier rejects KEDA
 * scale-to-zero (handled by the parent page wiring + KEDAToggle gating).
 */
export function ScaleSlider({
  value,
  planTier,
  onChange,
  disabled = false,
}: ScaleSliderProps) {
  const max = REPLICAS_CAP_BY_PLAN[planTier.toLowerCase()] ?? REPLICAS_CAP_BY_PLAN.starter;
  const safeValue = Math.min(Math.max(value, REPLICAS_MIN), max);
  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between">
        <Label htmlFor="scale-slider-replicas">
          Replicas: <span className="font-mono font-semibold">{safeValue}</span>
        </Label>
        <span className="text-xs text-muted-foreground">
          {planTier} plan ceiling: {max}
        </span>
      </div>
      <input
        id="scale-slider-replicas"
        type="range"
        min={REPLICAS_MIN}
        max={max}
        step={1}
        value={safeValue}
        onChange={(e) => onChange(Number(e.target.value))}
        disabled={disabled}
        className="w-full"
        aria-valuemin={REPLICAS_MIN}
        aria-valuemax={max}
        aria-valuenow={safeValue}
      />
      <div className="flex justify-between text-xs text-muted-foreground">
        <span>{REPLICAS_MIN}</span>
        <span>{max}</span>
      </div>
    </div>
  );
}
