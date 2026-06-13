import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { DeploymentRowVisitIcon } from './deployment-row-visit-icon';

const CANONICAL_URL = 'https://teste-production.loopzero.example';

describe('DeploymentRowVisitIcon (T3.1 active-only affordance)', () => {
  it('renders ExternalLink anchor only when isActive AND url present', () => {
    render(<DeploymentRowVisitIcon isActive={true} url={CANONICAL_URL} />);
    const link = screen.getByRole('link', { name: /open live url/i });
    expect(link).toHaveAttribute('href', CANONICAL_URL);
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });

  it('renders nothing when isActive is false (previous deploy row)', () => {
    const { container } = render(
      <DeploymentRowVisitIcon isActive={false} url={CANONICAL_URL} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing when url is undefined (status not ready)', () => {
    const { container } = render(
      <DeploymentRowVisitIcon isActive={true} url={undefined} />,
    );
    expect(container.firstChild).toBeNull();
  });
});
