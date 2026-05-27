import { Check, X } from 'lucide-react';
import { Button, CopyButton } from '@loopzero/ui';
import { useOnboardingState } from '@/hooks/use-onboarding-state';
import { reportEvent } from '@/lib/rum';

/**
 * OnboardingChecklist (Sprint 3 T3.2) — persistent dismissable widget
 * mounted at sidebar bottom. Renders ONLY if state.isVisible is true.
 *
 * Per ADR D2 (2026 FAANG synthesis): Linear-style checklist + JTBD framing
 * ("Get your app live"). Each step shows CLI command + copy-to-clipboard
 * matching LoopZero's CLI-first ethos.
 *
 * EC-17: copy uses navigator.clipboard with fallback to document.execCommand
 * for HTTP contexts. @loopzero/ui CopyButton handles this internally.
 */

interface ChecklistStep {
  key: 'cli_installed' | 'logged_in' | 'project_initialized' | 'deploy_succeeded' | 'domain_added';
  title: string;
  cliCommand?: string;
}

const STEPS: ChecklistStep[] = [
  { key: 'cli_installed', title: 'Install the LoopZero CLI', cliCommand: 'npm install -g @loopzero/cli' },
  { key: 'logged_in', title: 'Sign in', cliCommand: 'loopzero login' },
  { key: 'project_initialized', title: 'Initialize your first project', cliCommand: 'loopzero init' },
  { key: 'deploy_succeeded', title: 'Deploy your first project', cliCommand: 'loopzero deploy' },
  { key: 'domain_added', title: 'Add a custom domain (optional)', cliCommand: 'loopzero domain add app.example.com' },
];

export function OnboardingChecklist() {
  const { state, completed, total, percent, dismiss, markStepDone, isVisible } =
    useOnboardingState();

  if (!isVisible) return null;

  // Active step = first incomplete step
  const activeKey = STEPS.find((s) => !state.steps[s.key])?.key;

  return (
    <div className="border-t border-border/40 px-3 py-3 bg-muted/10">
      <div className="flex items-start justify-between gap-2 mb-2">
        <div className="min-w-0 flex-1">
          <p className="text-body-sm font-medium text-foreground">Get your app live</p>
          <div className="mt-1.5 flex items-center gap-2">
            <div className="flex-1 h-1.5 rounded-full bg-muted overflow-hidden">
              <div
                className="h-full bg-primary transition-all"
                style={{ width: `${percent}%` }}
                aria-label={`Onboarding progress ${percent}%`}
              />
            </div>
            <span className="text-label text-muted-foreground tabular-nums">
              {completed}/{total}
            </span>
          </div>
        </div>
        <button
          type="button"
          onClick={() => {
            dismiss();
            reportEvent('onboarding_dismissed');
          }}
          className="text-muted-foreground hover:text-foreground shrink-0"
          aria-label="Dismiss onboarding"
        >
          <X className="size-4" aria-hidden="true" />
        </button>
      </div>

      <ul className="space-y-1.5 mt-3">
        {STEPS.map((step) => {
          const done = state.steps[step.key];
          const isActive = step.key === activeKey;
          return (
            <li key={step.key} className="text-label">
              <div className="flex items-center gap-2">
                {done ? (
                  <Check className="size-3.5 text-success shrink-0" aria-label="done" />
                ) : (
                  <div className="size-3.5 rounded-full border border-border shrink-0" aria-label="pending" />
                )}
                <span
                  className={
                    done
                      ? 'text-muted-foreground line-through'
                      : isActive
                        ? 'text-foreground font-medium'
                        : 'text-muted-foreground'
                  }
                >
                  {step.title}
                </span>
              </div>
              {isActive && step.cliCommand ? (
                <div className="mt-1.5 ml-5.5 flex items-center gap-1 bg-muted/40 rounded px-2 py-1 font-mono text-[11px]">
                  <span className="text-muted-foreground">$</span>
                  <span className="flex-1 truncate text-foreground">{step.cliCommand}</span>
                  <CopyButton value={step.cliCommand} label="Copy" />
                </div>
              ) : null}
              {isActive && step.key === 'cli_installed' ? (
                <div className="mt-1 ml-5.5">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 px-2 text-label"
                    onClick={() => {
                      markStepDone('cli_installed');
                      reportEvent('onboarding_step_completed', { step: 'cli_installed' });
                    }}
                  >
                    I installed it
                  </Button>
                </div>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
