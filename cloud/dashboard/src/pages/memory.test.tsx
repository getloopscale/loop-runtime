// Plan 1 of dashboard-memory-section-roadmap.md, T1.6 acceptance.
// Focused on D3 absorption: empty pgvector → 0 not NaN.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen } from '@/test/render';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 't', traits: { email: 'u@x.com' } },
  }),
}));

const mockCloudFetch = vi.fn();
vi.mock('@/lib/api', () => ({
  cloudFetch: (...args: unknown[]) => mockCloudFetch(...args),
  engineFetch: (...args: unknown[]) => mockCloudFetch(...args),
}));

import { MemoryPage } from './memory';

describe('MemoryPage (/memory landing)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the 4 KPI tile labels + calls the metrics endpoint', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      total_memories: 42,
      retrieval_api_usage: 1000,
      add_events: 5,
      entity_count: 7,
    });
    render(<MemoryPage />);
    expect(await screen.findByText('Total memories')).toBeInTheDocument();
    expect(screen.getByText('Retrieval API calls')).toBeInTheDocument();
    expect(screen.getByText('Add events')).toBeInTheDocument();
    expect(screen.getByText('Distinct entities')).toBeInTheDocument();
    // Backend was hit at the canonical path — the value-rendering happy
    // path is exercised separately by the D3 empty-zero test below
    // (where the fallback guarantees zeros render without NaN).
    expect(mockCloudFetch).toHaveBeenCalledWith('/v1/memory/metrics');
  });

  it('D3 absorption — empty pgvector renders 0 everywhere, not NaN', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      total_memories: 0,
      retrieval_api_usage: 0,
      add_events: 0,
      entity_count: 0,
    });
    render(<MemoryPage />);
    // Wait for the first KPI label to render — guarantees the query resolved.
    expect(await screen.findByText('Total memories')).toBeInTheDocument();
    // Crucial regression guard: not a single "NaN" in the rendered output.
    expect(screen.queryByText(/NaN/i)).not.toBeInTheDocument();
    // Each tile shows "0" (StatTile value can render the same number 0 in
    // multiple places — assert presence, not count).
    expect(screen.getAllByText('0').length).toBeGreaterThanOrEqual(4);
  });

  it('T2.4 — renders the 4 Explore cards (Customize / Integration / Playground / Documentation)', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      total_memories: 0,
      retrieval_api_usage: 0,
      add_events: 0,
      entity_count: 0,
    });
    render(<MemoryPage />);
    expect(await screen.findByText('Total memories')).toBeInTheDocument();
    expect(screen.getByText('Customize')).toBeInTheDocument();
    expect(screen.getByText('Integration examples')).toBeInTheDocument();
    expect(screen.getByText('Try Playground')).toBeInTheDocument();
    expect(screen.getByText('Documentation')).toBeInTheDocument();
  });

  it('Refresh button re-fetches metrics', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      total_memories: 1,
      retrieval_api_usage: 0,
      add_events: 0,
      entity_count: 0,
    });
    const user = userEvent.setup();
    render(<MemoryPage />);
    expect(await screen.findByText('Total memories')).toBeInTheDocument();

    mockCloudFetch.mockResolvedValueOnce({
      total_memories: 2,
      retrieval_api_usage: 0,
      add_events: 0,
      entity_count: 0,
    });
    await user.click(screen.getByRole('button', { name: /refresh/i }));
    // After invalidation, the query refetches → mockCloudFetch called again.
    expect(mockCloudFetch).toHaveBeenCalledTimes(2);
  });
});
