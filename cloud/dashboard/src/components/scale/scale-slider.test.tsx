import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ScaleSlider } from './scale-slider';

describe('ScaleSlider', () => {
  it('test_caps_max_at_tier_ceiling_starter', () => {
    const onChange = vi.fn();
    render(<ScaleSlider value={1} planTier="starter" onChange={onChange} />);
    const slider = screen.getByLabelText(/Replicas/i) as HTMLInputElement;
    expect(slider.max).toBe('2');
  });

  it('test_caps_max_at_tier_ceiling_pro', () => {
    const onChange = vi.fn();
    render(<ScaleSlider value={3} planTier="pro" onChange={onChange} />);
    const slider = screen.getByLabelText(/Replicas/i) as HTMLInputElement;
    expect(slider.max).toBe('5');
  });

  it('test_falls_back_to_starter_for_unknown_tier', () => {
    const onChange = vi.fn();
    render(<ScaleSlider value={1} planTier="platinum-vip" onChange={onChange} />);
    const slider = screen.getByLabelText(/Replicas/i) as HTMLInputElement;
    expect(slider.max).toBe('2'); // starter fallback (security: fail-closed)
  });

  it('test_emits_onChange_with_numeric_value', () => {
    const onChange = vi.fn();
    render(<ScaleSlider value={1} planTier="pro" onChange={onChange} />);
    const slider = screen.getByLabelText(/Replicas/i);
    fireEvent.change(slider, { target: { value: '4' } });
    expect(onChange).toHaveBeenCalledWith(4);
  });
});
