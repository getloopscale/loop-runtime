import { Command } from 'cmdk';
import { Dialog } from '@loopzero/ui';
import type { ComponentType } from 'react';
import { reportError } from '@/lib/error-reporter';

/**
 * CommandPalette (Sprint 2 T2.1) — global ⌘K command surface.
 *
 * Wraps `cmdk` v1 (industry-standard primitive used by Vercel + Linear +
 * Raycast clones) com Dialog from @loopzero/ui. Categories rendered as cmdk
 * Groups; commands as cmdk Items.
 *
 * EC-09: cmdk uses refs + imperative state internally. If React Compiler
 * rejects, add `'use no memo'` directive at top of this file. Build-time
 * discovery — verified GREEN as of Sprint 2.
 *
 * EC-11: command.perform() wrapped em try/catch + reportError so handler
 * exceptions DON'T cascade to RouteBoundary. Palette closes either way.
 */

export type CommandCategory = 'navigate' | 'action' | 'project' | 'deploy';

export interface CommandItem {
  id: string;
  category: CommandCategory;
  label: string;
  keywords?: string[];
  icon?: ComponentType<{ className?: string }>;
  shortcut?: string;
  perform: () => void;
}

interface CommandPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  commands: CommandItem[];
}

const CATEGORY_LABEL: Record<CommandCategory, string> = {
  navigate: 'Navigate',
  action: 'Actions',
  project: 'Projects',
  deploy: 'Deployments',
};

const CATEGORY_ORDER: CommandCategory[] = ['navigate', 'action', 'project', 'deploy'];

export function CommandPalette({ open, onOpenChange, commands }: CommandPaletteProps) {
  const grouped = CATEGORY_ORDER.map((cat) => ({
    category: cat,
    items: commands.filter((c) => c.category === cat),
  })).filter((g) => g.items.length > 0);

  const handleSelect = (cmd: CommandItem) => {
    try {
      cmd.perform();
    } catch (err) {
      // EC-11: contain perform errors — palette stability before user
      // satisfaction; error reported but UI doesn't crash.
      reportError(err as Error, { routePath: `command:${cmd.id}` });
    } finally {
      onOpenChange(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-lg p-0 gap-0 overflow-hidden">
        <Dialog.Title className="sr-only">Command palette</Dialog.Title>
        <Dialog.Description className="sr-only">
          Search commands and navigate the dashboard with the keyboard
        </Dialog.Description>
        <Command
          label="Command Menu"
          className="bg-background"
        >
          <Command.Input
            placeholder="Type a command or search…"
            autoFocus
            className="w-full bg-transparent border-0 border-b border-border/40 px-4 py-3 text-body-sm placeholder:text-muted-foreground focus:outline-none"
          />
          <Command.List className="max-h-80 overflow-y-auto p-1">
            <Command.Empty className="px-4 py-6 text-center text-body-sm text-muted-foreground">
              No commands match.
            </Command.Empty>
            {grouped.map(({ category, items }) => (
              <Command.Group
                key={category}
                heading={CATEGORY_LABEL[category]}
                className="px-1 py-2 [&_[cmdk-group-heading]]:text-label [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:text-muted-foreground [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1"
              >
                {items.map((cmd) => {
                  const Icon = cmd.icon;
                  return (
                    <Command.Item
                      key={cmd.id}
                      value={`${cmd.label} ${(cmd.keywords ?? []).join(' ')}`}
                      onSelect={() => handleSelect(cmd)}
                      className="flex items-center gap-2 px-3 py-2 rounded-md cursor-pointer text-body-sm aria-selected:bg-muted/50"
                    >
                      {Icon ? <Icon className="size-4 text-muted-foreground" /> : null}
                      <span className="flex-1">{cmd.label}</span>
                      {cmd.shortcut ? (
                        <kbd className="text-label text-muted-foreground font-mono">
                          {cmd.shortcut}
                        </kbd>
                      ) : null}
                    </Command.Item>
                  );
                })}
              </Command.Group>
            ))}
          </Command.List>
          <div className="border-t border-border/40 px-4 py-2 text-label text-muted-foreground flex items-center gap-3">
            <span>↑↓ navigate</span>
            <span>↵ select</span>
            <span>esc close</span>
          </div>
        </Command>
      </Dialog.Content>
    </Dialog>
  );
}
