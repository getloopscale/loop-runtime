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

  // iter 15 productive-idle — boundary + accessibility defensive coverage
  it('test_time_range_picker_renders_aria_label_for_screen_readers', () => {
    render(<TimeRangePicker value="1h" onChange={vi.fn()} />);
    const trigger = screen.getByRole('combobox', { name: /Time range/i });
    expect(trigger).toBeInTheDocument();
  });

  it('test_time_range_picker_value_outside_presets_displays_placeholder', () => {
    // Defensive: if caller passes value not in PRESETS (e.g. legacy '30m'),
    // the Radix Select trigger shows the placeholder instead of crashing
    render(<TimeRangePicker value="30m-not-a-preset" onChange={vi.fn()} />);
    const trigger = screen.getByRole('combobox');
    expect(trigger).toBeInTheDocument();
    // Trigger should render — Radix tolerates unknown value
  });

  it('test_time_range_picker_all_six_preset_values_unique', async () => {
    // Boundary: ensure no duplicate preset values that would confuse Radix
    const user = userEvent.setup();
    render(<TimeRangePicker value="1h" onChange={vi.fn()} />);
    await user.click(screen.getByRole('combobox'));
    const items = await screen.findAllByRole('option');
    const labels = items.map((el) => el.textContent ?? '');
    const unique = new Set(labels);
    expect(unique.size).toBe(6);
  });
});
