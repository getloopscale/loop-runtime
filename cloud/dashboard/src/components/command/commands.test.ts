import { describe, it, expect, vi } from 'vitest';
import { buildCommands, sortByRecentDeploy, type CommandContext } from './commands';

function makeCtx(overrides: Partial<CommandContext> = {}): CommandContext {
  return {
    navigate: vi.fn(),
    recentProjects: [],
    ...overrides,
  };
}

describe('buildCommands (Sprint 2 T2.3)', () => {
  it('test_12_base_commands_plus_quick_jumps', () => {
    // 8 navigate + 4 action = 12 base; + 3 project quick-jumps if available
    const ctx = makeCtx({
      recentProjects: [
        { name: 'a', last_deploy: '2026-05-27T10:00:00Z' },
        { name: 'b', last_deploy: '2026-05-27T08:00:00Z' },
        { name: 'c', last_deploy: '2026-05-27T06:00:00Z' },
      ],
    });
    const cmds = buildCommands(ctx);
    expect(cmds).toHaveLength(15);
  });

  it('test_each_command_has_unique_id', () => {
    const ctx = makeCtx({
      recentProjects: [
        { name: 'a' },
        { name: 'b' },
        { name: 'c' },
      ],
    });
    const cmds = buildCommands(ctx);
    const ids = new Set(cmds.map((c) => c.id));
    expect(ids.size).toBe(cmds.length);
  });

  it('test_navigate_command_calls_navigate_with_correct_path', () => {
    const navigate = vi.fn();
    const cmds = buildCommands(makeCtx({ navigate }));
    const projectsCmd = cmds.find((c) => c.id === 'nav-projects');
    projectsCmd?.perform();
    expect(navigate).toHaveBeenCalledWith('/projects');
  });

  // EC-03: action commands use URL query param strategy
  it('test_action_create_project_sets_action_query_param', () => {
    const navigate = vi.fn();
    const cmds = buildCommands(makeCtx({ navigate }));
    const create = cmds.find((c) => c.id === 'act-new-project');
    create?.perform();
    expect(navigate).toHaveBeenCalledWith('/projects?action=new');
  });

  it('test_action_invite_member_uses_action_query_param', () => {
    const navigate = vi.fn();
    const cmds = buildCommands(makeCtx({ navigate }));
    cmds.find((c) => c.id === 'act-invite-member')?.perform();
    expect(navigate).toHaveBeenCalledWith('/team?action=invite');
  });

  it('test_action_add_domain_uses_action_query_param', () => {
    const navigate = vi.fn();
    const cmds = buildCommands(makeCtx({ navigate }));
    cmds.find((c) => c.id === 'act-add-domain')?.perform();
    expect(navigate).toHaveBeenCalledWith('/domains?action=add');
  });

  it('test_project_quick_jumps_take_top_3', () => {
    const ctx = makeCtx({
      recentProjects: [
        { name: 'one' }, { name: 'two' }, { name: 'three' }, { name: 'four' }, { name: 'five' },
      ],
    });
    const cmds = buildCommands(ctx);
    const projects = cmds.filter((c) => c.category === 'project');
    expect(projects).toHaveLength(3);
    expect(projects.map((c) => c.label)).toEqual(['one', 'two', 'three']);
  });
});

describe('sortByRecentDeploy (Sprint 2 T2.4 EC-12)', () => {
  it('test_quick_jumps_sort_by_last_deploy_desc', () => {
    const input = [
      { name: 'a', last_deploy: '2026-05-25T10:00:00Z' },
      { name: 'b', last_deploy: '2026-05-27T10:00:00Z' },
      { name: 'c', last_deploy: '2026-05-26T10:00:00Z' },
    ];
    const sorted = sortByRecentDeploy(input);
    expect(sorted.map((p) => p.name)).toEqual(['b', 'c', 'a']);
  });

  it('test_quick_jumps_tiebreaker_by_created_at', () => {
    const input = [
      { name: 'a', last_deploy: undefined, created_at: '2026-05-25T10:00:00Z' },
      { name: 'b', last_deploy: undefined, created_at: '2026-05-27T10:00:00Z' },
      { name: 'c', last_deploy: undefined, created_at: '2026-05-26T10:00:00Z' },
    ];
    const sorted = sortByRecentDeploy(input);
    expect(sorted.map((p) => p.name)).toEqual(['b', 'c', 'a']);
  });
});
