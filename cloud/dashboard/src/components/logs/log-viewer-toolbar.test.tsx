import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { LogViewerToolbar } from './log-viewer-toolbar';
import type { LogLine } from '@/lib/api/types';

const TS = '2026-06-12T20:00:00.000Z';
const sampleLines: LogLine[] = [{ ts: TS, level: 'info', msg: 'one' }];

const mkProps = (overrides: Partial<Parameters<typeof LogViewerToolbar>[0]> = {}) => ({
  lines: sampleLines,
  connected: true,
  error: false,
  paused: false,
  search: '',
  levelFilter: 'all' as const,
  onSearchChange: vi.fn(),
  onLevelFilterChange: vi.fn(),
  onTogglePause: vi.fn(),
  onClear: vi.fn(),
  ...overrides,
});

describe('LogViewerToolbar (J-19 iter 14 productive-idle — dedicated unit tests for extracted toolbar)', () => {
  beforeEach(() => {
    // ensure pointer polyfills active (used elsewhere) — toolbar uses simple buttons
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('test_log_viewer_toolbar_renders_search_input_with_aria_label', () => {
    render(<LogViewerToolbar {...mkProps()} />);
    const input = screen.getByLabelText(/Search log lines/i);
    expect(input).toBeInTheDocument();
    expect(input.getAttribute('type')).toBe('search');
  });

  it('test_log_viewer_toolbar_search_input_fires_onSearchChange', () => {
    const props = mkProps();
    render(<LogViewerToolbar {...props} />);
    const input = screen.getByLabelText(/Search log lines/i);
    fireEvent.change(input, { target: { value: 'error' } });
    expect(props.onSearchChange).toHaveBeenCalledWith('error');
  });

  it('test_log_viewer_toolbar_level_filter_buttons_render_4_options', () => {
    render(<LogViewerToolbar {...mkProps()} />);
    expect(screen.getByRole('button', { name: 'All' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Info' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Warn' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Error' })).toBeInTheDocument();
  });

  it('test_log_viewer_toolbar_level_filter_click_fires_callback', () => {
    const props = mkProps();
    render(<LogViewerToolbar {...props} />);
    fireEvent.click(screen.getByRole('button', { name: 'Error' }));
    expect(props.onLevelFilterChange).toHaveBeenCalledWith('error');
  });

  it('test_log_viewer_toolbar_connection_badge_shows_Live_when_connected', () => {
    render(<LogViewerToolbar {...mkProps({ connected: true, error: false })} />);
    expect(screen.getByText('Live')).toBeInTheDocument();
  });

  it('test_log_viewer_toolbar_connection_badge_shows_Disconnected_on_error', () => {
    render(<LogViewerToolbar {...mkProps({ connected: false, error: true })} />);
    expect(screen.getByText('Disconnected')).toBeInTheDocument();
  });

  it('test_log_viewer_toolbar_connection_badge_shows_Connecting_default', () => {
    render(<LogViewerToolbar {...mkProps({ connected: false, error: false })} />);
    expect(screen.getByText(/Connecting/)).toBeInTheDocument();
  });

  it('test_log_viewer_toolbar_pause_button_fires_onTogglePause', () => {
    const props = mkProps();
    render(<LogViewerToolbar {...props} />);
    fireEvent.click(screen.getByLabelText(/Pause log stream/i));
    expect(props.onTogglePause).toHaveBeenCalled();
  });

  it('test_log_viewer_toolbar_resume_button_when_paused', () => {
    const props = mkProps({ paused: true });
    render(<LogViewerToolbar {...props} />);
    fireEvent.click(screen.getByLabelText(/Resume log stream/i));
    expect(props.onTogglePause).toHaveBeenCalled();
  });

  it('test_log_viewer_toolbar_download_button_enabled_with_lines', () => {
    render(<LogViewerToolbar {...mkProps({ lines: sampleLines })} />);
    const dl = screen.getByLabelText(/Download log buffer/i);
    expect(dl).not.toBeDisabled();
  });

  it('test_log_viewer_toolbar_download_button_disabled_when_lines_empty', () => {
    render(<LogViewerToolbar {...mkProps({ lines: [] })} />);
    const dl = screen.getByLabelText(/Download log buffer/i);
    expect(dl).toBeDisabled();
  });

  it('test_log_viewer_toolbar_clear_button_fires_onClear', () => {
    const props = mkProps();
    render(<LogViewerToolbar {...props} />);
    fireEvent.click(screen.getByLabelText(/Clear log buffer/i));
    expect(props.onClear).toHaveBeenCalled();
  });
});
