import { describe, it, expect, vi } from 'vitest';
import { queryClient, invalidate } from './query-client';
import { QueryClient } from '@tanstack/react-query';

describe('queryClient', () => {
  it('test_singleton_exports_queryClient', () => {
    expect(queryClient).toBeInstanceOf(QueryClient);
  });

  it('test_default_staleTime_60s', () => {
    const defaults = queryClient.getDefaultOptions();
    expect(defaults.queries?.staleTime).toBe(60_000);
  });

  it('test_default_refetchOnWindowFocus_true', () => {
    const defaults = queryClient.getDefaultOptions();
    expect(defaults.queries?.refetchOnWindowFocus).toBe(true);
  });

  it('test_mutations_do_not_retry', () => {
    const defaults = queryClient.getDefaultOptions();
    expect(defaults.mutations?.retry).toBe(0);
  });

  it('test_invalidate_helper_calls_invalidateQueries', () => {
    const spy = vi.spyOn(queryClient, 'invalidateQueries');
    invalidate(['projects']);
    expect(spy).toHaveBeenCalledWith({ queryKey: ['projects'] });
    spy.mockRestore();
  });
});
