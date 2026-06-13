/**
 * DroppedLinesIndicator — J-19 T1.3 (R3 blueprint MUST), extracted from
 * LogViewer at T2.x batch to keep log-viewer.tsx under 250 LOC budget
 * (Global DoD + EC-9 halt-loop checkpoint).
 *
 * Surfaces an explicit indicator when the useLogStream FIFO buffer
 * silently dropped older lines (cap 5000). Closes EC-8 silent-drop gap.
 */
interface DroppedLinesIndicatorProps {
  count: number;
}

export function DroppedLinesIndicator({ count }: DroppedLinesIndicatorProps) {
  if (count <= 0) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      className="border-b border-warning/20 bg-warning/10 px-4 py-1 text-label text-warning"
    >
      {count === 1
        ? '1 older line dropped from buffer'
        : `${count} older lines dropped from buffer`}
    </div>
  );
}
