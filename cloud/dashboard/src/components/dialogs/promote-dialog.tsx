import { useEffect, useRef, useState } from 'react';
import { ArrowRight, Loader2, Plus, Rocket } from 'lucide-react';
import { Alert, Button, Dialog, Label } from '@loopzero/ui';
import { usePromoteEnvironment } from '@/hooks/mutations/use-environments';

interface EnvironmentLike {
  name: string;
  status?: string;
  // M2 v1.2 — per-(project, env) tuple field. Empty / undefined = legacy
  // production entry treated as belonging to every project (backward
  // compat ADR D2). Plan 2026-06-08-dashboard-screens-review-fixes T4.4
  // (EC-4 absorption) uses this for the cross-project promote toggle.
  project?: string;
}

interface PromoteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectName: string;
  /** All known environments for source/target dropdowns. */
  environments: EnvironmentLike[];
  /** Optional pre-selected source (e.g. opened from a row's "Promote" button). */
  initialFrom?: string;
  /** Callback to open the create-env sheet for "+ Create new..." sentinel. */
  onCreateRequested?: () => void;
  onPromoted?: () => void;
}

/**
 * PromoteDialog — Plan J-10 v1.1 T2.3 (refactored from buggy v1.0).
 *
 * v1.0 bugs fixed:
 *   - Body used `from_environment + to_environment` (backend expects `from + to`).
 *   - Free-text inputs with no dropdown — easy to mis-spell env name.
 *   - No create-on-the-fly when target env missing (per blueprint § 4.7).
 *   - No usePromoteEnvironment hook (direct engineFetch — no cache invalidate).
 *
 * v1.1 design (per blueprint § 2.1 Railway pattern + § 4.7):
 *   - Source dropdown lists envs with status=active (filtered).
 *   - Target dropdown lists envs (excluding source) + "+ Create new..." sentinel.
 *   - Sentinel triggers `onCreateRequested` to open CreateEnvironmentSheet; user
 *     returns to this dialog with the new env auto-selected via the
 *     `environments` prop re-render.
 *   - Uses usePromoteEnvironment hook (proper queryKey invalidate).
 *   - 409 typed errors surface as product-language banners.
 *   - Source == target validated client-side (EC-4 — don't even POST).
 */
export function PromoteDialog({
  open,
  onOpenChange,
  projectName,
  environments,
  initialFrom,
  onCreateRequested,
  onPromoted,
}: PromoteDialogProps) {
  const CREATE_NEW = '__create_new__';
  const [fromEnv, setFromEnv] = useState<string>(initialFrom ?? '');
  const [toEnv, setToEnv] = useState<string>('');
  const [error, setError] = useState<string | null>(null);
  // T4.4 finding 4.6 + EC-4 absorption: default the dialog to same-
  // project envs only. Cross-project promote (project rename / migration
  // use-case per ADR D6) requires explicit opt-in via the checkbox
  // below, which expands the dropdown set to ALL tenant envs and
  // renders a warning Alert when source.project !== target.project.
  const [includeOtherProjects, setIncludeOtherProjects] = useState(false);
  // Ref pointing at the Cancel button — pattern Vercel/Stripe destructive default focus.
  // Promote is mutating but not destructive in itself; even so, defaulting focus to
  // Cancel matches the broader a11y pattern users expect.
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const promote = usePromoteEnvironment(projectName);

  // When environments array changes (e.g. after create-on-the-fly), auto-select
  // the most recently added one as target if user was mid-flow.
  useEffect(() => {
    if (!toEnv && environments.length > 0) {
      const candidates = environments.filter((e) => e.name !== fromEnv);
      if (candidates.length > 0) {
        // Prefer the LAST env (most recently created), heuristic for "user just
        // came back from create flow".
        setToEnv(candidates[candidates.length - 1].name);
      }
    }
  }, [environments, fromEnv, toEnv]);

  // Default source to first active env when none preselected.
  useEffect(() => {
    if (!fromEnv && environments.length > 0) {
      const active = environments.find(
        (e) => (e.status ?? 'active').toLowerCase() === 'active',
      );
      if (active) setFromEnv(active.name);
    }
  }, [environments, fromEnv]);

  // Reset error on prop changes / re-open.
  useEffect(() => {
    if (open) setError(null);
  }, [open]);

  const handleTargetChange = (value: string) => {
    if (value === CREATE_NEW) {
      onCreateRequested?.();
      // Don't change toEnv — keep user's flow intact; new env will appear on next render.
      return;
    }
    setToEnv(value);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!fromEnv.trim() || !toEnv.trim()) {
      setError('Both source and target environments are required.');
      return;
    }
    if (fromEnv === toEnv) {
      setError('Source and target must be different environments.');
      return;
    }
    try {
      const res = await promote.mutateAsync({ from: fromEnv, to: toEnv });
      if (!res) {
        // Backend returned non-OK; engineFetch swallowed details. The 402
        // interceptor (lib/api.ts T1.4) emits loopzero:plan-limit event globally
        // if it was a plan limit. For 409/404, present product-language copy.
        setError(
          `Promote failed. The current release in ${fromEnv} remains active. ` +
            `It may be a deploy already in progress on ${toEnv} — try again in a moment.`,
        );
        return;
      }
      onPromoted?.();
      onOpenChange(false);
    } catch (err) {
      setError(
        err instanceof Error
          ? `Promote failed: ${err.message}.`
          : 'Promote failed. The current release remains active.',
      );
    }
  };

  // T4.4 finding 4.6 + EC-4 absorption: filter dropdown set by project.
  // Empty `project` field = legacy production entry — surfaced under
  // every project per ADR D2 backward compat. When the "Include envs
  // from other projects" toggle is ON, the filter passes through.
  function inProjectScope(e: EnvironmentLike): boolean {
    if (includeOtherProjects) return true;
    return !e.project || e.project === projectName;
  }

  // Build dropdown options. ADR D6: cross-project promote is allowed
  // but explicit — default scope is same-project-only.
  const sourceOptions = environments.filter(
    (e) => (e.status ?? 'active').toLowerCase() === 'active' && inProjectScope(e),
  );
  const targetOptions = environments.filter(
    (e) => e.name !== fromEnv && inProjectScope(e),
  );

  // Cross-project warning surfaces when source.project and target.project
  // are BOTH non-empty AND differ. Legacy empty-project envs do not
  // trigger the warning (ADR D2 — they implicitly belong to current).
  const fromEnvObj = environments.find((e) => e.name === fromEnv);
  const toEnvObj = environments.find((e) => e.name === toEnv);
  const isCrossProject = Boolean(
    fromEnvObj?.project &&
      toEnvObj?.project &&
      fromEnvObj.project !== toEnvObj.project,
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title className="inline-flex items-center gap-2">
            <Rocket className="size-5 text-primary" aria-hidden="true" />
            Promote release
          </Dialog.Title>
          <Dialog.Description>
            Copy the active release of <span className="font-mono">{fromEnv || '...'}</span>{' '}
            into <span className="font-mono">{toEnv || '...'}</span>. The source release
            stays active until the target deploy completes.
          </Dialog.Description>
        </Dialog.Header>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="flex items-end gap-2">
            <div className="flex-1">
              <Label htmlFor="promote-from">From</Label>
              <select
                id="promote-from"
                value={fromEnv}
                onChange={(e) => {
                  const next = e.target.value;
                  setFromEnv(next);
                  // EC-14 (2026-06-06 LIVE catch): when the user picks a
                  // source that collides with the auto-selected target,
                  // the submit button locks (from===to → disabled). Auto-
                  // re-pick a different target so the dialog stays usable
                  // without forcing the user to know they also need to
                  // touch the target dropdown.
                  if (next === toEnv) {
                    const remaining = environments.filter(
                      (env) => env.name !== next,
                    );
                    setToEnv(
                      remaining.length > 0
                        ? remaining[remaining.length - 1].name
                        : '',
                    );
                  }
                }}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-body-sm focus:outline-none focus:ring-2 focus:ring-ring"
                required
                aria-describedby="promote-from-help"
              >
                <option value="" disabled>
                  Select source env
                </option>
                {sourceOptions.map((env) => (
                  <option key={env.name} value={env.name}>
                    {env.name}
                  </option>
                ))}
              </select>
              <span id="promote-from-help" className="sr-only">
                Choose the environment whose active release will be cloned.
              </span>
            </div>
            <ArrowRight
              className="mb-2 size-5 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
            <div className="flex-1">
              <Label htmlFor="promote-to">To</Label>
              <select
                id="promote-to"
                value={toEnv}
                onChange={(e) => handleTargetChange(e.target.value)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-body-sm focus:outline-none focus:ring-2 focus:ring-ring"
                required
                aria-describedby="promote-to-help"
              >
                <option value="" disabled>
                  Select target env
                </option>
                {targetOptions.map((env) => (
                  <option key={env.name} value={env.name}>
                    {env.name}
                  </option>
                ))}
                {onCreateRequested && (
                  <option value={CREATE_NEW}>+ Create new environment…</option>
                )}
              </select>
              <span id="promote-to-help" className="sr-only">
                Choose the environment that will receive the promoted release.
              </span>
            </div>
          </div>

          <Alert
            intent="info"
            description="The promote re-deploys the source commit in the target env with target-scoped secrets. No image rebuild."
          />

          {/* T4.4 finding 4.6 + EC-4 + ADR D6: cross-project promote
              toggle. Default-restricted to same-project envs; toggle
              expands to all tenant envs (used for project rename
              migration use-cases). */}
          <div className="flex items-start gap-2">
            <input
              id="promote-cross-project"
              data-testid="promote-cross-project"
              type="checkbox"
              checked={includeOtherProjects}
              onChange={(e) => setIncludeOtherProjects(e.target.checked)}
              className="mt-0.5 size-4 rounded border-input"
            />
            {/* Label phrasing deliberately avoids the substrings
                "From"/"To" because the existing test suite uses
                /From/i and /To/i regexes for the source+target select
                labels. */}
            <Label
              htmlFor="promote-cross-project"
              className="cursor-pointer text-body-sm font-normal"
            >
              Include other projects
            </Label>
          </div>

          {/* T4.4 EC-4: warning surfaces when source + target belong to
              different projects AND the user has explicitly opted in. */}
          {isCrossProject && (
            <Alert
              intent="warning"
              description={`Promoting across projects rewrites the target project's deployment. Source: ${fromEnv} (${fromEnvObj?.project}) → Target: ${toEnv} (${toEnvObj?.project}).`}
            />
          )}

          {error && <Alert intent="destructive" description={error} />}

          <Dialog.Footer>
            <Button
              ref={cancelRef}
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={promote.isPending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={promote.isPending || !fromEnv || !toEnv || fromEnv === toEnv}
            >
              {promote.isPending && (
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
              )}
              {promote.isPending ? 'Promoting…' : `Promote ${fromEnv || '...'} → ${toEnv || '...'}`}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}

// Re-export Plus icon for any caller building "Create" buttons next to this dialog.
export { Plus };
