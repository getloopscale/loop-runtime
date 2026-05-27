import type { Meta, StoryObj } from '@storybook/react';
import { TeamSwitcher } from './team-switcher';

// TeamSwitcher consumes useWhoami internally. Without a mock the dropdown
// renders fallback name + empty teams. Story serves as visual baseline for
// the "logged-out / no-data" state — useful for verifying the empty path.
//
// For populated states, a future story can wrap with MSW or a hook stub via
// Storybook decorator. Today: keep KISS, show the chrome/disabled path.

const meta: Meta<typeof TeamSwitcher> = {
  title: 'Team/TeamSwitcher',
  component: TeamSwitcher,
  tags: ['autodocs'],
  args: {
    fallbackName: 'Demo Operator',
  },
};
export default meta;

type Story = StoryObj<typeof TeamSwitcher>;

export const FallbackState: Story = {};

export const WithCreateHandler: Story = {
  args: { onCreateTeam: () => alert('Create team clicked') },
};
