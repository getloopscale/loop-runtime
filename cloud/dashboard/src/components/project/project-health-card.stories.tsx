import type { Meta, StoryObj } from '@storybook/react';
import { ProjectHealthCard } from './project-health-card';
import type { ProjectStatusDetail } from '@/lib/api/types';

const baseStatus: ProjectStatusDetail = {
  project: 'acme-storefront',
  environment: 'production',
  replicas_ready: 3,
  replicas_desired: 3,
  deploy_status: 'succeeded',
  build_status: 'succeeded',
  traffic_5xx_pct_1h: 0.12,
  traffic_5xx_pct_24h: 0.34,
  alerts_active: 0,
  last_deploy_at: '2026-05-27T08:00:00Z',
};

const meta: Meta<typeof ProjectHealthCard> = {
  title: 'Project/ProjectHealthCard',
  component: ProjectHealthCard,
  tags: ['autodocs'],
};
export default meta;

type Story = StoryObj<typeof ProjectHealthCard>;

export const Healthy: Story = { args: { status: baseStatus } };

export const Degraded: Story = {
  args: {
    status: { ...baseStatus, traffic_5xx_pct_1h: 2.4, alerts_active: 2 },
  },
};

export const PartialReplicas: Story = {
  args: {
    status: { ...baseStatus, replicas_ready: 1, replicas_desired: 3 },
  },
};

export const Building: Story = {
  args: {
    status: { ...baseStatus, build_status: 'running', deploy_status: 'queued' },
  },
};
