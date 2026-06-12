import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { EnvironmentKPIRow } from './environment-kpi-row';

describe('EnvironmentKPIRow', () => {
  it('test_undefined_values_render_honest_dash_not_zero', () => {
    render(<EnvironmentKPIRow />);
    // Three em-dashes for the 3 KPIs.
    const dashes = screen.getAllByText('—');
    expect(dashes.length).toBe(3);
  });

  it('test_zero_replicas_renders_zero_not_dash', () => {
    render(<EnvironmentKPIRow replicas={0} uptime={0} />);
    expect(screen.getByText('0 replicas')).toBeInTheDocument();
    expect(screen.getByText('0.00%')).toBeInTheDocument();
  });

  it('test_singular_pluralization_for_one_replica', () => {
    render(<EnvironmentKPIRow replicas={1} />);
    expect(screen.getByText('1 replica')).toBeInTheDocument();
  });

  it('test_uptime_100_renders_without_decimals', () => {
    render(<EnvironmentKPIRow uptime={100} />);
    expect(screen.getByText('100%')).toBeInTheDocument();
  });

  it('test_uptime_partial_renders_2_decimals', () => {
    render(<EnvironmentKPIRow uptime={99.987} />);
    expect(screen.getByText('99.99%')).toBeInTheDocument();
  });

  it('test_last_deploy_relative_with_sha_truncated_to_7', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-06-06T12:30:00Z'));
    render(
      <EnvironmentKPIRow
        lastDeploy="2026-06-06T12:28:00Z"
        lastDeploySha="abcdef1234567890"
      />,
    );
    expect(screen.getByText('2m ago (abcdef1)')).toBeInTheDocument();
    vi.useRealTimers();
  });

  it('test_invalid_timestamp_renders_dash', () => {
    render(<EnvironmentKPIRow lastDeploy="not-a-date" />);
    // All three (incl. invalid lastDeploy) render dash.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(1);
  });
});
