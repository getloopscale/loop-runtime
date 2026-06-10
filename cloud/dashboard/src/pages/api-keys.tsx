import { useEffect, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { History, Key, Plus, Recycle, Trash2 } from 'lucide-react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  CodeBlock,
  ConfirmDialog,
  CopyButton,
  DataTable,
  type DataTableColumn,
  Dialog,
  EmptyState,
  Input,
  PageShell,
  Sheet,
  Timestamp,
  Tooltip,
} from '@loopzerokit/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { TableSkeleton } from '@/components/data/table-skeleton';
import { cloudFetch } from '@/lib/api';

/**
 * Plan 0 of dashboard-memory-section-roadmap.md, T0.1.
 * Platform unified API keys management page.
 *
 * Consumes the backend ADRs locked in 2026-06-06-platform-api-keys-with-scopes-plan.md v1.2:
 *  - D1 single loopzero_live_* key, multi-scope
 *  - D2 scope enum v0.1 (memory:*) + reserved (rag:*|traces:*|agents:*|deploys:*)
 *  - D3 secret displayed ONCE post-create; masked thereafter via key_prefix
 *  - D7 trash performs soft-delete via DELETE /v1/api-keys/{id}
 *  - DESIGN.md v1.0 5Q-3s + danger zone choreography per Memory Section mandate
 */

// Mirrors the wire shape returned by GET /v1/api-keys/list — see APIKeyView in
// cloud/internal/routes/api_keys.go. Wire field names use snake_case per the
// Go JSON tags; keep them verbatim on the UI side to avoid translation churn.
interface APIKeyView {
  id: string;
  name: string;
  key_prefix: string;
  scopes: string[];
  created_at: string;
  last_used_at?: string | null;
  expires_at?: string | null;
}

interface CreateKeyResponse {
  id: string;
  name: string;
  key_prefix: string;
  scopes: string[];
  /** Raw secret — shown ONCE; never returned again. */
  secret: string;
}

interface ListKeysResponse {
  keys: APIKeyView[];
}

const ACTIVE_SCOPES = [
  { id: 'memory:read', label: 'memory:read', description: 'Recall memories and read history' },
  { id: 'memory:write', label: 'memory:write', description: 'Remember new facts and reflect' },
  { id: 'memory:delete', label: 'memory:delete', description: 'Forget specific memories' },
  { id: 'memory:admin', label: 'memory:admin', description: 'Settings, webhooks, exports' },
] as const;

const RESERVED_SCOPES = [
  { id: 'rag:read', vertical: 'Retrieval' },
  { id: 'rag:write', vertical: 'Retrieval' },
  { id: 'traces:read', vertical: 'Traces' },
  { id: 'agents:read', vertical: 'Agents' },
  { id: 'agents:invoke', vertical: 'Agents' },
  { id: 'deploys:read', vertical: 'Deploys' },
  { id: 'deploys:write', vertical: 'Deploys' },
] as const;

export function ApiKeysPage() {
  const { setPageTitle } = usePageMeta();
  useEffect(() => setPageTitle('API Keys'), [setPageTitle]);
  useRequireAuth();
  const queryClient = useQueryClient();

  const listQuery = useQuery({
    queryKey: ['cloud', '/v1/api-keys/list'] as const,
    queryFn: () => cloudFetch<ListKeysResponse>('/v1/api-keys/list'),
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [revealed, setRevealed] = useState<CreateKeyResponse | null>(null);
  const [pendingDelete, setPendingDelete] = useState<APIKeyView | null>(null);
  // API Key Rotation plan T6.5 — pendingRotate mirrors pendingDelete pattern.
  const [pendingRotate, setPendingRotate] = useState<APIKeyView | null>(null);
  // API Key Lineage View plan T7.4 — pendingLineage opens the Sheet.
  const [pendingLineage, setPendingLineage] = useState<APIKeyView | null>(null);

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['cloud', '/v1/api-keys/list'] });

  const createMutation = useMutation({
    mutationFn: async (input: { name: string; scopes: string[]; expires_at?: string | null }) =>
      cloudFetch<CreateKeyResponse>('/v1/api-keys', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(input),
      }),
    onSuccess: (resp) => {
      setRevealed(resp);
      setCreateOpen(false);
      invalidate();
    },
  });

  const revokeMutation = useMutation({
    mutationFn: async (id: string) =>
      cloudFetch<null>(`/v1/api-keys/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setPendingDelete(null);
      invalidate();
    },
  });

  // API Key Rotation plan T6.5 — POST /v1/api-keys/{id}/rotate.
  // On success, reuse the existing RevealedSecretDialog for show-once secret.
  const rotateMutation = useMutation({
    mutationFn: async (id: string) =>
      cloudFetch<CreateKeyResponse>(`/v1/api-keys/${id}/rotate`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({}),
      }),
    onSuccess: (resp) => {
      setRevealed(resp);
      setPendingRotate(null);
      invalidate();
    },
  });

  const keys = listQuery.data?.keys ?? [];

  const columns: DataTableColumn<APIKeyView>[] = [
    {
      key: 'name',
      label: 'Name',
      render: (row) => <span className="font-medium">{row.name}</span>,
    },
    {
      key: 'key',
      label: 'Key',
      render: (row) => (
        <code className="text-xs font-mono text-muted-foreground">{row.key_prefix}…</code>
      ),
    },
    {
      key: 'scopes',
      label: 'Scopes',
      render: (row) => (
        <div className="flex flex-wrap gap-1">
          {row.scopes.map((s) => (
            <Badge key={s} variant="outline">
              {s}
            </Badge>
          ))}
        </div>
      ),
    },
    {
      key: 'created_at',
      label: 'Created',
      render: (row) => <Timestamp value={row.created_at} />,
    },
    {
      key: 'last_used_at',
      label: 'Last used',
      render: (row) =>
        row.last_used_at ? (
          <Timestamp value={row.last_used_at} />
        ) : (
          <span className="text-muted-foreground">Never</span>
        ),
    },
    {
      key: 'actions',
      label: '',
      align: 'right',
      render: (row) => (
        <div className="flex gap-1 justify-end">
          {/* API Key Lineage View plan T7.4 — Lineage button per row. */}
          <Button
            variant="ghost"
            size="sm"
            aria-label={`View lineage for API key ${row.name}`}
            onClick={() => setPendingLineage(row)}
          >
            <History className="size-4" />
          </Button>
          {/* API Key Rotation plan T6.5 — Rotate button per row. */}
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Rotate API key ${row.name}`}
            onClick={() => setPendingRotate(row)}
          >
            <Recycle className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Delete API key ${row.name}`}
            onClick={() => setPendingDelete(row)}
          >
            <Trash2 className="size-4 text-destructive" />
          </Button>
        </div>
      ),
    },
  ];

  return (
    <>
      <PageShell
        title="API Keys"
        description="Manage your unified LoopZero platform keys. Each key carries explicit scopes for memory and future product surfaces."
        primaryAction={{
          label: 'Create API Key',
          icon: Plus,
          onClick: () => setCreateOpen(true),
        }}
      >
        {listQuery.isLoading ? (
          <Card><TableSkeleton rows={4} columns={5} /></Card>
        ) : listQuery.isError ? (
          <Alert intent="warning" title="Could not load API keys">
            The platform service is not reachable right now. Existing keys keep
            working at the edge — only this listing is affected.
          </Alert>
        ) : keys.length === 0 ? (
          <EmptyState
            icon={Key}
            title="No API keys yet"
            description="Create your first key to enable programmatic access from your code."
            action={
              <Button onClick={() => setCreateOpen(true)}>
                <Plus className="size-4 mr-1" />
                Create API Key
              </Button>
            }
          />
        ) : (
          <Card>
            <DataTable columns={columns} data={keys} rowKey={(row) => row.id} />
          </Card>
        )}
      </PageShell>

      <CreateApiKeyDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onSubmit={(input) => createMutation.mutate(input)}
        submitting={createMutation.isPending}
        error={createMutation.error}
      />

      <RevealedSecretDialog
        revealed={revealed}
        onClose={() => setRevealed(null)}
      />

      <ConfirmDeleteDialog
        target={pendingDelete}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => pendingDelete && revokeMutation.mutate(pendingDelete.id)}
        submitting={revokeMutation.isPending}
      />

      <ConfirmRotateDialog
        target={pendingRotate}
        onCancel={() => setPendingRotate(null)}
        onConfirm={() => pendingRotate && rotateMutation.mutate(pendingRotate.id)}
        submitting={rotateMutation.isPending}
      />

      <LineageSheet
        target={pendingLineage}
        onClose={() => setPendingLineage(null)}
      />
    </>
  );
}

interface LineageItem {
  id: string;
  name: string;
  key_prefix: string;
  created_at: string;
  revoked_at?: string | null;
  expires_at?: string | null;
  is_current: boolean;
}

interface LineageResult {
  lineage: LineageItem[];
}

interface LineageSheetProps {
  target: APIKeyView | null;
  onClose: () => void;
}

/**
 * API Key Lineage View plan T7.4 — Sheet drawer showing the rotation chain
 * as a vertical timeline. Each item: prefix + name + status badge.
 * Current key highlighted with a "Current" badge.
 */
function LineageSheet({ target, onClose }: LineageSheetProps) {
  const open = target !== null;
  const query = useQuery({
    queryKey: ['cloud', `/v1/api-keys/${target?.id ?? ''}/lineage`] as const,
    queryFn: () => cloudFetch<LineageResult>(`/v1/api-keys/${target!.id}/lineage`),
    enabled: open && !!target,
  });

  return (
    <Sheet open={open} onOpenChange={(o) => !o && onClose()}>
      <Sheet.Content side="right">
        <Sheet.Header>
          <Sheet.Title>Rotation history</Sheet.Title>
          <Sheet.Description>
            {target ? `Lineage of "${target.name}" — root key first, current key last.` : null}
          </Sheet.Description>
        </Sheet.Header>
        <div className="p-4 space-y-3" data-testid="lineage-timeline">
          {query.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading lineage…</p>
          ) : query.isError ? (
            <Alert intent="warning" title="Could not load lineage" description="Try again in a moment." />
          ) : (
            (query.data?.lineage ?? []).map((item) => (
              <LineageRow key={item.id} item={item} />
            ))
          )}
        </div>
      </Sheet.Content>
    </Sheet>
  );
}

function LineageRow({ item }: { item: LineageItem }) {
  let statusLabel = 'Active';
  let statusVariant: 'default' | 'success' | 'warning' | 'destructive' = 'default';
  if (item.revoked_at) {
    statusLabel = 'Revoked';
    statusVariant = 'destructive';
  } else if (item.expires_at && new Date(item.expires_at) < new Date()) {
    statusLabel = 'Expired';
    statusVariant = 'warning';
  } else if (item.is_current) {
    statusLabel = 'Active';
    statusVariant = 'success';
  }

  return (
    <Card
      className={item.is_current ? 'border-primary' : undefined}
      data-testid={`lineage-row-${item.id}`}
    >
      <div className="p-3 space-y-1">
        <div className="flex items-center justify-between gap-2">
          <code className="font-mono text-xs text-muted-foreground">{item.key_prefix}…</code>
          <div className="flex gap-1">
            {item.is_current ? (
              <Badge variant="success" data-testid={`lineage-current-${item.id}`}>
                Current
              </Badge>
            ) : null}
            <Badge variant={statusVariant}>{statusLabel}</Badge>
          </div>
        </div>
        <p className="text-sm font-medium">{item.name}</p>
        <p className="text-xs text-muted-foreground">
          Created <Timestamp value={item.created_at} />
        </p>
      </div>
    </Card>
  );
}

interface ConfirmRotateDialogProps {
  target: APIKeyView | null;
  onCancel: () => void;
  onConfirm: () => void;
  submitting: boolean;
}

/**
 * API Key Rotation plan T6.5 — ConfirmDialog for the rotate flow.
 * Uses the same typed-verification pattern as ConfirmDeleteDialog.
 * Body copy explains the 24h default grace window so the customer
 * understands BOTH keys will work during the overlap.
 */
function ConfirmRotateDialog({ target, onCancel, onConfirm, submitting }: ConfirmRotateDialogProps) {
  if (!target) return null;

  return (
    <ConfirmDialog
      open={target !== null}
      onOpenChange={(o) => !o && onCancel()}
      title={`Rotate API key "${target.name}"?`}
      description={
        <div className="space-y-2 text-sm">
          <p>
            A new API key will be generated immediately. The old key will keep
            working for 24 hours, then expire automatically. Both keys overlap
            during the grace window so you can update your code without
            downtime.
          </p>
          <p className="text-muted-foreground">
            Type the key name to confirm. You will see the new secret only once.
          </p>
        </div>
      }
      confirmLabel={submitting ? 'Rotating…' : 'Rotate key'}
      cancelLabel="Cancel"
      intent="destructive"
      confirmationPhrase={target.name}
      loading={submitting}
      onConfirm={onConfirm}
    />
  );
}

interface CreateApiKeyDialogProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (input: { name: string; scopes: string[]; expires_at?: string | null }) => void;
  submitting: boolean;
  error: unknown;
}

/**
 * Create dialog. Form is intentionally simple — Input + scope checkboxes +
 * optional expiration. Validation mirrors the backend (`EMPTY_SCOPES`,
 * `EMPTY_NAME`, `NAME_TOO_LONG`, `EXPIRES_IN_PAST`) so the customer never
 * sees a server-side 400 they could have prevented client-side.
 */
function CreateApiKeyDialog({ open, onClose, onSubmit, submitting, error }: CreateApiKeyDialogProps) {
  const [name, setName] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set(['memory:read']));
  const [expiresAt, setExpiresAt] = useState('');

  const reset = () => {
    setName('');
    setSelected(new Set(['memory:read']));
    setExpiresAt('');
  };

  const handleClose = () => {
    if (!submitting) {
      reset();
      onClose();
    }
  };

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    if (submitting) return;
    if (name.trim() === '' || selected.size === 0) return;
    onSubmit({
      name: name.trim(),
      scopes: Array.from(selected),
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : null,
    });
  };

  const toggleScope = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const submitDisabled = submitting || name.trim() === '' || selected.size === 0;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && handleClose()}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Create API Key</Dialog.Title>
          <Dialog.Description>
            Choose scopes carefully — a key with fewer scopes is safer if it leaks.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Body>
          <form id="create-api-key-form" className="space-y-4" onSubmit={handleSubmit}>
            <div>
              <label htmlFor="api-key-name" className="text-sm font-medium block mb-1">
                Name
              </label>
              <Input
                id="api-key-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="prod-agent"
                maxLength={64}
                autoFocus
              />
              <p className="text-xs text-muted-foreground mt-1">
                A short label. Shown in the keys list and audit log.
              </p>
            </div>

            <fieldset>
              <legend className="text-sm font-medium mb-2">Scopes</legend>
              <div className="space-y-2">
                {ACTIVE_SCOPES.map((scope) => (
                  <label key={scope.id} className="flex items-start gap-2 cursor-pointer">
                    <Checkbox
                      checked={selected.has(scope.id)}
                      onCheckedChange={() => toggleScope(scope.id)}
                      aria-label={scope.id}
                    />
                    <span>
                      <span className="font-mono text-sm">{scope.label}</span>
                      <span className="text-xs text-muted-foreground block">{scope.description}</span>
                    </span>
                  </label>
                ))}
              </div>

              <p className="text-xs text-muted-foreground mt-3 mb-2">
                Reserved for future surfaces:
              </p>
              <div className="flex flex-wrap gap-1">
                {RESERVED_SCOPES.map((scope) => (
                  <Tooltip
                    key={scope.id}
                    label={`Coming with the ${scope.vertical} product surface`}
                  >
                    <Badge variant="outline" className="opacity-50">
                      {scope.id}
                    </Badge>
                  </Tooltip>
                ))}
              </div>
            </fieldset>

            <div>
              <label htmlFor="api-key-expires" className="text-sm font-medium block mb-1">
                Expiration <span className="text-muted-foreground">(optional)</span>
              </label>
              <Input
                id="api-key-expires"
                type="date"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
                min={new Date().toISOString().split('T')[0]}
              />
              <p className="text-xs text-muted-foreground mt-1">
                Leave blank for a key that never expires.
              </p>
            </div>

            {error ? (
              <Alert intent="destructive" title="Could not create key">
                {error instanceof Error ? error.message : 'Please try again.'}
              </Alert>
            ) : null}
          </form>
        </Dialog.Body>
        <Dialog.Footer>
          <Button type="button" variant="ghost" onClick={handleClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" form="create-api-key-form" disabled={submitDisabled}>
            {submitting ? 'Creating…' : 'Create key'}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

interface RevealedSecretDialogProps {
  revealed: CreateKeyResponse | null;
  onClose: () => void;
}

/**
 * EC-13 absorption: the "Done" button is DISABLED until the customer
 * acknowledges they've copied the secret. This is the show-once contract —
 * the raw secret never appears in any response again, never persists in
 * localStorage / sessionStorage, never goes through analytics. If the
 * customer dismisses without copying, the only recovery path is "revoke
 * and create new" (intentional friction).
 */
function RevealedSecretDialog({ revealed, onClose }: RevealedSecretDialogProps) {
  const [copied, setCopied] = useState(false);

  if (!revealed) return null;

  const handleClose = () => {
    if (!copied) return;
    setCopied(false);
    onClose();
  };

  return (
    <Dialog open={revealed !== null} onOpenChange={(o) => !o && handleClose()}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Your new API key</Dialog.Title>
          <Dialog.Description>
            Copy this key now — it will never be shown again.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Body>
          <div className="space-y-4">
            <Alert intent="info" title="Copy this key now">
              This is the only time the full key will be shown. After you close
              this dialog, you can only see the prefix. If you lose the key,
              revoke it and create a new one.
            </Alert>

            <CodeBlock language="text" code={revealed.secret} />

            <div className="flex justify-end">
              <CopyButton value={revealed.secret} />
            </div>

            <label className="flex items-start gap-2 cursor-pointer pt-2 border-t border-border">
              <Checkbox
                checked={copied}
                onCheckedChange={(v) => setCopied(v === true)}
                aria-label="I've copied my key safely"
              />
              <span className="text-sm">I've copied my key safely</span>
            </label>
          </div>
        </Dialog.Body>
        <Dialog.Footer>
          <Button
            type="button"
            onClick={handleClose}
            disabled={!copied}
            aria-disabled={!copied}
          >
            Done
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

interface ConfirmDeleteDialogProps {
  target: APIKeyView | null;
  onCancel: () => void;
  onConfirm: () => void;
  submitting: boolean;
}

/**
 * DESIGN.md danger zone choreography per Memory Section mandate.
 * Uses @loopzerokit/ui's built-in `confirmationPhrase` (typed verification)
 * + `intent="destructive"` (NOT primary).
 */
function ConfirmDeleteDialog({ target, onCancel, onConfirm, submitting }: ConfirmDeleteDialogProps) {
  if (!target) return null;

  return (
    <ConfirmDialog
      open={target !== null}
      onOpenChange={(o) => !o && onCancel()}
      title={`Delete API key "${target.name}"`}
      description={
        <div className="space-y-2 text-sm">
          <p>
            This API key will be revoked immediately. Any application using it
            will receive 401 Unauthorized on its next request.
          </p>
          <p className="text-muted-foreground">
            This cannot be undone. To re-enable access, create a new key and
            update your application.
          </p>
        </div>
      }
      confirmLabel={submitting ? 'Deleting…' : 'Delete key'}
      cancelLabel="Cancel"
      intent="destructive"
      confirmationPhrase={target.name}
      onConfirm={onConfirm}
      loading={submitting}
    />
  );
}
