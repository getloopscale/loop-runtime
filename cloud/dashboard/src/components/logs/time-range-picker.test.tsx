import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { TimeRangePicker } from './time-range-picker';

describe('TimeRangePicker (J-19 T1.1 — R1 blueprint MUST)', () => {
  it('test_time_range_picker_renders_six_preset_options', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TimeRangePicker value="1h" onChange={onChange} />);
    const trigger = screen.getByRole('combobox');
    await user.click(trigger);
    const items = await screen.findAllByRole('option');
    expect(items).toHaveLength(6);
  });

  it('test_time_range_picker_fires_onchange_with_preset_value', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TimeRangePicker value="1h" onChange={onChange} />);
    const trigger = screen.getByRole('combobox');
    await user.click(trigger);
    const item24h = await screen.findByRole('option', { name: /24 hours/i });
    await user.click(item24h);
    expect(onChange).toHaveBeenCalledWith('24h');
  });
});
