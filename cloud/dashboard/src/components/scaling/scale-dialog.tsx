import { useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Dialog, Input, Label } from '@loopzerokit/ui';
import { engineFetch } from '@/lib/api';
import type { ScaleResponse } from '@/lib/api/types';

interface ScaleDialogProps {
  projectName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentReplicas: number;
  maxReplicasFromPlan: number;
  /** Mock per-replica monthly cost in USD; replace with real billing rate when backend ships it. */
  costPerReplicaMonthlyUsd?: number;
  onScaled?: (response: ScaleResponse) => void;
}

/**
 * ScaleDialog — Plan v1.0 T2.3.
 *
 * Replicas input + HPA min/max + cost preview ("scaling 2→5 will cost +$15/month").
 * Validates against plan cap from useBilling (passed in via maxReplicasFromPlan).
 * Submit POST /api/v1/projects/{name}/scale.
 *
 * Honest UI: cost preview is computed client-side from a flat per-replica rate;
 * real billing computes cost via plan tier + actual usage. When backend exposes
 * /v1/billing/scale-preview, swap to real call. For now the mock preview teaches
 * users that scaling has cost — even rough is better than hiding.
 */
export function ScaleDialog({
  projectName,
  open,
  onOpenChange,
  currentReplicas,
  maxReplicasFromPlan,
  costPerReplicaMonthlyUsd = 5,
  onScaled,
}: ScaleDialogProps) {
  const [replicas, setReplicas] = useState(currentReplicas);
  const [min, setMin] = useState(currentReplicas);
  const [max, setMax] = useState(currentReplicas);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setReplicas(currentReplicas);
      setMin(currentReplicas);
      setMax(currentReplicas);
      setError(null);
    }
  }, [open, currentReplicas]);

  const exceedsPlan = replicas > maxReplicasFromPlan;
  const replicaDelta = replicas - currentReplicas;
  const monthlyCostDelta = replicaDelta * costPerReplicaMonthlyUsd;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (exceedsPlan) {
      setError(`Your plan allows up to ${maxReplicasFromPlan} replicas.`);
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      const res = await engineFetch<ScaleResponse>(
        `/api/v1/projects/${encodeURIComponent(projectName)}/scale`,
        {
          method: 'POST',
          body: JSON.stringify({ replicas, min, max }),
        },
      );
      if (res === null) {
        setError("Couldn't reach the scale endpoint. Try again in a moment.");
        return;
      }
      onScaled?.(res);
      onOpenChange(false);
    } catch (e) {
      // T0.1 TCIA — sponsor critério #6
      const msg = e instanceof Error
        ? `Scale failed: ${e.message}. Current replicas remain unchanged. Retry, or contact support if it persists.`
        : 'Scale failed. Current replicas remain unchanged. Retry, or contact support if it persists.';
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Scale {projectName}</Dialog.Title>
          <Dialog.Description>
            Adjust replicas and autoscaling bounds. Cost preview is approximate.
          </Dialog.Description>
        </Dialog.Header>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <Label htmlFor="replicas">Target replicas</Label>
            <Input
              id="replicas"
              type="number"
              min={0}
              max={maxReplicasFromPlan}
              value={replicas}
              onChange={(e) => setReplicas(Number(e.target.value))}
            />
            <p className="mt-1 text-label text-muted-foreground">
              Current: {currentReplicas} · Plan cap: {maxReplicasFromPlan}
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
                max={maxReplicasFromPlan}
                value={max}
                onChange={(e) => setMax(Number(e.target.value))}
              />
            </div>
          </div>

          <div className="rounded-md border border-border/60 bg-muted/30 p-3">
            <p className="text-label uppercase text-muted-foreground">Cost preview</p>
            <p className="mt-1 font-mono text-body-sm text-foreground">
              {replicaDelta === 0
                ? 'No change.'
                : `${replicaDelta > 0 ? '+' : ''}${replicaDelta} replica${Math.abs(replicaDelta) !== 1 ? 's' : ''}: ${monthlyCostDelta > 0 ? '+' : ''}$${monthlyCostDelta.toFixed(2)}/month`}
            </p>
          </div>

          {error && <Alert intent="destructive" description={error} />}

          <Dialog.Footer>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={submitting || replicaDelta === 0 || exceedsPlan}
            >
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Scaling…' : 'Apply scale'}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
