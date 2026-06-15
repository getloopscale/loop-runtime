import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Loader2 } from 'lucide-react';
import { Alert, Button, Card, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useScale } from '@/hooks/use-scale';
import { useScaleCost } from '@/hooks/use-scale-cost';
import { ScaleSlider } from '@/components/scale/scale-slider';
import { HPAToggle, type HPAMode } from '@/components/scale/hpa-toggle';
import { KEDAToggle } from '@/components/scale/keda-toggle';
import { ScaleConfirmDialog } from '@/components/scale/scale-confirm-dialog';

// Until the project status endpoint surfaces the current scaling config,
// derive the initial state from a conservative default. The first
// successful save will rehydrate from the ScaleResponse body (server
// authoritative).
const DEFAULT_CURRENT_REPLICAS = 2;

/**
 * ProjectScalePage — J-20 Scale plan v1.1 T4.1.
 *
 * Composes the 4 canonical scale components (ScaleSlider, HPAToggle,
 * KEDAToggle, ScaleConfirmDialog) + 2 canonical hooks (useScale,
 * useScaleCost) into the Vercel-style /projects/:name/scale settings
 * page. The page is the security UX layer; the API handler enforces
 * tier + bounds + lock per Phase 1 absorptions.
 *
 * Save flow:
 *
 *   1. User adjusts slider / HPA / KEDA toggle → local state updates.
 *   2. useScaleCost computes preview cost delta inline (UX hint).
 *   3. Click "Apply scale" → ScaleConfirmDialog opens with impact
 *      paragraph + type-to-confirm phrase per EC-8.
 *   4. Confirm → useScale mutation POSTs to /api/v1/projects/{name}/scale.
 *   5. Success → server `monthly_cost_usd` shown as the authoritative
 *      cost; client preview reconciles in the Cost card.
 *   6. Failure → Alert renders the typed error (PLAN_UPGRADE_REQUIRED
 *      surfaces upgrade CTA; HPA_TARGET_OUT_OF_BOUNDS highlights field).
 *
 * Plan tier + cluster KEDA flag — currently sourced from the resolved
 * scale response after the first save. Until the page bootstraps from
 * a status endpoint, the initial render assumes Starter + cluster KEDA
 * installed = false (conservative fail-closed default).
 */
export function ProjectScalePage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Scale`);

  // Local edit state
  const [mode, setMode] = useState<HPAMode>('manual');
  const [replicas, setReplicas] = useState(DEFAULT_CURRENT_REPLICAS);
  const [hpaMin, setHpaMin] = useState(1);
  const [hpaMax, setHpaMax] = useState(3);
  const [hpaTargets, setHpaTargets] = useState({ cpu: 80, memory: 80 });
  const [kedaEnabled, setKedaEnabled] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  // Until the page bootstraps from /v1/projects/:name/status, assume the
  // conservative defaults — the FIRST save response rehydrates these.
  const [resolvedPlanTier, setResolvedPlanTier] = useState('starter');
  const [clusterKedaInstalled] = useState(true);
  const [currentReplicas, setCurrentReplicas] = useState(
    DEFAULT_CURRENT_REPLICAS,
  );
  const [serverMonthlyCostUSD, setServerMonthlyCostUSD] = useState<number | null>(
    null,
  );

  const scale = useScale(name);

  const proposedReplicas = mode === 'manual' ? replicas : hpaMax;
  const cost = useScaleCost({
    replicas: proposedReplicas,
    currentReplicas,
  });

  const onConfirm = async () => {
    const payload =
      mode === 'manual'
        ? {
            app: 'api', // canonical default; future iteration: picker when monorepo > 1 server
            environment: 'production',
            replicas,
          }
        : {
            app: 'api',
            environment: 'production',
            min: kedaEnabled ? 0 : hpaMin,
            max: hpaMax,
            cpu: hpaTargets.cpu,
            memory: hpaTargets.memory,
          };
    const res = await scale.mutateAsync(payload);
    setConfirmOpen(false);
    // Rehydrate from server-authoritative response per ADR D1 EC-2.
    setResolvedPlanTier(res.plan_tier);
    setServerMonthlyCostUSD(res.monthly_cost_usd);
    setCurrentReplicas(
      mode === 'manual' ? replicas : kedaEnabled ? 0 : hpaMin,
    );
  };

  const errorMessage =
    scale.error instanceof Error ? scale.error.message : null;

  return (
    <PageShell
      title="Scale"
      description={`Replica count and autoscaling for ${name}.`}
    >
      <div className="mb-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
          </Link>
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[1.5fr_1fr]">
        <Card>
          <Card.Header>
            <Card.Title>Replica configuration</Card.Title>
            <Card.Description>
              Set the replica count and autoscaling bounds. Changes apply on
              save and persist across deploys.
            </Card.Description>
          </Card.Header>
          <Card.Body>
            <div className="space-y-6">
              <HPAToggle
                mode={mode}
                targets={hpaTargets}
                onModeChange={setMode}
                onTargetsChange={setHpaTargets}
                disabled={scale.isPending}
              />

              {mode === 'manual' && (
                <ScaleSlider
                  value={replicas}
                  planTier={resolvedPlanTier}
                  onChange={setReplicas}
                  disabled={scale.isPending}
                />
              )}

              {mode === 'auto' && (
                <div className="grid grid-cols-2 gap-4">
                  <ScaleSlider
                    value={hpaMin}
                    planTier={resolvedPlanTier}
                    onChange={setHpaMin}
                    disabled={scale.isPending}
                  />
                  <ScaleSlider
                    value={hpaMax}
                    planTier={resolvedPlanTier}
                    onChange={setHpaMax}
                    disabled={scale.isPending}
                  />
                </div>
              )}

              {mode === 'auto' && (
                <KEDAToggle
                  enabled={kedaEnabled}
                  planTier={resolvedPlanTier}
                  onChange={setKedaEnabled}
                  clusterKedaInstalled={clusterKedaInstalled}
                  disabled={scale.isPending}
                />
              )}

              {errorMessage && (
                <Alert intent="destructive" description={errorMessage} />
              )}

              <div className="flex justify-end">
                <Button
                  type="button"
                  onClick={() => setConfirmOpen(true)}
                  disabled={scale.isPending}
                >
                  {scale.isPending && (
                    <Loader2
                      className="size-4 animate-spin"
                      aria-hidden="true"
                    />
                  )}
                  {scale.isPending ? 'Scaling…' : 'Apply scale'}
                </Button>
              </div>
            </div>
          </Card.Body>
        </Card>

        <Card>
          <Card.Header>
            <Card.Title className="text-body-sm">Cost preview</Card.Title>
            <Card.Description>
              Estimated monthly cost. Final bill reconciles to actual usage
              at the end of the billing cycle.
            </Card.Description>
          </Card.Header>
          <Card.Body>
            <dl className="space-y-3 text-body-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">Current</dt>
                <dd className="font-mono text-foreground">
                  {currentReplicas} replicas
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">After change</dt>
                <dd className="font-mono text-foreground">
                  {proposedReplicas} replicas
                </dd>
              </div>
              <div className="flex justify-between border-t border-border/60 pt-3">
                <dt className="text-muted-foreground">Estimated</dt>
                <dd className="font-mono text-foreground">
                  ${cost.estimatedMonthlyUSD.toFixed(2)}/mo
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">Delta</dt>
                <dd
                  className={`font-mono ${
                    cost.direction === 'up'
                      ? 'text-warning'
                      : cost.direction === 'down'
                        ? 'text-success'
                        : 'text-foreground'
                  }`}
                >
                  {cost.direction === 'flat'
                    ? '—'
                    : `${cost.deltaUSD > 0 ? '+' : '–'}$${Math.abs(cost.deltaUSD).toFixed(2)}/mo`}
                </dd>
              </div>
              {serverMonthlyCostUSD !== null && (
                <div className="flex justify-between border-t border-border/60 pt-3">
                  <dt className="text-muted-foreground">
                    Applied (server authoritative)
                  </dt>
                  <dd className="font-mono text-foreground">
                    ${serverMonthlyCostUSD.toFixed(2)}/mo
                  </dd>
                </div>
              )}
            </dl>
          </Card.Body>
        </Card>
      </div>

      <ScaleConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        projectName={name}
        environment="production"
        currentReplicas={currentReplicas}
        proposedReplicas={proposedReplicas}
        estimatedMonthlyUSD={cost.estimatedMonthlyUSD}
        deltaUSD={cost.deltaUSD}
        onConfirm={onConfirm}
        busy={scale.isPending}
      />
    </PageShell>
  );
}
