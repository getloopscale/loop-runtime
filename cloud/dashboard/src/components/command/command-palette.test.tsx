import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@/test/render';
import { userEvent } from '@testing-library/user-event';
import { CommandPalette, type CommandItem } from './command-palette';

const COMMANDS: CommandItem[] = [
  { id: 'nav-projects', category: 'navigate', label: 'Projects', perform: vi.fn() },
  { id: 'nav-team', category: 'navigate', label: 'Team', perform: vi.fn() },
  { id: 'action-new-project', category: 'action', label: 'Create project', perform: vi.fn() },
];

describe('CommandPalette (Sprint 2 T2.1)', () => {
  it('test_renders_input_with_autofocus_when_open', () => {
    render(
      <CommandPalette open={true} onOpenChange={() => {}} commands={COMMANDS} />,
    );
    const input = screen.getByPlaceholderText(/type a command/i);
    expect(input).toBeInTheDocument();
  });

  it('test_renders_commands_grouped_by_category', () => {
    render(
      <CommandPalette open={true} onOpenChange={() => {}} commands={COMMANDS} />,
    );
    expect(screen.getByText('Projects')).toBeInTheDocument();
    expect(screen.getByText('Team')).toBeInTheDocument();
    expect(screen.getByText('Create project')).toBeInTheDocument();
  });

  it('test_does_not_render_when_closed', () => {
    render(
      <CommandPalette open={false} onOpenChange={() => {}} commands={COMMANDS} />,
    );
    expect(screen.queryByPlaceholderText(/type a command/i)).not.toBeInTheDocument();
  });

  it('test_invokes_perform_when_command_selected', async () => {
    const onOpenChange = vi.fn();
    const performSpy = vi.fn();
    const cmds: CommandItem[] = [
      { id: 'x', category: 'action', label: 'Do thing', perform: performSpy },
    ];
    render(
      <CommandPalette open={true} onOpenChange={onOpenChange} commands={cmds} />,
    );
    await userEvent.click(screen.getByText('Do thing'));
    expect(performSpy).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  // EC-11: perform throws → palette doesn't crash
  it('test_palette_handles_perform_throw', async () => {
    const onOpenChange = vi.fn();
    const throwingPerform = vi.fn(() => {
      throw new Error('boom');
    });
    const cmds: CommandItem[] = [
      { id: 'crash', category: 'action', label: 'Crash me', perform: throwingPerform },
    ];
    const originalError = console.error;
    console.error = vi.fn();
    try {
      render(
        <CommandPalette open={true} onOpenChange={onOpenChange} commands={cmds} />,
      );
      // Click should not throw; palette catches + closes
      await expect(userEvent.click(screen.getByText('Crash me'))).resolves.not.toThrow();
      expect(throwingPerform).toHaveBeenCalled();
      // Palette still closes on perform — error contained
      expect(onOpenChange).toHaveBeenCalledWith(false);
    } finally {
      console.error = originalError;
    }
  });
});
