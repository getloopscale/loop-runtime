import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Trash2 } from 'lucide-react';
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
  Timestamp,
} from '@loopzero/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { cloudFetch } from '@/lib/api';

/**
 * Plan 3 of dashboard-memory-section-roadmap.md, T3.7.
 *
 * /memory/webhooks — webhook registration UI.
 *
 * D2 invariant (EC-14): a persistent <Alert intent="warning"> renders at
 * the TOP of the page (NOT footer, NOT dismissible) disclosing that
 * webhook DELIVERY is not yet implemented. Half-shipped features lie by
 * omission; the banner is honest.
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
        {/* D2 / EC-14 — persistent demo-only Alert at TOP. Never dismissible.
            Use the `description` prop (children are ignored by @loopzero/ui Alert). */}
        <Alert
          intent="warning"
          title="Demo only"
          description="Webhook delivery is not yet implemented. You can register and view webhooks; events will not fire until we ship delivery in a future release."
          data-testid="webhook-demo-alert"
        />

        {list.isError ? (
          <Alert intent="warning" title="Could not load webhooks">
            Try again in a moment.
          </Alert>
        ) : list.isLoading ? (
          <Card>Loading…</Card>
        ) : webhooks.length === 0 ? (
          <EmptyState
            title="No webhooks yet"
            description="Click Add webhook to register your first endpoint."
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
    </>
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
