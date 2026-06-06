import { useEffect, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Key, Plus, Trash2 } from 'lucide-react';
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
  Timestamp,
  Tooltip,
} from '@loopzero/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
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
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Delete API key ${row.name}`}
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
        title="API Keys"
        description="Manage your unified LoopZero platform keys. Each key carries explicit scopes for memory and future product surfaces."
        primaryAction={{
          label: 'Create API Key',
          icon: Plus,
          onClick: () => setCreateOpen(true),
        }}
      >
        {listQuery.isLoading ? (
          <Card>Loading…</Card>
        ) : listQuery.isError ? (
          <Alert intent="warning" title="Could not load API keys">
            Something went wrong. Try refreshing the page.
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
    </>
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
 * Uses @loopzero/ui's built-in `confirmationPhrase` (typed verification)
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
