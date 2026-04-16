// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ValidateCommand, ValidateDeps } from '../../../src/commands/validate';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';

function tmpDir(): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-validate-'));
}

function createDeps(yamlResult: ReturnType<ValidateDeps['findLoopZeroYaml']>): ValidateDeps & { output: string[]; errors: string[] } {
  const output: string[] = [];
  const errors: string[] = [];
  return {
    findLoopZeroYaml: () => yamlResult,
    writeTo: (msg: string) => output.push(msg),
    writeError: (msg: string) => errors.push(msg),
    output,
    errors,
  };
}

describe('ValidateCommand', () => {
  it('should return 0 for valid config', async () => {
    const dir = tmpDir();
    const apiDir = path.join(dir, 'apps', 'api');
    fs.mkdirSync(apiDir, { recursive: true });
    const yamlPath = path.join(dir, 'loopzero.yaml');

    const deps = createDeps({
      path: yamlPath,
      content: {
        version: 1,
        project: 'test',
        apps: {
          api: { path: 'apps/api', framework: 'express', type: 'server', port: 3000 },
        },
      },
    });

    const cmd = new ValidateCommand(deps);
    const code = await cmd.execute({});
    expect(code).toBe(0);
    expect(deps.output.some(o => o.includes('valid'))).toBe(true);
    expect(deps.output.some(o => o.includes('1 app'))).toBe(true);
  });

  it('should return 1 when no loopzero.yaml found', async () => {
    const deps = createDeps(null);
    const cmd = new ValidateCommand(deps);
    const code = await cmd.execute({});
    expect(code).toBe(1);
    expect(deps.errors.some(e => e.includes('loopzero init'))).toBe(true);
  });

  it('should return 1 for invalid config', async () => {
    const dir = tmpDir();
    const yamlPath = path.join(dir, 'loopzero.yaml');

    const deps = createDeps({
      path: yamlPath,
      content: {
        project: 'test',
        apps: {
          api: { path: 'nonexistent', framework: 'express', type: 'server', port: 3000 },
        },
      },
    });

    const cmd = new ValidateCommand(deps);
    const code = await cmd.execute({});
    expect(code).toBe(1);
  });

  it('should output JSON format', async () => {
    const dir = tmpDir();
    const apiDir = path.join(dir, 'apps', 'api');
    fs.mkdirSync(apiDir, { recursive: true });
    const yamlPath = path.join(dir, 'loopzero.yaml');

    const deps = createDeps({
      path: yamlPath,
      content: {
        version: 1,
        project: 'test',
        apps: {
          api: { path: 'apps/api', framework: 'express', type: 'server', port: 3000 },
        },
      },
    });

    const cmd = new ValidateCommand(deps);
    const code = await cmd.execute({ json: true });
    expect(code).toBe(0);

    const parsed = JSON.parse(deps.output[0]);
    expect(parsed.valid).toBe(true);
    expect(parsed.apps).toBe(1);
    expect(parsed.errors).toEqual([]);
  });

  it('should output JSON with errors for invalid config', async () => {
    const dir = tmpDir();
    const yamlPath = path.join(dir, 'loopzero.yaml');

    const deps = createDeps({
      path: yamlPath,
      content: {
        project: 'test',
        apps: {
          api: { path: 'nonexistent', framework: 'express', type: 'server', port: null },
        },
      },
    });

    const cmd = new ValidateCommand(deps);
    const code = await cmd.execute({ json: true });
    expect(code).toBe(1);

    const parsed = JSON.parse(deps.output[0]);
    expect(parsed.valid).toBe(false);
    expect(parsed.errors.length).toBeGreaterThan(0);
  });

  it('should report parse errors', async () => {
    const deps = createDeps({
      path: '/tmp/loopzero.yaml',
      content: { project: '', apps: {} },
      parseError: 'invalid syntax on line 3',
    });

    const cmd = new ValidateCommand(deps);
    const code = await cmd.execute({});
    expect(code).toBe(1);
    expect(deps.errors.some(e => e.includes('invalid syntax'))).toBe(true);
  });
});
