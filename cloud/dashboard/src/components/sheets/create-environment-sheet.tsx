import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Checkbox, Input, Label, Select, Sheet } from '@loopzero/ui';
import { engineFetch } from '@/lib/api';
import { useEngineApi } from '@/hooks/use-api';
import type { Environment, Project } from '@/lib/api/types';

interface CreateEnvironmentSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (env: Environment) => void;
}

const NAME_PATTERN = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/**
 * CreateEnvironmentSheet — M2 v1.2 T5.1 (2026-06-07).
 *
 * Adds a project picker (canonical 3-segment namespace requires
 * `project` post-M2). Behaviour:
 *
 *   - 0 projects → EC-2 empty state: Alert + CTA "Create a project →"
 *     route to /projects. Submit button absent (the form is meaningless
 *     without a project).
 *   - 1 project  → auto-fill the picker + disable change. User just
 *     types the env name; one click less for the common case.
 *   - ≥2 projects → user MUST pick. Submit disabled until a non-empty
 *     project is selected.
 *
 * Body POST now includes `project` (per T4.1 backend contract). Empty
 * project would return 400 MISSING_FIELD from the backend; the front-end
 * gate prevents that round-trip.
 */
export function CreateEnvironmentSheet({ open, onOpenChange, onCreated }: CreateEnvironmentSheetProps) {
  const [name, setName] = useState('');
  const [project, setProject] = useState('');
  const [isProtected, setIsProtected] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { data: projectsData, error: projectsError } = useEngineApi<{ projects: Project[] }>('/api/v1/projects');
  const projects = projectsData?.projects ?? [];
  const hasProjects = projects.length > 0;
  const singleProject = projects.length === 1;

  // Auto-fill when exactly 1 project. Reset when the sheet re-opens.
  useEffect(() => {
    if (open && singleProject) {
      setProject(projects[0]!.name);
    }
    if (open && !singleProject) {
      // Don't clobber a previously-picked value mid-session, but reset
      // when the sheet re-opens on a tenant with multiple projects.
      setProject((curr) => (curr && projects.some((p) => p.name === curr) ? curr : ''));
    }
  }, [open, singleProject, projects]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!NAME_PATTERN.test(name)) {
      setError('Name must be lowercase, 3-40 chars, hyphens allowed.');
      return;
    }
    if (!project) {
      setError('Pick a project for the environment.');
      return;
    }
    setSubmitting(true);
    try {
      const res = await engineFetch<Environment>('/api/v1/environments', {
        method: 'POST',
        body: JSON.stringify({ name, project, protected: isProtected }),
      });
      if (res === null) {
        setError("Couldn't create environment. Try again in a moment.");
        return;
      }
      onCreated?.(res);
      setName('');
      setProject(singleProject ? projects[0]!.name : '');
      setIsProtected(false);
      onOpenChange(false);
    } catch (e) {
      // T0.1 TCIA — sponsor critério #6
      setError(
        e instanceof Error
          ? `Create environment failed: ${e.message}. No environment was created. Retry, or contact support if it persists.`
          : 'Create environment failed. No environment was created. Retry, or contact support if it persists.',
      );
    } finally {
      setSubmitting(false);
    }
  };

  // T4.1 finding 1.1 (P1): projects fetch failed → render error state
  // BEFORE the empty/form decision. Without this branch a 5xx from
  // /api/v1/projects renders an empty form the user can't submit (no
  // project to pick), masking the true root cause.
  if (projectsError) {
    return (
      <Sheet open={open} onOpenChange={onOpenChange}>
        <Sheet.Content side="right" className="w-full sm:max-w-md flex flex-col p-0">
          <Sheet.Header className="px-6 py-5 border-b border-border/60">
            <Sheet.Title>New environment</Sheet.Title>
            <Sheet.Description>
              We couldn't load the project list.
            </Sheet.Description>
          </Sheet.Header>

          <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            <Alert
              intent="destructive"
              title="Couldn't load projects"
              description="The dashboard couldn't fetch your projects. Check connectivity and reopen this panel."
            />
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Close
            </Button>
          </Sheet.Footer>
        </Sheet.Content>
      </Sheet>
    );
  }

  // EC-2: zero projects → render an actionable empty state instead of
  // a form the user cannot submit.
  if (!hasProjects && projectsData !== undefined) {
    return (
      <Sheet open={open} onOpenChange={onOpenChange}>
        <Sheet.Content side="right" className="w-full sm:max-w-md flex flex-col p-0">
          <Sheet.Header className="px-6 py-5 border-b border-border/60">
            <Sheet.Title>New environment</Sheet.Title>
            <Sheet.Description>
              You need a project before creating an environment.
            </Sheet.Description>
          </Sheet.Header>

          <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            <Alert
              intent="info"
              title="No projects yet"
              description="Environments live inside projects. Create your first project to add an environment to it."
            />
            <div data-testid="create-env-empty-state-cta">
              <Button asChild>
                <Link to="/projects">Create a project →</Link>
              </Button>
            </div>
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Close
            </Button>
          </Sheet.Footer>
        </Sheet.Content>
      </Sheet>
    );
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <Sheet.Content side="right" className="w-full sm:max-w-md flex flex-col p-0">
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <Sheet.Header className="px-6 py-5 border-b border-border/60">
            <Sheet.Title>New environment</Sheet.Title>
            <Sheet.Description>
              Environments are isolated namespaces for your deployments.
            </Sheet.Description>
          </Sheet.Header>

          <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            <div>
              <Label htmlFor="env-project">Project</Label>
              <Select
                value={project}
                onValueChange={(v) => setProject(v)}
                disabled={singleProject || submitting}
              >
                <Select.Trigger id="env-project" data-testid="env-project-trigger">
                  <Select.Value placeholder="Pick a project" />
                </Select.Trigger>
                <Select.Content>
                  {projects.map((p) => (
                    <Select.Item key={p.name} value={p.name}>
                      {p.name}
                    </Select.Item>
                  ))}
                </Select.Content>
              </Select>
              {singleProject && (
                <p className="text-label text-muted-foreground mt-1">
                  Only one project — auto-selected.
                </p>
              )}
            </div>

            <div>
              <Label htmlFor="env-name">Name</Label>
              <Input
                id="env-name"
                type="text"
                placeholder="staging"
                value={name}
                onChange={(e) => setName(e.target.value.toLowerCase())}
                autoFocus
                required
              />
            </div>

            <div className="flex items-start gap-2">
              <Checkbox
                id="env-protected"
                checked={isProtected}
                onCheckedChange={(c) => setIsProtected(c === true)}
              />
              <div>
                <Label htmlFor="env-protected">Protected</Label>
                <p className="text-label text-muted-foreground">
                  Requires explicit promote/rollback confirmation. Use for production-like envs.
                </p>
              </div>
            </div>

            {error && <Alert intent="destructive" description={error} />}
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitting || !project || !name}>
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Creating…' : 'Create environment'}
            </Button>
          </Sheet.Footer>
        </form>
      </Sheet.Content>
    </Sheet>
  );
}
