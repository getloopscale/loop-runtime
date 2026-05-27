import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Input, Label, Sheet } from '@loopzero/ui';
import { engineFetch } from '@/lib/api';
import type { Domain } from '@/lib/api/types';

interface AddDomainSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultProject?: string;
  onAdded?: (domain: Domain) => void;
}

const DOMAIN_PATTERN = /^[a-z0-9.-]+\.[a-z]{2,}$/;

/**
 * AddDomainSheet — right-side Sheet (2026 FAANG pattern).
 * Preserves domain table behind so user sees what's already there.
 */
export function AddDomainSheet({
  open,
  onOpenChange,
  defaultProject = '',
  onAdded,
}: AddDomainSheetProps) {
  const [domain, setDomain] = useState('');
  const [project, setProject] = useState(defaultProject);
  const [app, setApp] = useState('');
  const [environment, setEnvironment] = useState('production');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!DOMAIN_PATTERN.test(domain)) {
      setError('Enter a valid domain (e.g., app.example.com).');
      return;
    }
    setSubmitting(true);
    try {
      const res = await engineFetch<Domain>('/api/v1/domains', {
        method: 'POST',
        body: JSON.stringify({ domain, project, app, environment }),
      });
      if (res === null) {
        setError("Couldn't add domain. Try again in a moment.");
        return;
      }
      onAdded?.(res);
      setDomain('');
      onOpenChange(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <Sheet.Content side="right" className="w-full sm:max-w-md flex flex-col p-0">
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <Sheet.Header className="px-6 py-5 border-b border-border/60">
            <Sheet.Title>Add custom domain</Sheet.Title>
            <Sheet.Description>
              We'll generate the DNS records you need to add at your registrar.
            </Sheet.Description>
          </Sheet.Header>

          <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            <div>
              <Label htmlFor="add-domain">Domain</Label>
              <Input
                id="add-domain"
                type="text"
                placeholder="app.acme.com"
                value={domain}
                onChange={(e) => setDomain(e.target.value.toLowerCase())}
                autoFocus
                required
              />
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <Label htmlFor="add-domain-project">Project</Label>
                <Input
                  id="add-domain-project"
                  type="text"
                  placeholder="acme-web"
                  value={project}
                  onChange={(e) => setProject(e.target.value)}
                  required
                />
              </div>
              <div>
                <Label htmlFor="add-domain-app">App</Label>
                <Input
                  id="add-domain-app"
                  type="text"
                  placeholder="frontend"
                  value={app}
                  onChange={(e) => setApp(e.target.value)}
                  required
                />
              </div>
            </div>

            <div>
              <Label htmlFor="add-domain-env">Environment</Label>
              <Input
                id="add-domain-env"
                type="text"
                placeholder="production"
                value={environment}
                onChange={(e) => setEnvironment(e.target.value)}
              />
            </div>

            {error && <Alert intent="destructive" description={error} />}
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Adding…' : 'Add domain'}
            </Button>
          </Sheet.Footer>
        </form>
      </Sheet.Content>
    </Sheet>
  );
}
