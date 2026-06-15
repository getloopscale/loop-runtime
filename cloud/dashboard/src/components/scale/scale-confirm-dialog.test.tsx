import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ScaleConfirmDialog } from './scale-confirm-dialog';

describe('ScaleConfirmDialog', () => {
  it('test_renders_impact_paragraph_with_replicas_delta', () => {
    render(
      <ScaleConfirmDialog
        open={true}
        onOpenChange={vi.fn()}
        projectName="my-api"
        environment="production"
        currentReplicas={2}
        proposedReplicas={5}
        estimatedMonthlyUSD={182.5}
        deltaUSD={109.5}
        onConfirm={vi.fn()}
      />,
    );
    expect(
      screen.getByText(/Apply scale to/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/2 → 5 replicas/i)).toBeInTheDocument();
    expect(screen.getByText(/\$182.50\/mo/)).toBeInTheDocument();
    // delta paragraph
    expect(screen.getByText(/\+\$109.50\/mo vs current/)).toBeInTheDocument();
  });

  it('test_shows_decrease_word_when_scaling_down', () => {
    render(
      <ScaleConfirmDialog
        open={true}
        onOpenChange={vi.fn()}
        projectName="my-api"
        environment="production"
        currentReplicas={5}
        proposedReplicas={2}
        estimatedMonthlyUSD={73}
        deltaUSD={-109.5}
        onConfirm={vi.fn()}
      />,
    );
    expect(screen.getByText(/decrease/i)).toBeInTheDocument();
    expect(screen.getByText(/–\$109.50\/mo vs current/)).toBeInTheDocument();
  });

  it('test_renders_with_canonical_type_to_confirm_phrase', () => {
    // The ConfirmDialog upstream primitive surfaces the phrase via an
    // input prompt. We assert the phrase string appears in the rendered
    // dialog (`scale <projectname>` per EC-8 canonical convention).
    render(
      <ScaleConfirmDialog
        open={true}
        onOpenChange={vi.fn()}
        projectName="checkout-api"
        environment="production"
        currentReplicas={3}
        proposedReplicas={3}
        estimatedMonthlyUSD={109.5}
        deltaUSD={0}
        onConfirm={vi.fn()}
      />,
    );
    // The phrase appears in the dialog (rendered by ConfirmDialog upstream
    // in multiple slots: title trigger + input placeholder + confirm label).
    // Use getAllByText since the substring intentionally surfaces in ≥1
    // slot — assert non-empty.
    const matches = screen.getAllByText((_, node) => {
      const text = node?.textContent ?? '';
      return text.includes('scale checkout-api');
    });
    expect(matches.length).toBeGreaterThan(0);
  });

  it('test_closed_dialog_does_not_render', () => {
    const { container } = render(
      <ScaleConfirmDialog
        open={false}
        onOpenChange={vi.fn()}
        projectName="my-api"
        environment="production"
        currentReplicas={2}
        proposedReplicas={2}
        estimatedMonthlyUSD={73}
        deltaUSD={0}
        onConfirm={vi.fn()}
      />,
    );
    // ConfirmDialog renders nothing when open=false (Radix Portal pattern).
    expect(container.querySelector('[role="dialog"]')).toBeNull();
  });
});
