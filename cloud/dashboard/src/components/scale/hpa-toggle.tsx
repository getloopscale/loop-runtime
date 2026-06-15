import { Input, Label } from '@loopzero/ui';

/**
 * HPA target bounds — mirrors pkg/enforcement.HPACPUTargetMin/Max +
 * HPAMemoryTargetMin/Max. UI mirrors the server bounds so the user
 * gets inline feedback BEFORE submit; the API handler is the security
 * boundary (EC-4 absorption — client-side validation is UX, not the
 * authoritative gate).
 */
export const HPA_CPU_MIN = 10;
export const HPA_CPU_MAX = 95;
export const HPA_MEMORY_MIN = 10;
export const HPA_MEMORY_MAX = 90;

export type HPAMode = 'manual' | 'auto';

export interface HPATargets {
  cpu: number;
  memory: number;
}

export interface HPAToggleProps {
  /** Current mode — Manual = fixed replicas; Auto = HPA targets. */
  mode: HPAMode;
  /** HPA target values (cpu + memory percentages). */
  targets: HPATargets;
  /** Mode-switch callback. */
  onModeChange: (mode: HPAMode) => void;
  /** Target-edit callback (per-field). */
  onTargetsChange: (targets: HPATargets) => void;
  /** Disable controls while a mutation is in flight. */
  disabled?: boolean;
}

/**
 * HPAToggle — J-20 plan v1.1 T3.2.
 *
 * Radio Manual/Auto + when Auto expands CPU + memory percentage inputs.
 * Mutually exclusive with replicas slider per ADR D2 — the parent page
 * orchestrates the swap; this component only emits intent.
 *
 * Client-side bound enforcement (UX only — server enforces at handler):
 *   - cpu_target  ∈ [10, 95]   (HPA_CPU_MIN..MAX)
 *   - memory_target ∈ [10, 90] (HPA_MEMORY_MIN..MAX)
 *
 * Inline validation message appears below the offending field so the
 * user understands the rejection without round-tripping to the server.
 */
export function HPAToggle({
  mode,
  targets,
  onModeChange,
  onTargetsChange,
  disabled = false,
}: HPAToggleProps) {
  const cpuOutOfBounds = targets.cpu < HPA_CPU_MIN || targets.cpu > HPA_CPU_MAX;
  const memoryOutOfBounds =
    targets.memory < HPA_MEMORY_MIN || targets.memory > HPA_MEMORY_MAX;
  return (
    <div className="space-y-3">
      <fieldset className="space-y-2" disabled={disabled}>
        <legend className="text-sm font-medium">Scaling mode</legend>
        <div className="flex gap-4">
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              name="hpa-mode"
              value="manual"
              checked={mode === 'manual'}
              onChange={() => onModeChange('manual')}
              disabled={disabled}
            />
            <span className="text-sm">Manual — fixed replicas</span>
          </label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              name="hpa-mode"
              value="auto"
              checked={mode === 'auto'}
              onChange={() => onModeChange('auto')}
              disabled={disabled}
            />
            <span className="text-sm">Auto — HPA scales by CPU/memory</span>
          </label>
        </div>
      </fieldset>

      {mode === 'auto' && (
        <div className="space-y-3 pl-6 border-l-2 border-muted">
          <div className="space-y-1">
            <Label htmlFor="hpa-cpu-target">
              CPU target ({HPA_CPU_MIN}–{HPA_CPU_MAX}%)
            </Label>
            <Input
              id="hpa-cpu-target"
              type="number"
              min={HPA_CPU_MIN}
              max={HPA_CPU_MAX}
              value={targets.cpu}
              onChange={(e) =>
                onTargetsChange({ ...targets, cpu: Number(e.target.value) })
              }
              disabled={disabled}
              aria-invalid={cpuOutOfBounds}
            />
            {cpuOutOfBounds && (
              <p className="text-xs text-destructive">
                CPU target must be between {HPA_CPU_MIN}% and {HPA_CPU_MAX}%.
              </p>
            )}
          </div>
          <div className="space-y-1">
            <Label htmlFor="hpa-memory-target">
              Memory target ({HPA_MEMORY_MIN}–{HPA_MEMORY_MAX}%)
            </Label>
            <Input
              id="hpa-memory-target"
              type="number"
              min={HPA_MEMORY_MIN}
              max={HPA_MEMORY_MAX}
              value={targets.memory}
              onChange={(e) =>
                onTargetsChange({ ...targets, memory: Number(e.target.value) })
              }
              disabled={disabled}
              aria-invalid={memoryOutOfBounds}
            />
            {memoryOutOfBounds && (
              <p className="text-xs text-destructive">
                Memory target must be between {HPA_MEMORY_MIN}% and {HPA_MEMORY_MAX}%.
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
