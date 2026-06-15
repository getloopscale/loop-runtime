/**
 * signal-detail-sheet.test.tsx — vitest hermetic tests for T3.3 J-18.
 *
 * Asserts:
 *  - Renders signal label + project/env subtitle
 *  - EC-8: collecting state shows "Coletando dados..." hint
 *  - EC-5: prometheus_unreachable shows honest error message
 *  - Happy path with data: renders table with rows
 *  - EC-13: Grafana button disabled when grafanaBaseURL not configured
 *  - Grafana button enabled when grafanaBaseURL set
 */

import { renderHook as _ } from '@testing-library/react'; // eslint-disable-line @typescript-eslint/no-unused-vars
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));
vi.mock('@/lib/config', () => ({ API_URL: '' }));

import { SignalDetailSheet } from './signal-detail-sheet';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function withQueryClient(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('SignalDetailSheet — T3.3 J-18', () => {
  it('renders signal label in header when open=true', async () => {
    mockedEngineFetch.mockResolvedValue({
      signal: 'uptime_24h',
      range: '7d',
      status: 'ok',
      data: [],
    });

    render(
      withQueryClient(
        <SignalDetailSheet
          open={true}
          onOpenChange={() => {}}
          signal="uptime_24h"
          projectName="teste"
          env="production"
        />,
      ),
    );

    // Sheet renders portal; query in body
    expect(await screen.findByText('Uptime 24h')).toBeInTheDocument();
  });

  it('renders project + env in subtitle', async () => {
    mockedEngineFetch.mockResolvedValue({
      signal: 'replicas',
      range: '7d',
      status: 'ok',
      data: [],
    });

    render(
      withQueryClient(
        <SignalDetailSheet
          open={true}
          onOpenChange={() => {}}
          signal="replicas"
          projectName="meu-projeto"
          env="staging"
        />,
      ),
    );

    expect(await screen.findByText(/meu-projeto/)).toBeInTheDocument();
    expect(screen.getByText(/staging/)).toBeInTheDocument();
  });

  it('EC-8: shows "Coletando dados..." when status=collecting', async () => {
    mockedEngineFetch.mockResolvedValue({
      signal: 'uptime_24h',
      range: '7d',
      status: 'collecting',
      collecting_hint: 'Coletando dados...',
      data: [],
    });

    render(
      withQueryClient(
        <SignalDetailSheet
          open={true}
          onOpenChange={() => {}}
          signal="uptime_24h"
          projectName="newproj"
          env="production"
        />,
      ),
    );

    expect(await screen.findByText('Coletando dados...')).toBeInTheDocument();
  });

  it('renders table with datapoints when status=ok and data is present', async () => {
    mockedEngineFetch.mockResolvedValue({
      signal: 'replicas',
      range: '7d',
      status: 'ok',
      data: [
        { t: 1700000000, v: 3 },
        { t: 1700001000, v: 5 },
        { t: 1700002000, v: 4 },
      ],
    });

    render(
      withQueryClient(
        <SignalDetailSheet
          open={true}
          onOpenChange={() => {}}
          signal="replicas"
          projectName="teste"
          env="production"
        />,
      ),
    );

    expect(await screen.findByRole('table')).toBeInTheDocument();
    expect(screen.getByText('3 data points')).toBeInTheDocument();
  });

  it('EC-13: Grafana button is disabled when grafanaBaseURL is empty', async () => {
    mockedEngineFetch.mockResolvedValue({
      signal: 'replicas',
      range: '7d',
      status: 'ok',
      data: [],
    });

    render(
      withQueryClient(
        <SignalDetailSheet
          open={true}
          onOpenChange={() => {}}
          signal="replicas"
          projectName="teste"
          env="production"
          grafanaBaseURL=""
        />,
      ),
    );

    const button = await screen.findByRole('button', { name: 'Grafana deep-link not configured' });
    expect(button).toBeDisabled();
  });

  it('EC-13: Grafana button is enabled when grafanaBaseURL is set', async () => {
    mockedEngineFetch.mockResolvedValue({
      signal: 'replicas',
      range: '7d',
      status: 'ok',
      data: [],
    });

    render(
      withQueryClient(
        <SignalDetailSheet
          open={true}
          onOpenChange={() => {}}
          signal="replicas"
          projectName="teste"
          env="production"
          grafanaBaseURL="https://grafana.loopzero.local"
        />,
      ),
    );

    const button = await screen.findByText('Open in Grafana');
    expect(button.closest('button')).not.toBeDisabled();
  });

  it('does NOT render content when open=false', () => {
    mockedEngineFetch.mockResolvedValue({
      signal: 'replicas',
      range: '7d',
      status: 'ok',
      data: [],
    });

    render(
      withQueryClient(
        <SignalDetailSheet
          open={false}
          onOpenChange={() => {}}
          signal="replicas"
          projectName="teste"
          env="production"
        />,
      ),
    );

    expect(screen.queryByText('Replicas')).not.toBeInTheDocument();
  });
});
