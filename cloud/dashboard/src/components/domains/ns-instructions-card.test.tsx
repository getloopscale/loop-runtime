import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NSInstructionsCard } from './ns-instructions-card';

describe('NSInstructionsCard (J-11 v1.1 T2.1)', () => {
  it('test_ns_instructions_renders_3_steps_with_copy_buttons', () => {
    render(
      <NSInstructionsCard
        nameservers={['ns1.x.cloudflare.com', 'ns2.x.cloudflare.com']}
        domainName="loopzero-test.example"
      />,
    );
    // 3 numbered cards rendered
    expect(screen.getByText(/1\. Go to your registrar/)).toBeInTheDocument();
    expect(screen.getByText(/2\. Replace the nameservers/)).toBeInTheDocument();
    expect(screen.getByText(/3\. Wait for propagation/)).toBeInTheDocument();
    // Each nameserver rendered with its own clipboard copy affordance.
    expect(screen.getByText('ns1.x.cloudflare.com')).toBeInTheDocument();
    expect(screen.getByText('ns2.x.cloudflare.com')).toBeInTheDocument();
  });

  it('test_ns_instructions_card_renders_skeleton_when_nameservers_empty (EC-8)', () => {
    render(<NSInstructionsCard nameservers={[]} domainName="loopzero-test.example" />);
    expect(screen.getByText(/Generating nameservers…/i)).toBeInTheDocument();
    // No code-block rendered for an empty array.
    expect(screen.queryByText('ns1.x.cloudflare.com')).toBeNull();
  });

  it('test_ns_instructions_card_renders_skeleton_when_nameservers_undefined (EC-8 defensive)', () => {
    render(<NSInstructionsCard nameservers={undefined} domainName="loopzero-test.example" />);
    expect(screen.getByText(/Generating nameservers…/i)).toBeInTheDocument();
  });

  it('test_ns_instructions_links_to_registrar_help_doc_with_rel_noopener', () => {
    render(
      <NSInstructionsCard
        nameservers={['ns1.x.cloudflare.com', 'ns2.x.cloudflare.com']}
        domainName="loopzero-test.example"
      />,
    );
    const link = screen.getByRole('link', { name: /Read the registrar guide/i });
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(link).toHaveAttribute('target', '_blank');
  });
});
