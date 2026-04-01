import { SecretSetCommand, SecretListCommand, SecretDeleteCommand, SecretDeps } from '../../../src/commands/secret';

function createMockDeps(overrides: Partial<SecretDeps> = {}): SecretDeps {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/project/loopzero.yaml',
      content: { project: 'my-app', apps: { api: { path: '.', framework: 'express', port: 3000 } } },
    }),
    setSecret: jest.fn().mockResolvedValue({
      name: 'DATABASE_URL',
      environment: 'production',
      app: 'api',
      created: true,
    }),
    listSecrets: jest.fn().mockResolvedValue({
      secrets: [
        { name: 'DATABASE_URL', environment: 'production', app: 'api' },
        { name: 'REDIS_URL', environment: 'production', app: 'api' },
      ],
    }),
    deleteSecret: jest.fn().mockResolvedValue({ name: 'DATABASE_URL', deleted: true }),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('SecretSetCommand', () => {
  it('should set secret and return exit 0, value NOT in output', async () => {
    const deps = createMockDeps();
    const cmd = new SecretSetCommand(deps);

    const exitCode = await cmd.execute({ nameValue: 'DATABASE_URL=postgres://secret' });

    expect(exitCode).toBe(0);
    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
    expect(output).toContain('DATABASE_URL');
    expect(output).not.toContain('postgres://secret');
    expect(output).not.toContain('secret');
  });

  it('should return exit 1 without = separator', async () => {
    const deps = createMockDeps();
    const cmd = new SecretSetCommand(deps);

    const exitCode = await cmd.execute({ nameValue: 'NOEQUALS' });

    expect(exitCode).toBe(1);
  });

  it('should return exit 1 for lowercase name', async () => {
    const deps = createMockDeps();
    const cmd = new SecretSetCommand(deps);

    const exitCode = await cmd.execute({ nameValue: 'lowercase=value' });

    expect(exitCode).toBe(1);
    const error = (deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
    expect(error).toContain('uppercase');
  });

  it('should return exit 4 without auth', async () => {
    const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
    const cmd = new SecretSetCommand(deps);

    expect(await cmd.execute({ nameValue: 'KEY=val' })).toBe(4);
  });

  it('should return exit 1 without loopzero.yaml', async () => {
    const deps = createMockDeps({ findLoopZeroYaml: jest.fn().mockReturnValue(null) });
    const cmd = new SecretSetCommand(deps);

    expect(await cmd.execute({ nameValue: 'KEY=val' })).toBe(1);
  });

  it('--json should return schema without value', async () => {
    const deps = createMockDeps();
    const cmd = new SecretSetCommand(deps);

    await cmd.execute({ nameValue: 'DB_URL=postgres://secret', json: true });

    const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
    const parsed = JSON.parse(output);
    expect(parsed.name).toBe('DATABASE_URL');
    expect(parsed.created).toBe(true);
    expect(parsed.value).toBeUndefined();
    expect(output).not.toContain('postgres');
  });
});

describe('SecretListCommand', () => {
  it('should list secret names and return exit 0', async () => {
    const deps = createMockDeps();
    const cmd = new SecretListCommand(deps);

    const exitCode = await cmd.execute({});

    expect(exitCode).toBe(0);
    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
    expect(output).toContain('DATABASE_URL');
    expect(output).toContain('REDIS_URL');
  });

  it('--json should return correct schema', async () => {
    const deps = createMockDeps();
    const cmd = new SecretListCommand(deps);

    await cmd.execute({ json: true });

    const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
    const parsed = JSON.parse(output);
    expect(parsed.secrets).toHaveLength(2);
    expect(parsed.secrets[0].name).toBe('DATABASE_URL');
  });

  it('should return exit 4 without auth', async () => {
    const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
    const cmd = new SecretListCommand(deps);

    expect(await cmd.execute({})).toBe(4);
  });
});

describe('SecretDeleteCommand', () => {
  it('should delete secret and return exit 0', async () => {
    const deps = createMockDeps();
    const cmd = new SecretDeleteCommand(deps);

    const exitCode = await cmd.execute({ name: 'DATABASE_URL' });

    expect(exitCode).toBe(0);
    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('\n');
    expect(output).toContain('DATABASE_URL');
    expect(output).toContain('deleted');
  });

  it('should return exit 4 without auth', async () => {
    const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
    const cmd = new SecretDeleteCommand(deps);

    expect(await cmd.execute({ name: 'KEY' })).toBe(4);
  });
});

describe('no internal terms', () => {
  it('should not contain forbidden terms in any output', async () => {
    const forbidden = [
      'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
      'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
      'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      'Loki', 'Promtail', 'LogQL', 'ExternalSecret',
    ];

    const deps1 = createMockDeps();
    await new SecretSetCommand(deps1).execute({ nameValue: 'KEY=val' });
    const deps2 = createMockDeps();
    await new SecretListCommand(deps2).execute({});
    const deps3 = createMockDeps();
    await new SecretDeleteCommand(deps3).execute({ name: 'KEY' });
    const deps4 = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
    await new SecretSetCommand(deps4).execute({ nameValue: 'KEY=val' });

    for (const deps of [deps1, deps2, deps3, deps4]) {
      const all = [
        ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
        ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
      ].join('\n');
      for (const term of forbidden) {
        expect(all).not.toContain(term);
      }
    }
  });

  // F-02: --json without --yes should require confirmation
  it('secret delete with --json but no --yes should return exit 1 CONFIRMATION_REQUIRED', async () => {
    const deps = createMockDeps();
    const cmd = new SecretDeleteCommand(deps);

    const exitCode = await cmd.execute({ name: 'KEY', json: true });

    expect(exitCode).toBe(1);
    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
    expect(output).toContain('CONFIRMATION_REQUIRED');
  });

  it('secret delete with --json and --yes should proceed', async () => {
    const deps = createMockDeps();
    const cmd = new SecretDeleteCommand(deps);

    const exitCode = await cmd.execute({ name: 'KEY', json: true, yes: true });

    expect(exitCode).toBe(0);
    expect(deps.deleteSecret).toHaveBeenCalled();
  });

  it('value should NEVER appear in writeTo or writeError calls', async () => {
    const secretValue = 'super-secret-db-connection-string';
    const deps = createMockDeps();
    const cmd = new SecretSetCommand(deps);

    await cmd.execute({ nameValue: `DB_URL=${secretValue}` });

    const allOutput = [
      ...(deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]),
      ...(deps.writeError as jest.Mock).mock.calls.map((c: string[]) => c[0]),
    ].join('\n');

    expect(allOutput).not.toContain(secretValue);
  });
});
