import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { ReadinessChecklist } from './readiness-checklist';
import { SIGNALS } from '@/lib/readiness/signals';
import type { ReadinessSignal } from '@/hooks/use-readiness-signals';

function buildSignals(overrides: Partial<Record<string, Partial<ReadinessSignal>>> = {}): Record<string, ReadinessSignal> {
  return Object.fromEntries(
    SIGNALS.map((def) => [
      def.name,
      {
        name: def.name,
        status: 'pass',
        weight: def.weight,
        fixPath: def.fixPath,
        ...(overrides[def.name] ?? {}),
      } as ReadinessSignal,
    ]),
  );
}

function renderWith(signals: Record<string, ReadinessSignal>) {
  return render(
    <MemoryRouter>
      <ReadinessChecklist signals={signals} projectName="my-api" />
    </MemoryRouter>,
  );
}

describe('ReadinessChecklist', () => {
  it('test_checklist_renders_tristate_icons_per_adr_d3 (EC-3)', () => {
    const signals = buildSignals({
      auto_deploy: { status: 'pass' },
      hpa_configured: { status: 'fail' },
      cache_hit_rate: { status: 'unknown' },
    });
    renderWith(signals);

    // ADR D3 canonical aria-label strings.
    expect(screen.getAllByLabelText('Ready').length).toBeGreaterThan(0);
    expect(screen.getAllByLabelText('Action required').length).toBeGreaterThan(0);
    expect(screen.getAllByLabelText('Not measurable right now').length).toBeGreaterThan(0);
  });

  it('test_checklist_fix_link_implemented_route_renders_link', () => {
    // Only HPA fail → only one Fix-it link rendered + it must point to /scale.
    const signals = buildSignals(
      Object.fromEntries(SIGNALS.map((s) => [s.name, { status: 'unknown' }])),
    );
    signals.hpa_configured = {
      name: 'hpa_configured',
      status: 'fail',
      weight: 20,
      fixPath: '/projects/:name/scale',
    };
    renderWith(signals);

    const links = screen.getAllByRole('link', { name: /Fix it/i });
    expect(links).toHaveLength(1);
    expect(links[0]).toHaveAttribute('href', '/projects/my-api/scale');
  });

  it('test_checklist_fix_link_unimplemented_route_renders_coming_soon (EC-7)', () => {
    const signals = buildSignals({
      custom_domain: { status: 'fail' }, // fixPath: /projects/:name/domains (NOT in KNOWN_ROUTES)
    });
    renderWith(signals);
    // At least one "Coming soon" span renders for unimplemented J-11 route.
    expect(screen.getAllByText(/Coming soon/i).length).toBeGreaterThan(0);
  });

  it('test_checklist_unknown_signal_does_not_render_fix_link', () => {
    // All signals unknown → zero "Fix it" links + zero "Coming soon" spans
    // (Unknown rows have no actionable affordance).
    const signals = buildSignals(
      Object.fromEntries(SIGNALS.map((s) => [s.name, { status: 'unknown' }])),
    );
    renderWith(signals);
    expect(screen.queryAllByRole('link', { name: /Fix it/i })).toHaveLength(0);
    expect(screen.queryAllByText(/Coming soon/i)).toHaveLength(0);
  });
});
