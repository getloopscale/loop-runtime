/**
 * signal-grid.test.tsx — vitest hermetic tests for T3.2 J-18.
 *
 * Asserts (per plan v1.1 EC-7 absorption):
 *  - Renders 8 SignalPills regardless of partial values
 *  - Null/undefined values render as "—" honest
 *  - Click on pill emits onDrillDown(signalName)
 *  - Keyboard Enter/Space activates drill-down for accessibility
 *  - Non-clickable when onDrillDown not provided
 *  - Status pill colors: Healthy=success, Degraded=destructive
 *  - Replicas=0 → destructive tone
 *  - Alerts >=1 → destructive tone
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

import { SignalGrid } from './signal-grid';

describe('SignalGrid — T3.2 J-18 (EC-7 absorption: 8 pills always render)', () => {
  it('renders all 8 SignalPills with their labels', () => {
    render(
      <SignalGrid
        signals={{
          status: 'Healthy',
          replicas: 3,
          uptime_24h: 99.95,
          p95_latency: 120,
          error_rate: 0.1,
          rps: 42,
          alerts_active: 0,
          last_deploy_time: new Date(Date.now() - 60_000).toISOString(),
        }}
      />,
    );
    // 8 labels expected
    const labels = ['Status', 'Replicas', 'Uptime 24h', 'p95 Latency', 'Error Rate', 'RPS', 'Alerts', 'Last deploy'];
    labels.forEach((l) => expect(screen.getByText(l)).toBeInTheDocument());
  });

  it('EC-7 absorption: renders 8 pills with 5 undefined values rendering as "—"', () => {
    render(
      <SignalGrid
        signals={{
          status: 'Healthy',
          replicas: 3,
          uptime_24h: 99.95,
          // p95_latency, error_rate, rps, alerts_active, last_deploy_time undefined
        }}
      />,
    );
    const emDashes = screen.getAllByText('—');
    expect(emDashes.length).toBe(5);
  });

  it('renders 8 pills even when ALL values are undefined', () => {
    render(<SignalGrid signals={{}} />);
    const emDashes = screen.getAllByText('—');
    expect(emDashes.length).toBe(8);
  });

  it('null values render as "—" honest (NOT 0)', () => {
    render(
      <SignalGrid
        signals={{
          status: null,
          replicas: null,
          uptime_24h: null,
          p95_latency: null,
          error_rate: null,
          rps: null,
          alerts_active: null,
          last_deploy_time: null,
        }}
      />,
    );
    const emDashes = screen.getAllByText('—');
    expect(emDashes.length).toBe(8);
  });

  it('click on pill emits onDrillDown(signalName)', () => {
    const onDrillDown = vi.fn();
    render(
      <SignalGrid
        signals={{ status: 'Healthy', replicas: 3 }}
        onDrillDown={onDrillDown}
      />,
    );
    const replicasButton = screen.getByRole('button', { name: /Replicas: 3/ });
    fireEvent.click(replicasButton);
    expect(onDrillDown).toHaveBeenCalledWith('replicas');
  });

  it('keyboard Enter activates drill-down for accessibility', () => {
    const onDrillDown = vi.fn();
    render(
      <SignalGrid
        signals={{ status: 'Healthy' }}
        onDrillDown={onDrillDown}
      />,
    );
    const statusButton = screen.getByRole('button', { name: /Status: Healthy/ });
    fireEvent.keyDown(statusButton, { key: 'Enter' });
    expect(onDrillDown).toHaveBeenCalledWith('status');
  });

  it('keyboard Space activates drill-down', () => {
    const onDrillDown = vi.fn();
    render(
      <SignalGrid
        signals={{ replicas: 3 }}
        onDrillDown={onDrillDown}
      />,
    );
    const button = screen.getByRole('button', { name: /Replicas: 3/ });
    fireEvent.keyDown(button, { key: ' ' });
    expect(onDrillDown).toHaveBeenCalledWith('replicas');
  });

  it('pills are NOT clickable when onDrillDown is not provided', () => {
    render(<SignalGrid signals={{ status: 'Healthy' }} />);
    const buttons = screen.queryAllByRole('button');
    expect(buttons.length).toBe(0);
  });

  it('Status=Healthy renders with success tone', () => {
    const { container } = render(<SignalGrid signals={{ status: 'Healthy' }} />);
    expect(container.querySelector('.text-success')).toBeInTheDocument();
  });

  it('Status=Degraded renders with destructive tone', () => {
    const { container } = render(<SignalGrid signals={{ status: 'Degraded' }} />);
    expect(container.querySelector('.text-destructive')).toBeInTheDocument();
  });

  it('Replicas=0 renders with destructive tone', () => {
    const { container } = render(<SignalGrid signals={{ replicas: 0 }} />);
    expect(screen.getByText('0')).toBeInTheDocument();
    expect(container.querySelector('.text-destructive')).toBeInTheDocument();
  });

  it('Alerts >=1 renders with destructive tone', () => {
    const { container } = render(<SignalGrid signals={{ alerts_active: 3 }} />);
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(container.querySelector('.text-destructive')).toBeInTheDocument();
  });

  it('Uptime 98% renders with destructive tone', () => {
    const { container } = render(<SignalGrid signals={{ uptime_24h: 98 }} />);
    expect(screen.getByText('98.00%')).toBeInTheDocument();
    expect(container.querySelector('.text-destructive')).toBeInTheDocument();
  });

  it('last_deploy_time renders relative format', () => {
    const ts = new Date(Date.now() - 5 * 60_000).toISOString(); // 5 min ago
    render(<SignalGrid signals={{ last_deploy_time: ts }} />);
    expect(screen.getByText(/m ago/)).toBeInTheDocument();
  });
});
