import { useState } from 'react';
import { Card } from '@loopzero/ui';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { SIGNALS, type SignalName } from '@/lib/readiness/signals';

/**
 * J-21 Readiness Score plan v1.1 T2.3 — ReadinessFAQ.
 *
 * Collapsible explainer for each of the 8 signals. Each row expands on
 * click to reveal the `description` field from `signals.ts`. The page
 * can deep-link to a specific row via `?signal=<name>` URL search param
 * (T3.2) by passing `defaultOpen` for that row.
 */
export interface ReadinessFAQProps {
  /** Signal name to expand on initial render (T3.2 deep-link absorption). */
  defaultOpen?: SignalName;
}

export function ReadinessFAQ({ defaultOpen }: ReadinessFAQProps) {
  return (
    <Card>
      <Card.Header>
        <Card.Title>What each signal measures</Card.Title>
        <Card.Description>
          Click a row to expand the explanation. Each signal feeds the score
          per its weight (per plan ADR D2).
        </Card.Description>
      </Card.Header>
      <Card.Body>
        <ul className="divide-y divide-border" aria-label="Readiness FAQ">
          {SIGNALS.map((def) => (
            <FAQRow
              key={def.name}
              name={def.name}
              label={def.label}
              description={def.description}
              defaultOpen={defaultOpen === def.name}
            />
          ))}
        </ul>
      </Card.Body>
    </Card>
  );
}

interface FAQRowProps {
  name: SignalName;
  label: string;
  description: string;
  defaultOpen: boolean;
}

function FAQRow({ name, label, description, defaultOpen }: FAQRowProps) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <li id={`faq-${name}`} className="py-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-2 text-left text-body font-medium hover:text-primary"
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className="size-4" aria-hidden="true" />
        ) : (
          <ChevronRight className="size-4" aria-hidden="true" />
        )}
        <span>{label}</span>
      </button>
      {open && (
        <p className="mt-2 ml-6 text-body-sm text-muted-foreground">{description}</p>
      )}
    </li>
  );
}
