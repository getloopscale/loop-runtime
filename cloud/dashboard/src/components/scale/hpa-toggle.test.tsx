import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { HPAToggle } from './hpa-toggle';

const baseTargets = { cpu: 80, memory: 80 };

describe('HPAToggle', () => {
  it('test_manual_mode_hides_targets', () => {
    render(
      <HPAToggle
        mode="manual"
        targets={baseTargets}
        onModeChange={vi.fn()}
        onTargetsChange={vi.fn()}
      />,
    );
    expect(screen.queryByLabelText(/CPU target/i)).toBeNull();
  });

  it('test_auto_mode_shows_cpu_and_memory_inputs', () => {
    render(
      <HPAToggle
        mode="auto"
        targets={baseTargets}
        onModeChange={vi.fn()}
        onTargetsChange={vi.fn()}
      />,
    );
    expect(screen.getByLabelText(/CPU target/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Memory target/i)).toBeInTheDocument();
  });

  it('test_inline_validation_for_cpu_below_min', () => {
    render(
      <HPAToggle
        mode="auto"
        targets={{ cpu: 1, memory: 80 }}
        onModeChange={vi.fn()}
        onTargetsChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/CPU target must be between 10% and 95%/i)).toBeInTheDocument();
  });

  it('test_inline_validation_for_memory_above_max', () => {
    render(
      <HPAToggle
        mode="auto"
        targets={{ cpu: 80, memory: 99 }}
        onModeChange={vi.fn()}
        onTargetsChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/Memory target must be between 10% and 90%/i)).toBeInTheDocument();
  });

  it('test_emits_mode_change_when_switching_to_auto', () => {
    const onModeChange = vi.fn();
    render(
      <HPAToggle
        mode="manual"
        targets={baseTargets}
        onModeChange={onModeChange}
        onTargetsChange={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByLabelText(/Auto — HPA scales/i));
    expect(onModeChange).toHaveBeenCalledWith('auto');
  });
});
