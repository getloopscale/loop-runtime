import { describe, it, expect, vi } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { Plus } from 'lucide-react';
import { render, screen } from '@/test/render';
import { ActionBar } from './action-bar';

/**
 * T-B.3 ActionBar tests (4 planned in plan v1.2 TDD section).
 */
describe('ActionBar', () => {
  it('test_action_bar_renders_search_input_with_placeholder', () => {
    render(<ActionBar searchPlaceholder="Search projects..." />);
    expect(screen.getByPlaceholderText('Search projects...')).toBeInTheDocument();
  });

  it('test_action_bar_fires_onSearchChange_on_typing', async () => {
    const onSearchChange = vi.fn();
    const user = userEvent.setup();
    render(
      <ActionBar
        searchPlaceholder="Search..."
        searchValue=""
        onSearchChange={onSearchChange}
      />,
    );
    const input = screen.getByPlaceholderText('Search...');
    await user.type(input, 'h');
    expect(onSearchChange).toHaveBeenCalledWith('h');
  });

  it('test_action_bar_fires_onFilterClick_when_clicked', async () => {
    const onFilterClick = vi.fn();
    const user = userEvent.setup();
    render(<ActionBar onFilterClick={onFilterClick} />);
    const filterButton = screen.getByLabelText('Open filters');
    await user.click(filterButton);
    expect(onFilterClick).toHaveBeenCalledTimes(1);
  });

  it('test_action_bar_primary_action_renders_with_icon_and_fires_onClick', async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();
    render(
      <ActionBar
        primaryAction={{ label: 'Add New', icon: Plus, onClick }}
      />,
    );
    const button = screen.getByRole('button', { name: /add new/i });
    await user.click(button);
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
