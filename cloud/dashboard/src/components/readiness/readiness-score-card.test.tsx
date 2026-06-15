import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { ReadinessScoreCard } from './readiness-score-card';

beforeEach(() => {
  localStorage.clear();
});

describe('ReadinessScoreCard', () => {
  it('test_score_card_renders_value_and_threshold_color_classes', () => {
    const { rerender } = render(<ReadinessScoreCard score={95} projectName="p1" />);
    let bigNumber = screen.getByLabelText(/Readiness score 95/);
    expect(bigNumber.className).toMatch(/text-success/);
    expect(screen.getByText('Production ready')).toBeInTheDocument();

    rerender(<ReadinessScoreCard score={70} projectName="p1" />);
    bigNumber = screen.getByLabelText(/Readiness score 70/);
    expect(bigNumber.className).toMatch(/text-warning/);
    expect(screen.getByText('Needs attention')).toBeInTheDocument();

    rerender(<ReadinessScoreCard score={30} projectName="p1" />);
    bigNumber = screen.getByLabelText(/Readiness score 30/);
    expect(bigNumber.className).toMatch(/text-destructive/);
    expect(screen.getByText('Action required')).toBeInTheDocument();
  });

  it('test_score_card_renders_em_dash_when_loading (EC-6)', () => {
    render(<ReadinessScoreCard score={null} projectName="p1" />);
    expect(screen.getByLabelText('Score loading')).toHaveTextContent('—');
  });

  it('test_score_card_delta_uses_localStorage_prev_render (EC-11)', async () => {
    // First render — no prior snapshot — no delta visible.
    const { unmount } = render(<ReadinessScoreCard score={80} projectName="p1" />);
    expect(screen.queryByLabelText(/Delta vs previous render/)).toBeNull();
    unmount();

    // localStorage now has 80; next render with 95 → +15.
    render(<ReadinessScoreCard score={95} projectName="p1" />);
    const delta = await screen.findByLabelText(/Delta vs previous render/);
    expect(delta.textContent).toMatch(/\+15/);
    expect(delta.className).toMatch(/text-success/);
  });

  it('test_score_card_delta_negative_renders_em_dash_minus', async () => {
    render(<ReadinessScoreCard score={90} projectName="p2" />);
    // Re-render with lower score.
    render(<ReadinessScoreCard score={70} projectName="p2" />);
    const delta = await screen.findByLabelText(/Delta vs previous render/);
    expect(delta.textContent).toMatch(/–20|−20|-20/); // accept en-dash or hyphen
    expect(delta.className).toMatch(/text-destructive/);
  });
});
