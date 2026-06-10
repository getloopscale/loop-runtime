import { useEffect, useRef } from 'react';
import confetti from 'canvas-confetti';
import { useDeployments } from '@/hooks/use-deployments';
import { reportEvent } from '@/lib/rum';

/**
 * useFirstDeployTrigger (Sprint 3 T3.4) — fires celebration ONCE per project
 * when first build transitions to succeeded DURING current session.
 *
 * EC-05 fix: mountedAt timestamp filter prevents celebration on HISTORICAL
 * builds. User visiting deployment-detail.tsx of an old deploy (after
 * clearing localStorage) would otherwise see confetti for a 6-month-old
 * deploy — celebration fatigue + loss of real first-deploy emotional moment.
 *
 * Real-time filter: only builds with finished_at > mountedAt qualify.
 *
 * localStorage flag (loopzero-first-deploy-{projectName}-celebrated) prevents
 * re-firing across tabs/sessions. prefers-reduced-motion respected.
 */

interface Toast {
  show: (message: string) => void;
}

interface Options {
  projectName: string | undefined;
  toast?: Toast;
  liveUrl?: string;
}

export function useFirstDeployTrigger({ projectName, toast, liveUrl }: Options): void {
  const mountedAt = useRef<number>(Date.now());
  const { builds } = useDeployments(projectName);

  useEffect(() => {
    if (!projectName) return;
    const flagKey = `loopzero-first-deploy-${projectName}-celebrated`;

    // Safety: prevent re-firing
    try {
      if (localStorage.getItem(flagKey)) return;
    } catch {
      // localStorage disabled — degrade gracefully (might re-fire in
      // edge case, but not a security issue)
    }

    // EC-05: only real-time transitions count
    const realtimeSucceeded = builds.find(
      (b) =>
        b.status === 'succeeded' &&
        b.finished_at &&
        new Date(b.finished_at).getTime() > mountedAt.current,
    );
    if (!realtimeSucceeded) return;

    // Set flag immediately to prevent double-fire
    try {
      localStorage.setItem(flagKey, '1');
    } catch {
      // OK
    }

    // Respect reduced motion
    const reduceMotion =
      typeof window !== 'undefined' &&
      window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;

    if (!reduceMotion) {
      try {
        confetti({
          particleCount: 50,
          spread: 70,
          origin: { y: 0.7 },
        });
      } catch {
        // confetti error — non-fatal, toast still fires
      }
    }

    toast?.show(`Your app is live${liveUrl ? ` at ${liveUrl}` : ''} 🎉`);
    reportEvent('first_deploy_celebrated', {
      project: projectName,
      build_id: realtimeSucceeded.build_id,
    });
  }, [projectName, builds, toast, liveUrl]);
}
