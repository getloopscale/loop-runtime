import { useCallback, useState } from 'react';
import { useEngineApi } from '@/hooks/use-api';
import { useDeployments } from '@/hooks/use-deployments';
import { useProjects } from '@/hooks/use-projects';
import { useWhoami } from '@/hooks/use-whoami';

/**
 * useOnboardingState (Sprint 3 T3.1) — first-run UX state machine.
 *
 * Per ADR D2 (2026 FAANG synthesis): persistent dismissable checklist combined
 * com empty-state-as-onboarding + first-deploy celebration. State persists in
 * localStorage (versioned key). Auto-derived steps re-evaluate from API on every
 * render; user-confirmed step (cli_installed) requires explicit markStepDone.
 *
 * EC-04 fix: safeGet/safeSet wrap all localStorage ops — Safari Private mode,
 * QuotaExceeded, and corrupted JSON fall back to in-memory state. App MUST NOT
 * crash if localStorage is unavailable.
 *
 * EC-06 fix: replay() resets DEFAULT_STATE so widget reappears even when user
 * had completed all 5 steps. Auto-derived steps re-derive from API rapidly.
 *
 * EC-14 fix: cli_installed is NOT auto-derived from session existence. User
 * could log in via browser flow without ever installing CLI. Requires explicit
 * markStepDone('cli_installed') from user click ("I installed it" CTA).
 */

export const ONBOARDING_KEY = 'loopzero-onboarding-v1';

interface OnboardingSteps {
  cli_installed: boolean;       // EC-14: user-confirmed only
  logged_in: boolean;           // auto
  project_initialized: boolean; // auto
  deploy_succeeded: boolean;    // auto
  domain_added: boolean;        // auto
}

export interface OnboardingState {
  dismissed: boolean;
  steps: OnboardingSteps;
  completedAt?: string;
}

const DEFAULT_STATE: OnboardingState = {
  dismissed: false,
  steps: {
    cli_installed: false,
    logged_in: false,
    project_initialized: false,
    deploy_succeeded: false,
    domain_added: false,
  },
};

// EC-04: localStorage MAY throw or be corrupted — wrap all ops.
function safeGet(): OnboardingState {
  try {
    if (typeof localStorage === 'undefined') return DEFAULT_STATE;
    const raw = localStorage.getItem(ONBOARDING_KEY);
    if (!raw) return DEFAULT_STATE;
    const parsed = JSON.parse(raw) as Partial<OnboardingState>;
    return {
      dismissed: !!parsed.dismissed,
      steps: { ...DEFAULT_STATE.steps, ...(parsed.steps ?? {}) },
      completedAt: parsed.completedAt,
    };
  } catch {
    // Safari Private, QuotaExceeded, malformed JSON — fall back to default.
    return DEFAULT_STATE;
  }
}

function safeSet(state: OnboardingState): void {
  try {
    if (typeof localStorage === 'undefined') return;
    localStorage.setItem(ONBOARDING_KEY, JSON.stringify(state));
  } catch {
    // Quota exceeded or storage disabled — in-memory only is acceptable
    // fallback; user can still navigate, just won't survive reload.
  }
}

export function useOnboardingState() {
  const [persistedState, setPersistedState] = useState<OnboardingState>(() => safeGet());

  const { whoami } = useWhoami();
  const { projects } = useProjects();
  const { data: domainsData } = useEngineApi<{ domains: unknown[] }>('/api/v1/domains');
  const { builds } = useDeployments(projects[0]?.name);

  // Derive auto-tracked steps from API state. User-confirmed step
  // (cli_installed) reads from persisted localStorage only (EC-14).
  const steps: OnboardingSteps = {
    cli_installed: persistedState.steps.cli_installed,
    logged_in: !!whoami,
    project_initialized: projects.length > 0,
    deploy_succeeded: builds.some((b) => b.status === 'succeeded'),
    domain_added: (domainsData?.domains?.length ?? 0) > 0,
  };

  const state: OnboardingState = {
    dismissed: persistedState.dismissed,
    steps,
    completedAt: persistedState.completedAt,
  };

  const completed = Object.values(steps).filter(Boolean).length;
  const total = 5;
  const percent = Math.round((completed / total) * 100);

  const dismiss = useCallback(() => {
    const next: OnboardingState = { ...state, dismissed: true };
    setPersistedState(next);
    safeSet(next);
  }, [state]);

  // EC-06: replay resets DEFAULT_STATE (not just dismissed flag). Without
  // this, isVisible permanece false quando user havia completado tudo.
  const replay = useCallback(() => {
    setPersistedState(DEFAULT_STATE);
    safeSet(DEFAULT_STATE);
  }, []);

  // EC-14: cli_installed (user-confirmed) marked via this method only
  const markStepDone = useCallback((step: keyof OnboardingSteps) => {
    setPersistedState((prev) => {
      const next = { ...prev, steps: { ...prev.steps, [step]: true } };
      safeSet(next);
      return next;
    });
  }, []);

  const isVisible = !state.dismissed && completed < total;

  return {
    state,
    completed,
    total,
    percent,
    dismiss,
    replay,
    markStepDone,
    isVisible,
  };
}
