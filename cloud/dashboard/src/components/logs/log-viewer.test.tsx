import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { LogViewer } from './log-viewer';
import type { LogLine } from '@/lib/api/types';

const TS = '2026-06-12T20:00:00.000Z';

describe('LogViewer (J-19 T1.2 — R2 ANSI render integration)', () => {
  it('test_log_viewer_renders_ansi_spans_in_lines', () => {
    const lines: LogLine[] = [
      { ts: TS, level: 'info', msg: '\x1b[31mERROR\x1b[0m: connection refused' },
    ];
    render(
      <LogViewer
        lines={lines}
        connected={true}
        error={false}
        paused={false}
        onTogglePause={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    // ANSI sequence converted to styled span via parseAnsi + dangerouslySetInnerHTML
    const styledSpan = document.querySelector('span[style*="color"]');
    expect(styledSpan).not.toBeNull();
    expect(styledSpan?.textContent).toBe('ERROR');
    // Surrounding text remains intact
    expect(screen.getByText(/connection refused/)).toBeInTheDocument();
  });

  it('test_log_viewer_plain_msg_renders_unchanged_no_extra_span', () => {
    const lines: LogLine[] = [
      { ts: TS, level: 'info', msg: 'plain message no ANSI' },
    ];
    render(
      <LogViewer
        lines={lines}
        connected={true}
        error={false}
        paused={false}
        onTogglePause={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.getByText(/plain message no ANSI/)).toBeInTheDocument();
    // Fast path: no extra span injected
    expect(document.querySelector('span[style*="color"]')).toBeNull();
  });

  it('test_log_viewer_renders_dropped_indicator_when_count_gt_zero', () => {
    render(
      <LogViewer
        lines={[]}
        connected={true}
        error={false}
        paused={false}
        droppedCount={42}
        onTogglePause={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.getByText(/42 older lines dropped/)).toBeInTheDocument();
  });

  it('test_log_viewer_renders_dropped_indicator_singular_for_one', () => {
    render(
      <LogViewer
        lines={[]}
        connected={true}
        error={false}
        paused={false}
        droppedCount={1}
        onTogglePause={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.getByText('1 older line dropped from buffer')).toBeInTheDocument();
  });

  it('test_log_viewer_hides_dropped_indicator_when_count_zero', () => {
    render(
      <LogViewer
        lines={[]}
        connected={true}
        error={false}
        paused={false}
        droppedCount={0}
        onTogglePause={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.queryByText(/older line.*dropped/)).toBeNull();
  });

  it('test_log_viewer_hides_dropped_indicator_when_prop_absent_backward_compat', () => {
    // EC-6 — existing build-logs.tsx mounts without droppedCount; indicator must stay hidden
    render(
      <LogViewer
        lines={[]}
        connected={true}
        error={false}
        paused={false}
        onTogglePause={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.queryByText(/older line.*dropped/)).toBeNull();
  });
});
