import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Check, ChevronsUpDown, Loader2, Plus } from 'lucide-react';
import { Badge, Button, DropdownMenu } from '@loopzero/ui';
import { useWhoami } from '@/hooks/use-whoami';
import { useSwitchTeam } from '@/hooks/mutations/use-switch-team';

interface TeamSwitcherProps {
  /** Display name fallback when whoami data hasn't loaded yet. */
  fallbackName: string;
  /** Triggered when the trigger button is clicked outside dropdown (a11y). */
  onCreateTeam?: () => void;
}

/**
 * TeamSwitcher — Plan v1.0 T5.2.
 *
 * Sidebar Header's account/team dropdown. Renders user's name + current team
 * + chevron; opens a DropdownMenu with: current team (checked) + other teams
 * + "Create team" CTA at bottom.
 *
 * Switch calls POST /api/v1/auth/team/switch then reloads the page so all
 * tenant-scoped data refetches against the new context. Reload is heavier
 * than fine-grained cache invalidation but matches Vercel's behavior — the
 * app boundary IS the tenant.
 */
export function TeamSwitcher({ fallbackName, onCreateTeam }: TeamSwitcherProps) {
  const { whoami } = useWhoami();
  const navigate = useNavigate();
  const switchTeam = useSwitchTeam();
  const [switchingId, setSwitchingId] = useState<string | null>(null);

  const currentTeamId = whoami?.team_id ?? '';
  const currentTeamSlug = whoami?.team_slug ?? '';
  const teams = whoami?.teams ?? [];
  const userName = whoami?.name ?? whoami?.email ?? fallbackName;

  // T1.3 + EC-02: useSwitchTeam.onSuccess does queryClient.clear() (nuke
  // every tenant-scoped cache). navigate('/') replaces window.location.assign
  // — SPA-style, no full page reload.
  const handleSwitch = (teamId: string) => {
    if (teamId === currentTeamId) return;
    setSwitchingId(teamId);
    switchTeam.mutate(teamId, {
      onSuccess: () => navigate('/'),
      onSettled: () => setSwitchingId(null),
    });
  };
  const switching = switchingId;

  return (
    <DropdownMenu>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left hover:bg-muted/40 transition-colors"
          aria-label="Switch team"
        >
          <div className="min-w-0 flex-1">
            <p className="truncate text-body-sm font-medium text-foreground">
              {userName}
            </p>
            {currentTeamSlug && (
              <p className="truncate text-label text-muted-foreground">
                {currentTeamSlug}
              </p>
            )}
          </div>
          <ChevronsUpDown
            className="size-4 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="start" className="min-w-[220px]">
        <DropdownMenu.Label>Switch team</DropdownMenu.Label>
        <DropdownMenu.Separator />
        {teams.length === 0 ? (
          <DropdownMenu.Item disabled>
            <span className="text-muted-foreground">No teams available</span>
          </DropdownMenu.Item>
        ) : (
          teams.map((t) => {
            const isCurrent = t.id === currentTeamId;
            const isLoading = switching === t.id;
            return (
              <DropdownMenu.Item
                key={t.id}
                onSelect={(e) => {
                  if (isCurrent) {
                    e.preventDefault();
                    return;
                  }
                  void handleSwitch(t.id);
                }}
                disabled={isLoading}
              >
                <div className="flex w-full items-center justify-between gap-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-body-sm font-medium">{t.slug}</p>
                    <p className="text-label text-muted-foreground">{t.role}</p>
                  </div>
                  {isCurrent && (
                    <Check
                      className="size-4 shrink-0 text-success"
                      aria-label="Current team"
                    />
                  )}
                  {isLoading && (
                    <Loader2
                      className="size-4 shrink-0 animate-spin text-muted-foreground"
                      aria-label="Switching"
                    />
                  )}
                  {!isCurrent && !isLoading && t.role && (
                    <Badge variant="outline" className="text-label uppercase">
                      {t.role}
                    </Badge>
                  )}
                </div>
              </DropdownMenu.Item>
            );
          })
        )}
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          onSelect={() => {
            if (onCreateTeam) {
              onCreateTeam();
            } else {
              // Default: directs to CLI until backend POST /v1/teams ships
              window.open('https://docs.loopzero.example/teams/create', '_blank');
            }
          }}
        >
          <Plus className="size-4" aria-hidden="true" />
          <span>Create team</span>
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}

// Compatibility re-export for default-export shape if needed
export default TeamSwitcher;

// Helper re-export to keep callers from importing Button (avoid unused-import warnings in test environments)
export { Button };
