import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, RotateCw, Trash2 } from 'lucide-react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  ConfirmDialog,
  DataTable,
  type DataTableColumn,
  Dialog,
  EmptyState,
  Input,
  PageShell,
  Sheet,
  Timestamp,
  useToast,
} from '@loopzerokit/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { TableSkeleton } from '@/components/data/table-skeleton';
import { cloudFetch } from '@/lib/api';

/**
 * Plan 3 of dashboard-memory-section-roadmap.md, T3.7.
 * Delivery Replay plan, T8.4 — adds per-delivery Replay button to a Sheet
 * listing all 10 recent deliveries per webhook.
 *
 * /memory/webhooks — webhook registration + delivery inspection UI.
 */

interface Webhook {
  id: string;
  url: string;
  label: string;
  events: string[];
  created_at?: string;
}

interface ListResult {
  webhooks: Webhook[];
}

interface Delivery {
  id: string;
  event_type: string;
  status: 'pending' | 'delivering' | 'delivered' | 'failed' | 'dead_letter';
  attempt_count: number;
  last_response_status?: number;
  last_error_message?: string;
  created_at: string;
  completed_at?: string;
}

interface DeliveriesResult {
  deliveries: Delivery[];
}

interface ReplayResult {
  id: string;
  status: 'pending' | 'delivering';
  noop: boolean;
}

function deliveryStatusVariant(s: Delivery['status']): 'default' | 'success' | 'warning' | 'destructive' {
  switch (s) {
    case 'pending':
    case 'delivering':
      return 'warning';
    case 'delivered':
      return 'success';
    case 'failed':
      return 'default';
    case 'dead_letter':
      return 'destructive';
  }
}

// Replay button is visible ONLY for these statuses (D5 of the plan).
// `delivered` would create a duplicate; `pending|delivering` would be a no-op
// the customer doesn't need to trigger manually.
function canReplay(s: Delivery['status']): boolean {
  return s === 'failed' || s === 'dead_letter';
}

const AVAILABLE_EVENTS = [
  { id: 'memory.remembered', label: 'memory.remembered' },
  { id: 'memory.forgotten', label: 'memory.forgotten' },
  { id: 'memory.exported', label: 'memory.exported' },
] as const;

export function WebhooksPage() {
  const { setPageTitle } = usePageMeta();
  useEffect(() => setPageTitle('Webhooks'), [setPageTitle]);
  useRequireAuth();
  const queryClient = useQueryClient();

  const list = useQuery({
    queryKey: ['cloud', '/v1/memory/webhooks/list'] as const,
    queryFn: () => cloudFetch<ListResult>('/v1/memory/webhooks/list'),
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Webhook | null>(null);
  const [viewingFor, setViewingFor] = useState<Webhook | null>(null);

  const create = useMutation({
    mutationFn: (input: { url: string; label: string; events: string[] }) =>
      cloudFetch<Webhook>('/v1/memory/webhooks', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      setCreateOpen(false);
      void queryClient.invalidateQueries({ queryKey: ['cloud', '/v1/memory/webhooks/list'] });
    },
  });

  const revoke = useMutation({
    mutationFn: (id: string) => cloudFetch<null>(`/v1/memory/webhooks/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setPendingDelete(null);
      void queryClient.invalidateQueries({ queryKey: ['cloud', '/v1/memory/webhooks/list'] });
    },
  });

  const webhooks = list.data?.webhooks ?? [];

  const columns: DataTableColumn<Webhook>[] = [
    {
      key: 'recent',
      label: '',
      render: (row) => (
        <RecentDeliveriesBadge webhookId={row.id} onClick={() => setViewingFor(row)} />
      ),
    },
    { key: 'label', label: 'Label', render: (row) => <span className="font-medium">{row.label}</span> },
    {
      key: 'url',
      label: 'URL',
      render: (row) => (
        <code className="font-mono text-xs text-muted-foreground">{row.url}</code>
      ),
    },
    {
      key: 'events',
      label: 'Events',
      render: (row) => (
        <div className="flex flex-wrap gap-1">
          {row.events.map((e) => (
            <Badge key={e} variant="outline">
              {e}
            </Badge>
          ))}
        </div>
      ),
    },
    {
      key: 'created_at',
      label: 'Created',
      render: (row) => (row.created_at ? <Timestamp value={row.created_at} /> : null),
    },
    {
      key: 'actions',
      label: '',
      align: 'right',
      render: (row) => (
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Delete webhook ${row.label}`}
          onClick={() => setPendingDelete(row)}
        >
          <Trash2 className="size-4 text-destructive" />
        </Button>
      ),
    },
  ];

  return (
    <>
      <PageShell
        title="Webhooks"
        description="Register URLs to be notified when memories change."
        primaryAction={{
          label: 'Add webhook',
          icon: Plus,
          onClick: () => setCreateOpen(true),
        }}
      >
        {list.isError ? (
          <Alert intent="warning" title="Could not load webhooks">
            The Memory service is not reachable right now. Try refreshing in a moment.
          </Alert>
        ) : list.isLoading ? (
          <Card><TableSkeleton rows={4} columns={5} /></Card>
        ) : webhooks.length === 0 ? (
          <EmptyState
            title="Register your first webhook"
            description="Webhooks let your services react to memory events (remember / forget / promote) as they happen — useful for re-indexing search caches, syncing CRMs, or triggering downstream agents."
            action={
              <Button onClick={() => setCreateOpen(true)}>
                <Plus className="size-4 mr-1" aria-hidden="true" />
                Add webhook
              </Button>
            }
          />
        ) : (
          <Card>
            <DataTable columns={columns} data={webhooks} rowKey={(row) => row.id} />
          </Card>
        )}
      </PageShell>

      <AddWebhookDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        submitting={create.isPending}
        onSubmit={(input) => create.mutate(input)}
      />

      {pendingDelete ? (
        <ConfirmDialog
          open={pendingDelete !== null}
          onOpenChange={(o) => !o && setPendingDelete(null)}
          title={`Delete webhook "${pendingDelete.label}"?`}
          description={
            <div className="space-y-2 text-sm">
              <p>
                This webhook will be permanently removed. Any future events for
                this URL will not be queued for delivery.
              </p>
              <p className="text-muted-foreground">This cannot be undone.</p>
            </div>
          }
          confirmLabel={revoke.isPending ? 'Deleting…' : 'Delete webhook'}
          cancelLabel="Cancel"
          intent="destructive"
          confirmationPhrase={pendingDelete.label}
          loading={revoke.isPending}
          onConfirm={() => revoke.mutate(pendingDelete.id)}
        />
      ) : null}

      {viewingFor ? (
        <DeliveriesSheet
          webhook={viewingFor}
          open={viewingFor !== null}
          onOpenChange={(o) => !o && setViewingFor(null)}
        />
      ) : null}
    </>
  );
}

/**
 * Memory Webhook Delivery plan T5.7 — per-row Recent deliveries chip.
 * Delivery Replay plan T8.4 — chip becomes a click trigger for the
 * DeliveriesSheet (lifting state to the page).
 */
function RecentDeliveriesBadge({
  webhookId,
  onClick,
}: {
  webhookId: string;
  onClick: () => void;
}) {
  const query = useQuery({
    queryKey: ['cloud', `/v1/memory/webhooks/${webhookId}/deliveries`] as const,
    queryFn: () => cloudFetch<DeliveriesResult>(`/v1/memory/webhooks/${webhookId}/deliveries`),
  });
  const deliveries = query.data?.deliveries ?? [];
  if (query.isLoading) {
    return <span className="text-xs text-muted-foreground">…</span>;
  }
  if (deliveries.length === 0) {
    return (
      <span className="text-xs text-muted-foreground" data-testid={`deliveries-empty-${webhookId}`}>
        No deliveries
      </span>
    );
  }
  const latest = deliveries[0];
  return (
    <button
      type="button"
      onClick={onClick}
      className="cursor-pointer"
      aria-label={`View deliveries for webhook ${webhookId}`}
    >
      <Badge variant={deliveryStatusVariant(latest.status)} data-testid={`deliveries-latest-${webhookId}`}>
        {deliveries.length} · {latest.status}
      </Badge>
    </button>
  );
}

/**
 * Delivery Replay plan T8.4 — Sheet listing all recent deliveries with a
 * per-row Replay button gated on status (failed | dead_letter only).
 */
interface DeliveriesSheetProps {
  webhook: Webhook;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function DeliveriesSheet({ webhook, open, onOpenChange }: DeliveriesSheetProps) {
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [pendingReplay, setPendingReplay] = useState<Delivery | null>(null);

  const query = useQuery({
    queryKey: ['cloud', `/v1/memory/webhooks/${webhook.id}/deliveries`] as const,
    queryFn: () => cloudFetch<DeliveriesResult>(`/v1/memory/webhooks/${webhook.id}/deliveries`),
    enabled: open,
  });

  const replay = useMutation({
    mutationFn: (deliveryId: string) =>
      cloudFetch<ReplayResult>(
        `/v1/memory/webhooks/${webhook.id}/deliveries/${deliveryId}/replay`,
        { method: 'POST' },
      ),
    onSuccess: (data) => {
      setPendingReplay(null);
      // cloudFetch returns T | null on empty response; on success we expect
      // the JSON body with status + noop. Default to noop=false when null
      // so the toast still surfaces — failure-mode is the destructive toast.
      const noop = data?.noop ?? false;
      toast({
        title: noop ? 'Delivery already queued' : 'Replay queued',
        description: noop
          ? 'This delivery is already pending. No action taken.'
          : 'The delivery has been re-queued and will be dispatched shortly.',
        variant: 'default',
      });
      void queryClient.invalidateQueries({
        queryKey: ['cloud', `/v1/memory/webhooks/${webhook.id}/deliveries`],
      });
    },
    onError: () => {
      toast({
        title: 'Could not replay delivery',
        description: 'Please try again. If the problem persists, contact support.',
        variant: 'destructive',
      });
    },
  });

  const deliveries = query.data?.deliveries ?? [];

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <Sheet.Content side="right">
          <Sheet.Header>
            <Sheet.Title>Recent deliveries</Sheet.Title>
            <Sheet.Description>
              {webhook.label} · last {deliveries.length} {deliveries.length === 1 ? 'delivery' : 'deliveries'}
            </Sheet.Description>
          </Sheet.Header>
          <Sheet.Body className="space-y-3">
            {query.isLoading ? (
              <p className="text-sm text-muted-foreground">Loading deliveries…</p>
            ) : deliveries.length === 0 ? (
              <p className="text-sm text-muted-foreground">No deliveries yet.</p>
            ) : (
              deliveries.map((d) => (
                <DeliveryCard
                  key={d.id}
                  delivery={d}
                  onReplayClick={() => setPendingReplay(d)}
                />
              ))
            )}
          </Sheet.Body>
        </Sheet.Content>
      </Sheet>

      {pendingReplay ? (
        <ConfirmDialog
          open={pendingReplay !== null}
          onOpenChange={(o) => !o && setPendingReplay(null)}
          title={`Replay delivery ${pendingReplay.id}?`}
          description={
            <div className="space-y-2 text-sm">
              <p>
                This will re-queue the delivery for dispatch. The subscriber
                URL will receive the same event payload again.
              </p>
              <p>
                <span className="font-medium">Event:</span> {pendingReplay.event_type}
              </p>
              {pendingReplay.last_error_message ? (
                <p className="text-muted-foreground">
                  Last error: <code className="font-mono text-xs">{pendingReplay.last_error_message}</code>
                </p>
              ) : null}
              <p className="text-muted-foreground">
                Retry budget will be reset. Idempotent — safe to retry.
              </p>
            </div>
          }
          confirmLabel={replay.isPending ? 'Replaying…' : 'Replay delivery'}
          cancelLabel="Cancel"
          intent="default"
          confirmationPhrase="replay"
          loading={replay.isPending}
          onConfirm={() => replay.mutate(pendingReplay.id)}
        />
      ) : null}
    </>
  );
}

function DeliveryCard({
  delivery,
  onReplayClick,
}: {
  delivery: Delivery;
  onReplayClick: () => void;
}) {
  return (
    <Card data-testid={`delivery-card-${delivery.id}`} className="p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1 min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Badge variant={deliveryStatusVariant(delivery.status)}>{delivery.status}</Badge>
            <code className="font-mono text-xs text-muted-foreground truncate">{delivery.id}</code>
          </div>
          <p className="text-sm font-medium">{delivery.event_type}</p>
          <p className="text-xs text-muted-foreground">
            <Timestamp value={delivery.created_at} /> · attempt {delivery.attempt_count}
          </p>
          {delivery.last_error_message ? (
            <p className="text-xs text-destructive truncate" title={delivery.last_error_message}>
              {delivery.last_error_message}
            </p>
          ) : null}
        </div>
        {canReplay(delivery.status) ? (
          <Button
            variant="secondary"
            size="sm"
            onClick={onReplayClick}
            aria-label={`Replay delivery ${delivery.id}`}
            data-testid={`replay-button-${delivery.id}`}
          >
            <RotateCw className="size-3.5 mr-1" />
            Replay
          </Button>
        ) : null}
      </div>
    </Card>
  );
}

interface AddWebhookDialogProps {
  open: boolean;
  onClose: () => void;
  submitting: boolean;
  onSubmit: (input: { url: string; label: string; events: string[] }) => void;
}

function AddWebhookDialog({ open, onClose, submitting, onSubmit }: AddWebhookDialogProps) {
  const [url, setUrl] = useState('');
  const [label, setLabel] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set([AVAILABLE_EVENTS[0].id]));

  const reset = () => {
    setUrl('');
    setLabel('');
    setSelected(new Set([AVAILABLE_EVENTS[0].id]));
  };
  const handleClose = () => {
    if (!submitting) {
      reset();
      onClose();
    }
  };
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    onSubmit({ url, label: label.trim(), events: Array.from(selected) });
  };

  const submitDisabled = submitting || !/^https?:\/\//i.test(url) || label.trim() === '' || selected.size === 0;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && handleClose()}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Add webhook</Dialog.Title>
          <Dialog.Description>Register a URL to be notified when memory events occur.</Dialog.Description>
        </Dialog.Header>
        <Dialog.Body>
          <form id="add-webhook-form" className="space-y-4" onSubmit={submit}>
            <div>
              <label htmlFor="wh-url" className="text-sm font-medium block mb-1">
                URL
              </label>
              <Input
                id="wh-url"
                placeholder="https://hook.example.com/memory"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="wh-label" className="text-sm font-medium block mb-1">
                Label
              </label>
              <Input
                id="wh-label"
                placeholder="prod-events"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                maxLength={64}
              />
            </div>
            <fieldset>
              <legend className="text-sm font-medium mb-2">Events</legend>
              <div className="space-y-2">
                {AVAILABLE_EVENTS.map((evt) => (
                  <label key={evt.id} className="flex items-center gap-2 cursor-pointer">
                    <Checkbox
                      checked={selected.has(evt.id)}
                      onCheckedChange={() => {
                        setSelected((prev) => {
                          const next = new Set(prev);
                          if (next.has(evt.id)) next.delete(evt.id);
                          else next.add(evt.id);
                          return next;
                        });
                      }}
                      aria-label={evt.id}
                    />
                    <span className="font-mono text-sm">{evt.label}</span>
                  </label>
                ))}
              </div>
            </fieldset>
          </form>
        </Dialog.Body>
        <Dialog.Footer>
          <Button type="button" variant="ghost" onClick={handleClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" form="add-webhook-form" disabled={submitDisabled}>
            {submitting ? 'Adding…' : 'Add webhook'}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
