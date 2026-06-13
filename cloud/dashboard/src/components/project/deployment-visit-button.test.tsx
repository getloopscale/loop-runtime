import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { DeploymentVisitButton } from './deployment-visit-button';

const CANONICAL_URL = 'https://teste-production.loopzero.example';

describe('DeploymentVisitButton (T2.1 anti-trap UX)', () => {
  it('visible when build is succeeded AND active release AND url present', () => {
    render(
      <DeploymentVisitButton
        buildStatus="succeeded"
        isActiveRelease={true}
        url={CANONICAL_URL}
      />,
    );
    const link = screen.getByRole('link', { name: /visit/i });
    expect(link).toHaveAttribute('href', CANONICAL_URL);
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });

  it('HIDDEN when build is previous (not the active release)', () => {
    render(
      <DeploymentVisitButton
        buildStatus="succeeded"
        isActiveRelease={false}
        url={CANONICAL_URL}
      />,
    );
    expect(screen.queryByRole('link', { name: /visit/i })).toBeNull();
  });

  it('HIDDEN when build failed (even if active release)', () => {
    render(
      <DeploymentVisitButton
        buildStatus="failed"
        isActiveRelease={true}
        url={CANONICAL_URL}
      />,
    );
    expect(screen.queryByRole('link', { name: /visit/i })).toBeNull();
  });

  it('HIDDEN when url is undefined (status not yet populated)', () => {
    render(
      <DeploymentVisitButton
        buildStatus="succeeded"
        isActiveRelease={true}
        url={undefined}
      />,
    );
    expect(screen.queryByRole('link', { name: /visit/i })).toBeNull();
  });
});
