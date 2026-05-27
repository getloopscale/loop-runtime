import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Loader2 } from 'lucide-react';
import {
  Alert,
  Button,
  Card,
  Input,
  Label,
  PageShell,
} from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { engineFetch } from '@/lib/api';
import type { ScaleResponse } from '@/lib/api/types';

const DEFAULT_CURRENT = 2;
const DEFAULT_MAX_FROM_PLAN = 5;
const COST_PER_REPLICA_MONTHLY_USD = 5;

/**
 * ProjectScalePage — Plan v1.0 T2.3 (refactored from dialog to page,
 * 2026-05-26 sponsor directive "FAANG-level UX").
 *
 * Why page > dialog: deep-linkable (`/projects/X/scale`), browser-back
 * works, fits the sidebar drill-down nav pattern (sidebar item navigates
 * here), more room for cost preview + HPA bounds + future scale history.
 *
 * Vercel/Linear follow this convention — scaling is a settings-style page,
 * not a transient modal.
 */
export function ProjectScalePage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Scale`);

  const [replicas, setReplicas] = useState(DEFAULT_CURRENT);
  const [min, setMin] = useState(DEFAULT_CURRENT);
  const [max, setMax] = useState(DEFAULT_CURRENT);
  const [targetCpu, setTargetCpu] = useState(70);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<ScaleResponse | null>(null);

  const exceedsPlan = replicas > DEFAULT_MAX_FROM_PLAN;
  const replicaDelta = replicas - DEFAULT_CURRENT;
  const monthlyCostDelta = replicaDelta * COST_PER_REPLICA_MONTHLY_USD;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (exceedsPlan) {
      setError(`Your plan allows up to ${DEFAULT_MAX_FROM_PLAN} replicas.`);
      return;
    }
    setError(null);
    setSuccess(null);
    setSubmitting(true);
    try {
      const res = await engineFetch<ScaleResponse>(
        `/api/v1/projects/${encodeURIComponent(name)}/scale`,
        {
          method: 'POST',
          body: JSON.stringify({ replicas, min, max, target_cpu: targetCpu }),
        },
      );
      if (res === null) {
        setError("Couldn't reach the scale endpoint. Try again in a moment.");
        return;
      }
      setSuccess(res);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong.');
    } finally {
      setSubmitting(false);
    }
  };

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
              Set the target replica count and autoscaling bounds for this
              project. Changes apply immediately on submit.
            </Card.Description>
          </Card.Header>
          <Card.Body>
            <form onSubmit={handleSubmit} className="space-y-5">
              <div>
                <Label htmlFor="replicas">Target replicas</Label>
                <Input
                  id="replicas"
                  type="number"
                  min={0}
                  max={DEFAULT_MAX_FROM_PLAN}
                  value={replicas}
                  onChange={(e) => setReplicas(Number(e.target.value))}
                />
                <p className="mt-1 text-label text-muted-foreground">
                  Current: {DEFAULT_CURRENT} · Plan cap: {DEFAULT_MAX_FROM_PLAN}
                </p>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <Label htmlFor="min-replicas">HPA min</Label>
                  <Input
                    id="min-replicas"
                    type="number"
                    min={0}
                    value={min}
                    onChange={(e) => setMin(Number(e.target.value))}
                  />
                </div>
                <div>
                  <Label htmlFor="max-replicas">HPA max</Label>
                  <Input
                    id="max-replicas"
                    type="number"
                    min={0}
                    max={DEFAULT_MAX_FROM_PLAN}
                    value={max}
                    onChange={(e) => setMax(Number(e.target.value))}
                  />
                </div>
              </div>

              <div>
                <Label htmlFor="target-cpu">Target CPU %</Label>
                <Input
                  id="target-cpu"
                  type="number"
                  min={1}
                  max={100}
                  value={targetCpu}
                  onChange={(e) => setTargetCpu(Number(e.target.value))}
                />
                <p className="mt-1 text-label text-muted-foreground">
                  Autoscaler scales up when avg CPU exceeds this threshold.
                </p>
              </div>

              {error && <Alert intent="destructive" description={error} />}
              {success && (
                <Alert
                  intent="success"
                  description={`Scaled ${name} to ${success.applied.replicas} replicas.`}
                />
              )}

              <div className="flex justify-end">
                <Button
                  type="submit"
                  disabled={submitting || replicaDelta === 0 || exceedsPlan}
                >
                  {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
                  {submitting ? 'Scaling…' : 'Apply scale'}
                </Button>
              </div>
            </form>
          </Card.Body>
        </Card>

        <Card>
          <Card.Header>
            <Card.Title className="text-body-sm">Cost preview</Card.Title>
            <Card.Description>
              Approximate monthly impact. Real billing depends on plan tier +
              actual usage.
            </Card.Description>
          </Card.Header>
          <Card.Body>
            <dl className="space-y-3 text-body-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">Current</dt>
                <dd className="font-mono text-foreground">
                  {DEFAULT_CURRENT} replicas
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">After change</dt>
                <dd className="font-mono text-foreground">{replicas} replicas</dd>
              </div>
              <div className="flex justify-between border-t border-border/60 pt-3">
                <dt className="text-muted-foreground">Monthly delta</dt>
                <dd
                  className={`font-mono ${
                    monthlyCostDelta > 0
                      ? 'text-warning'
                      : monthlyCostDelta < 0
                        ? 'text-success'
                        : 'text-foreground'
                  }`}
                >
                  {replicaDelta === 0
                    ? '—'
                    : `${monthlyCostDelta > 0 ? '+' : ''}$${monthlyCostDelta.toFixed(2)}/mo`}
                </dd>
              </div>
            </dl>
          </Card.Body>
        </Card>
      </div>
    </PageShell>
  );
}
