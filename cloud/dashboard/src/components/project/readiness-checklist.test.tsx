import { describe, it, expect } from 'vitest';
import { render, screen } from '@/test/render';
import {
  ReadinessChecklist,
  type ReadinessSignal,
} from './readiness-checklist';

const SIGNALS: ReadinessSignal[] = [
  { id: 'a', title: 'Signal A', description: 'Desc A', status: 'ok' },
  { id: 'b', title: 'Signal B', description: 'Desc B', status: 'warning', fixHref: '/domains', fixLabel: 'Add domain' },
  { id: 'c', title: 'Signal C', description: 'Desc C', status: 'fail' },
  { id: 'd', title: 'Signal D', description: 'Desc D', status: 'unknown' },
];

describe('ReadinessChecklist (Sprint 1 T1.2)', () => {
  it('test_renders_signal_titles_and_descriptions', () => {
    render(<ReadinessChecklist signals={SIGNALS} />);
    expect(screen.getByText('Signal A')).toBeInTheDocument();
    expect(screen.getByText('Desc A')).toBeInTheDocument();
    expect(screen.getByText('Signal D')).toBeInTheDocument();
  });

  it('test_renders_status_icons_per_status', () => {
    const { container } = render(<ReadinessChecklist signals={SIGNALS} />);
    // 4 status variants — assert distinct aria-label per variant
    expect(container.querySelector('[aria-label="ok"]')).toBeTruthy();
    expect(container.querySelector('[aria-label="warning"]')).toBeTruthy();
    expect(container.querySelector('[aria-label="fail"]')).toBeTruthy();
    expect(container.querySelector('[aria-label="unknown"]')).toBeTruthy();
  });

  it('test_renders_fix_cta_only_when_fixHref_set', () => {
    render(<ReadinessChecklist signals={SIGNALS} />);
    // Signal B has fixHref + fixLabel — should render link
    expect(screen.getByRole('link', { name: 'Add domain' })).toBeInTheDocument();
    // Signal A/C/D lack fixHref — only Signal B link exists
    expect(screen.getAllByRole('link')).toHaveLength(1);
  });

  it('test_renders_score_badge_with_ok_count', () => {
    render(<ReadinessChecklist signals={SIGNALS} />);
    expect(screen.getByText('1/4 ready')).toBeInTheDocument();
  });

  it('test_renders_zero_score_when_signals_empty', () => {
    render(<ReadinessChecklist signals={[]} />);
    expect(screen.getByText('0/0 ready')).toBeInTheDocument();
  });
});
