// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { parseLoopZeroYaml } from '../../../src/adapters/loopzero-yaml';

describe('parseLoopZeroYaml', () => {
  it('should parse project name', () => {
    const yaml = `project: my-app\napps:\n  api:\n    path: ./api\n    port: 3000\n`;
    const result = parseLoopZeroYaml(yaml);
    expect(result.project).toBe('my-app');
  });

  it('should parse app with type, framework, port', () => {
    const yaml = `project: my-app\napps:\n  api:\n    type: server\n    framework: nestjs\n    path: apps/api\n    port: 3000\n`;
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api).toEqual({
      type: 'server',
      framework: 'nestjs',
      path: 'apps/api',
      port: 3000,
    });
  });

  it('should parse secrets without leading dash or spaces', () => {
    const yaml = [
      'project: dogfood-saas',
      'apps:',
      '  api:',
      '    type: server',
      '    path: apps/api',
      '    port: 3000',
      '    secrets:',
      '      - DATABASE_URL',
      '      - JWT_SECRET',
      '  worker:',
      '    type: worker',
      '    path: apps/worker',
      '    port: null',
      '    secrets:',
      '      - DATABASE_URL',
      '      - REDIS_URL',
    ].join('\n');

    const result = parseLoopZeroYaml(yaml);

    expect(result.apps.api.secrets).toEqual(['DATABASE_URL', 'JWT_SECRET']);
    expect(result.apps.worker.secrets).toEqual(['DATABASE_URL', 'REDIS_URL']);
  });

  it('should parse multiple apps', () => {
    const yaml = [
      'project: dogfood-saas',
      'apps:',
      '  api:',
      '    type: server',
      '    framework: nestjs',
      '    path: apps/api',
      '    port: 3000',
      '  gateway:',
      '    type: server',
      '    framework: express',
      '    path: apps/gateway',
      '    port: 4000',
      '  worker:',
      '    type: worker',
      '    framework: node',
      '    path: apps/worker',
      '  web:',
      '    type: frontend',
      '    framework: nextjs',
      '    path: apps/web',
    ].join('\n');

    const result = parseLoopZeroYaml(yaml);

    expect(Object.keys(result.apps)).toEqual(['api', 'gateway', 'worker', 'web']);
    expect(result.apps.worker.type).toBe('worker');
    expect(result.apps.web.type).toBe('frontend');
  });

  it('should throw for malformed yaml', () => {
    const yaml = 'this is not valid yaml for loopzero';
    expect(() => parseLoopZeroYaml(yaml)).toThrow('could not parse loopzero.yaml');
  });

  it('should handle app without secrets', () => {
    const yaml = `project: test\napps:\n  api:\n    path: .\n    port: 3000\n`;
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.secrets).toBeUndefined();
  });
});
