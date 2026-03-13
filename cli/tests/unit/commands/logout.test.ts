import { LogoutCommand, LogoutDeps } from '../../../src/commands/logout';

function createMockDeps(overrides: Partial<LogoutDeps> = {}): LogoutDeps {
  return {
    clearToken: jest.fn(),
    writeTo: jest.fn(),
    ...overrides,
  };
}

describe('LogoutCommand', () => {
  it('should clear token and return exit code 0', async () => {
    const deps = createMockDeps();
    const cmd = new LogoutCommand(deps);

    const exitCode = await cmd.execute();

    expect(deps.clearToken).toHaveBeenCalled();
    expect(exitCode).toBe(0);
  });

  it('should output human-readable confirmation', async () => {
    const deps = createMockDeps();
    const cmd = new LogoutCommand(deps);

    await cmd.execute();

    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
    expect(output).toContain('Logged out');
  });

  it('should output JSON when json flag is set', async () => {
    const deps = createMockDeps();
    const cmd = new LogoutCommand(deps);

    await cmd.execute({ json: true });

    const output = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join('');
    const parsed = JSON.parse(output);
    expect(parsed.authenticated).toBe(false);
  });

  it('should not throw if no token exists', async () => {
    const deps = createMockDeps({
      clearToken: jest.fn(), // no-op, no error
    });
    const cmd = new LogoutCommand(deps);

    const exitCode = await cmd.execute();
    expect(exitCode).toBe(0);
  });
});
