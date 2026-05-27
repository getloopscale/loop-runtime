import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Input, Label, Sheet } from '@loopzero/ui';
import { engineFetch } from '@/lib/api';
import type { Project } from '@/lib/api/types';

interface CreateProjectSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (project: Project) => void;
}

const SLUG_PATTERN = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/**
 * CreateProjectSheet — right-side Sheet (2026 FAANG pattern).
 *
 * Why Sheet vs centered Dialog: forms benefit from preserving context (user
 * still sees the project list behind the sheet). Vercel/Linear/Notion all
 * use right-side sheets for create/edit forms since ~2024.
 *
 * Padding is intentional: Sheet.Header / Sheet.Body / Sheet.Footer don't
 * include internal padding by default — we add `px-6 py-5` for breathing
 * room. Without it, content visually "glues" to the sheet edge.
 */
export function CreateProjectSheet({ open, onOpenChange, onCreated }: CreateProjectSheetProps) {
  const [name, setName] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!SLUG_PATTERN.test(name)) {
      setError('Name must be lowercase, 3-40 chars, start with letter, hyphens allowed.');
      return;
    }
    setSubmitting(true);
    try {
      const res = await engineFetch<Project>('/api/v1/projects', {
        method: 'POST',
        body: JSON.stringify({ name }),
      });
      if (res === null) {
        setError("Couldn't create project. Try again in a moment.");
        return;
      }
      onCreated?.(res);
      setName('');
      onOpenChange(false);
    } catch (e) {
      // T0.1 TCIA — sponsor critério #6
      setError(
        e instanceof Error
          ? `Create project failed: ${e.message}. No project was created. Retry, or contact support if it persists.`
          : 'Create project failed. No project was created. Retry, or contact support if it persists.',
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <Sheet.Content side="right" className="w-full sm:max-w-md flex flex-col p-0">
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <Sheet.Header className="px-6 py-5 border-b border-border/60">
            <Sheet.Title>New project</Sheet.Title>
            <Sheet.Description>
              Register a new project. The full GitHub picker + framework detect wizard lands in a future iter.
            </Sheet.Description>
          </Sheet.Header>

          <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            <div>
              <Label htmlFor="project-name">Project name</Label>
              <Input
                id="project-name"
                type="text"
                placeholder="my-app"
                value={name}
                onChange={(e) => setName(e.target.value.toLowerCase())}
                autoFocus
                required
              />
              <p className="mt-1 text-label text-muted-foreground">
                Lowercase, 3-40 chars, hyphens allowed. Used in URLs.
              </p>
            </div>

            {error && <Alert intent="destructive" description={error} />}
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Creating…' : 'Create project'}
            </Button>
          </Sheet.Footer>
        </form>
      </Sheet.Content>
    </Sheet>
  );
}
