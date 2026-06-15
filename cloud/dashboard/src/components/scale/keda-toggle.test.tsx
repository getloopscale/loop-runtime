import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { KEDAToggle } from './keda-toggle';

describe('KEDAToggle', () => {
  it('test_starter_shows_upgrade_cta_and_disables_toggle', () => {
    render(
      <KEDAToggle
        enabled={false}
        planTier="starter"
        onChange={vi.fn()}
        clusterKedaInstalled={true}
      />,
    );
    const checkbox = screen.getByRole('checkbox');
    expect(checkbox).toBeDisabled();
    expect(screen.getByText(/Pro plan required/i)).toBeInTheDocument();
    // The upgrade CTA is an <a> tag inside an Alert description. Query by
    // href since the text spans element boundaries inside the rendered Alert.
    const upgradeLink = document.querySelector('a[href="/billing"]');
    expect(upgradeLink).not.toBeNull();
    expect(upgradeLink?.textContent).toMatch(/Upgrade to Pro/i);
  });

  it('test_pro_with_cluster_keda_enables_toggle', () => {
    render(
      <KEDAToggle
        enabled={false}
        planTier="pro"
        onChange={vi.fn()}
        clusterKedaInstalled={true}
      />,
    );
    const checkbox = screen.getByRole('checkbox');
    expect(checkbox).not.toBeDisabled();
    expect(screen.queryByText(/Pro plan required/i)).toBeNull();
  });

  it('test_pro_without_cluster_keda_shows_cluster_warning', () => {
    render(
      <KEDAToggle
        enabled={false}
        planTier="pro"
        onChange={vi.fn()}
        clusterKedaInstalled={false}
      />,
    );
    expect(screen.getByText(/Not available on this cluster/i)).toBeInTheDocument();
  });

  it('test_enterprise_eligible_like_pro_and_team', () => {
    render(
      <KEDAToggle
        enabled={true}
        planTier="enterprise"
        onChange={vi.fn()}
        clusterKedaInstalled={true}
      />,
    );
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox).not.toBeDisabled();
    expect(checkbox.checked).toBe(true);
  });
});
