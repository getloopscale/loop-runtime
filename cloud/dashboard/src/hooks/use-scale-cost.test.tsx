import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import {
  DEFAULT_POD_COST_PER_HOUR,
  useScaleCost,
} from './use-scale-cost';

describe('useScaleCost', () => {
  it('test_estimates_730_hour_month_at_default_rate', () => {
    const { result } = renderHook(() =>
      useScaleCost({ replicas: 3, currentReplicas: 3 }),
    );
    // 3 * $0.05 * 730 = $109.50
    expect(result.current.estimatedMonthlyUSD).toBeCloseTo(
      3 * DEFAULT_POD_COST_PER_HOUR * 730,
      4,
    );
    expect(result.current.deltaUSD).toBe(0);
    expect(result.current.direction).toBe('flat');
  });

  it('test_positive_delta_when_scaling_up', () => {
    const { result } = renderHook(() =>
      useScaleCost({ replicas: 5, currentReplicas: 2 }),
    );
    // delta = (5 - 2) * 0.05 * 730 = $109.50
    expect(result.current.deltaUSD).toBeCloseTo(3 * 0.05 * 730, 4);
    expect(result.current.direction).toBe('up');
  });

  it('test_negative_delta_when_scaling_down', () => {
    const { result } = renderHook(() =>
      useScaleCost({ replicas: 1, currentReplicas: 4 }),
    );
    expect(result.current.deltaUSD).toBeLessThan(0);
    expect(result.current.direction).toBe('down');
  });

  it('test_honors_per_cluster_rate_override', () => {
    const { result } = renderHook(() =>
      useScaleCost({
        replicas: 2,
        currentReplicas: 2,
        podCostPerHour: 0.1, // higher-tier cluster rate
      }),
    );
    // 2 * $0.10 * 730 = $146.00
    expect(result.current.estimatedMonthlyUSD).toBeCloseTo(146, 4);
  });

  it('test_clamps_negative_replicas_to_zero', () => {
    const { result } = renderHook(() =>
      useScaleCost({ replicas: -5, currentReplicas: 3 }),
    );
    expect(result.current.estimatedMonthlyUSD).toBe(0);
    expect(result.current.direction).toBe('down');
  });
});
