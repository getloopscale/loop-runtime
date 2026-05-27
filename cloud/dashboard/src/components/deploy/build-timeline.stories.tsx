import type { Meta, StoryObj } from '@storybook/react';
import { BuildTimeline } from './build-timeline';

const meta: Meta<typeof BuildTimeline> = {
  title: 'Deploy/BuildTimeline',
  component: BuildTimeline,
  tags: ['autodocs'],
  argTypes: {
    status: {
      control: 'select',
      options: ['queued', 'running', 'succeeded', 'failed', 'cancelled', 'cancelling'],
    },
  },
};
export default meta;

type Story = StoryObj<typeof BuildTimeline>;

export const Queued: Story = { args: { status: 'queued' } };
export const Running: Story = { args: { status: 'running' } };
export const Succeeded: Story = { args: { status: 'succeeded' } };
export const Failed: Story = { args: { status: 'failed' } };
export const Cancelled: Story = { args: { status: 'cancelled' } };
