/**
 * kpi-strip-status.test.tsx — vitest hermetic tests for T3.1 J-18.
 *
 * Asserts:
 *  - Renders all 5 KPI cards (Replicas, Uptime 24h, p95 Latency, Error Rate, RPS)
 *  - All 5 cards have tabular-nums class
 *  - Null/undefined values render as "—" honest (NOT 0)
 *  - Tone classes apply per value thresholds (success/warning/destructive/muted)
 *  - Percentage values render with 2-decimal precision
 */

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import { KPIStripStatus } from './kpi-strip-status';

describe('KPIStripStatus — T3.1 J-18', () => {
  it('renders 5 KPI cards with their labels', () => {
    render(
      <KPIStripStatus
        replicas={3}
        uptime24h={99.95}
        p95LatencyMs={120}
        errorRate={0.1}
        rps={42}
      />,
    );
    expect(screen.getByText('Replicas')).toBeInTheDocument();
    expect(screen.getByText('Uptime 24h')).toBeInTheDocument();
    expect(screen.getByText('p95 Latency')).toBeInTheDocument();
    expect(screen.getByText('Error Rate')).toBeInTheDocument();
    expect(screen.getByText('RPS')).toBeInTheDocument();
  });

  it('renders 5 cards in the grid (group role)', () => {
    const { container } = render(
      <KPIStripStatus replicas={3} uptime24h={99} p95LatencyMs={100} errorRate={0} rps={10} />,
    );
    const group = container.querySelector('[role="group"]');
    expect(group).toBeInTheDocument();
    expect(group?.getAttribute('aria-label')).toBe('Project KPI strip');
  });

  it('all values render with tabular-nums for proper alignment', () => {
    const { container } = render(
      <KPIStripStatus replicas={3} uptime24h={99} p95LatencyMs={100} errorRate={0} rps={10} />,
    );
    const tabularValues = container.querySelectorAll('.tabular-nums');
    expect(tabularValues.length).toBe(5);
  });

  it('null values render as em-dash "—" (honest empty, NOT 0)', () => {
    render(
      <KPIStripStatus
        replicas={null}
        uptime24h={null}
        p95LatencyMs={null}
        errorRate={null}
        rps={null}
      />,
    );
    const emDashes = screen.getAllByText('—');
    expect(emDashes.length).toBe(5);
  });

  it('undefined values render as em-dash "—"', () => {
    render(<KPIStripStatus />);
    const emDashes = screen.getAllByText('—');
    expect(emDashes.length).toBe(5);
  });

  it('uptime 99.95% renders with 2-decimal precision and success tone', () => {
    const { container } = render(<KPIStripStatus uptime24h={99.95} />);
    expect(screen.getByText('99.95%')).toBeInTheDocument();
    expect(container.querySelector('.text-success')).toBeInTheDocument();
  });

  it('uptime 98.0% renders destructive tone (below 99%)', () => {
    const { container } = render(<KPIStripStatus uptime24h={98.0} />);
    expect(screen.getByText('98.00%')).toBeInTheDocument();
    expect(container.querySelector('.text-destructive')).toBeInTheDocument();
  });

  it('error rate 0.10% renders success tone', () => {
    const { container } = render(<KPIStripStatus errorRate={0.1} />);
    expect(screen.getByText('0.10%')).toBeInTheDocument();
    expect(container.querySelector('.text-success')).toBeInTheDocument();
  });

  it('error rate 6.5% renders destructive tone (above 5%)', () => {
    const { container } = render(<KPIStripStatus errorRate={6.5} />);
    expect(screen.getByText('6.50%')).toBeInTheDocument();
    expect(container.querySelector('.text-destructive')).toBeInTheDocument();
  });

  it('p95 latency 1200ms renders destructive tone (above 1000ms)', () => {
    const { container } = render(<KPIStripStatus p95LatencyMs={1200} />);
    expect(screen.getByText('1200ms')).toBeInTheDocument();
    expect(container.querySelector('.text-destructive')).toBeInTheDocument();
  });

  it('replicas 0 renders destructive tone (no replicas = problem)', () => {
    const { container } = render(<KPIStripStatus replicas={0} />);
    expect(screen.getByText('0')).toBeInTheDocument();
    expect(container.querySelector('.text-destructive')).toBeInTheDocument();
  });

  it('partial fixture (3 of 5 set) renders mix of values + em-dashes', () => {
    render(
      <KPIStripStatus
        replicas={3}
        uptime24h={99.5}
        // p95, errorRate, rps undefined
      />,
    );
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('99.50%')).toBeInTheDocument();
    const emDashes = screen.getAllByText('—');
    expect(emDashes.length).toBe(3); // p95, errorRate, rps
  });
});
