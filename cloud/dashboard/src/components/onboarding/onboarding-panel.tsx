import { useState } from 'react';
import { Check, ChevronDown, Rocket, X } from 'lucide-react';
import { Button, CopyButton } from '@loopzero/ui';
import { useOnboardingState } from '@/hooks/use-onboarding-state';
import { reportEvent } from '@/lib/rum';

/**
 * OnboardingPanel (Sprint 3 T3.2 v2) — Linear/Vercel 2026 floating pattern.
 *
 * Replaces the cramped sidebar widget (220px wide, hard to read) with a
 * floating panel bottom-right (360x480 expanded) + circular progress badge
 * (52x52 collapsed). Per 2026 FAANG synthesis:
 *
 * - Floating position NEVER competes with sidebar nav real estate
 * - Larger surface = legible CLI commands + full descriptions
 * - Collapsed badge shows progress ring — user always aware without forced UI
 * - Dismiss collapses to badge (state preserved); long-dismiss via Settings
 *
 * Anti-patterns avoided:
 * - ❌ Modal jail (full-screen takeover)
 * - ❌ Forced wizard with Next buttons
 * - ❌ Tiny sidebar widget that competes with nav
 * - ❌ Persistent always-expanded panel (blocks content)
 *
 * EC-17: CopyButton from @loopzero/ui handles clipboard fallback (HTTPS vs HTTP).
 */

interface ChecklistStep {
  key: 'cli_installed' | 'logged_in' | 'project_initialized' | 'deploy_succeeded' | 'domain_added';
  title: string;
  description: string;
  cliCommand?: string;
  optional?: boolean;
}

const STEPS: ChecklistStep[] = [
  {
    key: 'cli_installed',
    title: 'Install the LoopZero CLI',
    description: 'The CLI is your primary tool. Dashboard is for status + history.',
    cliCommand: 'npm install -g @loopzero/cli',
  },
  {
    key: 'logged_in',
    title: 'Sign in',
    description: 'Connect the CLI to your LoopZeroCloud team.',
    cliCommand: 'loopzero login',
  },
  {
    key: 'project_initialized',
    title: 'Initialize your first project',
    description: 'Connect a repo or start from a starter template.',
    cliCommand: 'loopzero init',
  },
  {
    key: 'deploy_succeeded',
    title: 'Deploy your first project',
    description: 'Goes from your laptop to a live URL in about 4 minutes.',
    cliCommand: 'loopzero deploy',
  },
  {
    key: 'domain_added',
    title: 'Add a custom domain',
    description: 'Point your own domain at the deploy. TLS handled automatically.',
    cliCommand: 'loopzero domain add app.example.com',
    optional: true,
  },
];

export function OnboardingPanel() {
  const { state, completed, total, percent, dismiss, markStepDone, isVisible } =
    useOnboardingState();
  // Local UI state — collapsed (badge only) vs expanded (full panel)
  // Default collapsed: user opts in to see full panel via badge click.
  const [expanded, setExpanded] = useState(false);

  if (!isVisible) return null;

  const activeKey = STEPS.find((s) => !state.steps[s.key])?.key;

  // Collapsed: floating badge with progress ring
  if (!expanded) {
    return (
      <button
        type="button"
        onClick={() => setExpanded(true)}
        className="fixed bottom-6 right-6 z-50 size-14 rounded-full bg-primary text-primary-foreground shadow-lg hover:scale-105 transition-transform flex items-center justify-center group"
        aria-label={`Get your app live — ${completed} of ${total} done. Click to expand.`}
      >
        <ProgressRing percent={percent} size={56} />
        <Rocket
          className="size-5 absolute"
          aria-hidden="true"
        />
        <span className="absolute -top-1 -right-1 bg-background text-foreground border border-border rounded-full text-[10px] font-mono tabular-nums px-1.5 py-0.5 leading-none">
          {completed}/{total}
        </span>
      </button>
    );
  }

  // Expanded: full panel
  return (
    <div
      className="fixed bottom-6 right-6 z-50 w-[380px] max-h-[calc(100vh-3rem)] rounded-xl border border-border bg-background shadow-2xl flex flex-col overflow-hidden"
      role="dialog"
      aria-label="Onboarding panel"
    >
      {/* Header with progress + collapse + dismiss */}
      <div className="px-5 py-4 border-b border-border/40 bg-gradient-to-br from-primary/5 to-transparent">
        <div className="flex items-start justify-between gap-3 mb-3">
          <div className="flex items-center gap-3 min-w-0">
            <div className="flex size-10 items-center justify-center rounded-xl bg-primary/10 border border-primary/20 shrink-0">
              <Rocket className="size-5 text-primary" aria-hidden="true" />
            </div>
            <div className="min-w-0">
              <h2 className="font-display text-body-md font-semibold text-foreground">
                Get your app live
              </h2>
              <p className="text-label text-muted-foreground">
                {completed === total
                  ? 'All set! 🎉'
                  : `${completed} of ${total} steps complete`}
              </p>
            </div>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            <button
              type="button"
              onClick={() => setExpanded(false)}
              className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/40"
              aria-label="Collapse to badge"
            >
              <ChevronDown className="size-4" aria-hidden="true" />
            </button>
            <button
              type="button"
              onClick={() => {
                dismiss();
                reportEvent('onboarding_dismissed');
              }}
              className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/40"
              aria-label="Dismiss onboarding"
            >
              <X className="size-4" aria-hidden="true" />
            </button>
          </div>
        </div>

        {/* Larger progress bar — prominent visual */}
        <div className="h-2 rounded-full bg-muted overflow-hidden">
          <div
            className="h-full bg-gradient-to-r from-primary to-primary/80 transition-all duration-500 ease-out"
            style={{ width: `${percent}%` }}
            aria-label={`Onboarding progress ${percent}%`}
          />
        </div>
      </div>

      {/* Scrollable step list — rich content per step */}
      <div className="flex-1 overflow-y-auto px-3 py-3 space-y-2">
        {STEPS.map((step, idx) => {
          const done = state.steps[step.key];
          const isActive = step.key === activeKey;
          return (
            <div
              key={step.key}
              className={`rounded-lg border p-3 transition-colors ${
                done
                  ? 'border-border/30 bg-muted/10'
                  : isActive
                    ? 'border-primary/40 bg-primary/5'
                    : 'border-border/30'
              }`}
            >
              <div className="flex items-start gap-3">
                <div className="shrink-0 mt-0.5">
                  {done ? (
                    <div className="flex size-6 items-center justify-center rounded-full bg-success/15 border border-success/30">
                      <Check className="size-3.5 text-success" aria-label="done" />
                    </div>
                  ) : (
                    <div
                      className={`flex size-6 items-center justify-center rounded-full border-2 text-label font-mono tabular-nums ${
                        isActive
                          ? 'border-primary text-primary'
                          : 'border-border text-muted-foreground'
                      }`}
                      aria-label={isActive ? 'active step' : 'pending'}
                    >
                      {idx + 1}
                    </div>
                  )}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <p
                      className={`text-body-sm font-medium ${
                        done ? 'text-muted-foreground line-through' : 'text-foreground'
                      }`}
                    >
                      {step.title}
                    </p>
                    {step.optional && (
                      <span className="text-label text-muted-foreground uppercase">
                        optional
                      </span>
                    )}
                  </div>
                  <p className="text-label text-muted-foreground mt-0.5">
                    {step.description}
                  </p>

                  {/* CLI command — visible for ALL incomplete steps (not just active) */}
                  {!done && step.cliCommand && (
                    <div className="mt-2 flex items-center gap-2 bg-muted/40 rounded-md px-2.5 py-1.5 font-mono text-[12px]">
                      <span className="text-muted-foreground">$</span>
                      <span className="flex-1 truncate text-foreground">
                        {step.cliCommand}
                      </span>
                      <CopyButton value={step.cliCommand} label="Copy" />
                    </div>
                  )}

                  {/* "I installed it" override only for cli_installed step (EC-14) */}
                  {!done && step.key === 'cli_installed' && (
                    <div className="mt-2">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 px-2 text-label"
                        onClick={() => {
                          markStepDone('cli_installed');
                          reportEvent('onboarding_step_completed', { step: 'cli_installed' });
                        }}
                      >
                        I installed it
                      </Button>
                    </div>
                  )}
                </div>
              </div>
            </div>
          );
        })}
      </div>

      {/* Footer hint */}
      <div className="px-4 py-2.5 border-t border-border/40 text-label text-muted-foreground bg-muted/10">
        Click the rocket badge anytime to reopen this panel.
      </div>
    </div>
  );
}

/** Circular progress ring for the collapsed badge. SVG-based, lightweight. */
function ProgressRing({ percent, size }: { percent: number; size: number }) {
  const stroke = 3;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference - (percent / 100) * circumference;
  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      className="absolute inset-0"
      aria-hidden="true"
    >
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        className="text-primary-foreground/20"
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        strokeLinecap="round"
        className="text-primary-foreground transition-all duration-500"
        style={{ transform: 'rotate(-90deg)', transformOrigin: 'center' }}
      />
    </svg>
  );
}
