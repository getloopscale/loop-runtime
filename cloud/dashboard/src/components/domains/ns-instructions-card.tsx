import { Card, CodeBlock } from '@loopzero/ui';
import { ExternalLink } from 'lucide-react';

/**
 * J-11 Domains plan v1.1 T2.1 — NSInstructionsCard.
 *
 * 3-step setup card shown after the user adds a custom domain via
 * `AddDomainSheet`. Mirrors the Vercel canonical UX: "go to registrar →
 * replace nameservers → wait for propagation".
 *
 * Per ADR D1: each nameserver has its own `<CopyButton>` so the user can
 * paste them one at a time into the registrar form. Per EC-8: empty
 * `nameservers` array renders a skeleton with "Generating nameservers…"
 * instead of broken empty code-blocks (race when CF Zone is still
 * provisioning).
 */
export interface NSInstructionsCardProps {
  /** Cloudflare-assigned nameservers from the canonical Create + Get response. */
  nameservers: string[] | undefined;
  /** The domain name the user is configuring (used in body copy). */
  domainName: string;
}

const REGISTRAR_HELP_URL = 'https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/';

export function NSInstructionsCard({ nameservers, domainName }: NSInstructionsCardProps) {
  const hasNameservers = Array.isArray(nameservers) && nameservers.length > 0;
  return (
    <Card>
      <Card.Header>
        <Card.Title>Configure DNS for {domainName}</Card.Title>
        <Card.Description>
          Three steps to activate this domain. LoopZero polls every 30s and updates
          the status automatically once propagation completes.
        </Card.Description>
      </Card.Header>
      <Card.Body>
        <ol className="space-y-6" aria-label="Domain configuration steps">
          <li>
            <p className="text-body font-medium">1. Go to your registrar</p>
            <p className="text-body-sm text-muted-foreground">
              Sign in to the DNS provider where this domain is registered (GoDaddy,
              Namecheap, Google Domains, etc.).
            </p>
            <a
              href={REGISTRAR_HELP_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="text-body-sm text-primary inline-flex items-center gap-1 mt-1 hover:underline"
            >
              Read the registrar guide
              <ExternalLink className="size-3" aria-hidden="true" />
            </a>
          </li>

          <li>
            <p className="text-body font-medium">2. Replace the nameservers</p>
            <p className="text-body-sm text-muted-foreground mb-2">
              Replace the existing nameservers with the two LoopZero-assigned values
              below.
            </p>
            {hasNameservers ? (
              <ul className="space-y-2" aria-label="Cloudflare nameservers">
                {nameservers!.map((ns) => (
                  <li key={ns} className="flex items-center gap-2">
                    <CodeBlock code={ns} copyable />
                  </li>
                ))}
              </ul>
            ) : (
              <p
                className="text-body-sm text-muted-foreground italic"
                aria-live="polite"
              >
                Generating nameservers…
              </p>
            )}
          </li>

          <li>
            <p className="text-body font-medium">3. Wait for propagation</p>
            <p className="text-body-sm text-muted-foreground">
              DNS changes can take up to 24 hours to propagate globally —
              typically much faster (minutes to a few hours). LoopZero will update
              the status automatically.
            </p>
          </li>
        </ol>
      </Card.Body>
    </Card>
  );
}
