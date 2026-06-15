import { Card, Dialog } from '@loopzero/ui';
import { AlertTriangle, BookOpen, Globe, HelpCircle } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

/**
 * J-11 Domains plan v1.1 T2.4 — DomainTroubleshootSheet.
 *
 * Drilldown sheet opened from a Failed/Degraded status pill click. Renders
 * canonical troubleshoot tips keyed on the backend `failure_mode` field.
 *
 * Per ADR D4: each mode has a 1-line tip that teaches the user the next
 * action. Unknown / absent `failure_mode` falls back to generic
 * "Contact support" copy — never a generic "Something went wrong" wall.
 */
export interface DomainTroubleshootSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Domain name shown in the sheet title. */
  domainName: string;
  /**
   * Backend-provided failure mode key. Known values mapped below;
   * undefined / unknown values render the generic fallback tip.
   */
  failureMode?: string;
}

interface Tip {
  icon: LucideIcon;
  title: string;
  body: string;
}

/**
 * Canonical mapping. New backend failure_mode values should be added here
 * so the user surface stays in sync with backend taxonomy.
 */
const TIPS: Record<string, Tip> = {
  'cert-manager error': {
    icon: AlertTriangle,
    title: 'Certificate issuance failed',
    body:
      'This usually means DNS propagation hasn\'t completed yet. Check at your registrar that the LoopZero-assigned nameservers are saved, then click Re-verify after a few minutes.',
  },
  'CF API error': {
    icon: Globe,
    title: 'Cloudflare API temporarily unavailable',
    body:
      'Cloudflare may be experiencing intermittent issues. Try Re-verify in a few minutes. If the issue persists, check the Cloudflare status page.',
  },
  'CF API timeout': {
    icon: Globe,
    title: 'Cloudflare API timeout',
    body:
      'The request to Cloudflare took longer than expected. Try Re-verify after a moment.',
  },
};

const GENERIC_TIP: Tip = {
  icon: HelpCircle,
  title: 'Need help?',
  body:
    'Contact support with the domain name and the time of this failure. We can investigate the backend state and the most recent verification attempt.',
};

export function DomainTroubleshootSheet({
  open,
  onOpenChange,
  domainName,
  failureMode,
}: DomainTroubleshootSheetProps) {
  const tip = failureMode ? (TIPS[failureMode] ?? GENERIC_TIP) : GENERIC_TIP;
  const Icon = tip.icon;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Troubleshoot {domainName}</Dialog.Title>
          <Dialog.Description>
            Steps you can try to resolve this domain&apos;s verification issue.
          </Dialog.Description>
        </Dialog.Header>
        <Card>
          <Card.Body>
            <div className="flex items-start gap-3">
              <Icon className="size-5 text-warning mt-0.5" aria-hidden="true" />
              <div className="flex-1">
                <p className="text-body font-medium">{tip.title}</p>
                <p className="text-body-sm text-muted-foreground mt-1">{tip.body}</p>
              </div>
            </div>
          </Card.Body>
        </Card>
        <div className="flex items-center gap-1 text-body-sm text-muted-foreground">
          <BookOpen className="size-4" aria-hidden="true" />
          <a
            href="https://docs.loopzero.example/domains/troubleshooting"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:underline"
          >
            Open the troubleshooting guide
          </a>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

/** Exported for hermetic tests so the canonical key set is auditable. */
export const __test_TIPS = TIPS;
export const __test_GENERIC_TIP = GENERIC_TIP;
