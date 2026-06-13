import { Select } from '@loopzero/ui';

/**
 * TimeRangePicker — J-19 T1.1 (R1 blueprint MUST).
 *
 * Six-preset dropdown that maps to LoopZero's Beta Logs Gate Criterion 7
 * `?since=` query param. Preset values are the canonical strings the backend
 * accepts (`5m`, `15m`, `1h`, `6h`, `24h`, `7d`), so NO mapping layer is
 * needed between this component and the SSE URL.
 *
 * Loki retention is 168h (7d) per `.claude/knowledge-base/concepts/observability/loki-retention-and-since-flag.md`;
 * `7d` is therefore the maximum window the picker offers. Custom date-range
 * picker is OUT OF SCOPE for v0.1 per ADR D3 (KISS).
 *
 * Per `.claude/rules/cli-output.md § Rule 3`, this UX uses product language
 * ("Last X hours"); internal terms (Loki, since) stay backend-only.
 */
export const TIME_RANGE_PRESETS = [
  { value: '5m', label: 'Last 5 min' },
  { value: '15m', label: 'Last 15 min' },
  { value: '1h', label: 'Last 1 hour' },
  { value: '6h', label: 'Last 6 hours' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
] as const;

export interface TimeRangePickerProps {
  value: string;
  onChange: (next: string) => void;
}

export function TimeRangePicker({ value, onChange }: TimeRangePickerProps) {
  return (
    <Select value={value} onValueChange={onChange}>
      <Select.Trigger aria-label="Time range" className="w-44">
        <Select.Value placeholder="Time range" />
      </Select.Trigger>
      <Select.Content>
        {TIME_RANGE_PRESETS.map((p) => (
          <Select.Item key={p.value} value={p.value}>
            {p.label}
          </Select.Item>
        ))}
      </Select.Content>
    </Select>
  );
}
