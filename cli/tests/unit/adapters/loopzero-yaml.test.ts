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

  // ---------------------------------------------------------------------------
  // Schema v2 fields
  // ---------------------------------------------------------------------------

  it('should parse version field', () => {
    const yaml = `version: 1\nproject: my-app\napps:\n  api:\n    path: .\n    port: 3000\n`;
    const result = parseLoopZeroYaml(yaml);
    expect(result.version).toBe(1);
  });

  it('should parse ignore field', () => {
    const yaml = [
      'version: 1',
      'project: my-app',
      'ignore:',
      '  - node_modules',
      '  - .git',
      'apps:',
      '  api:',
      '    path: .',
      '    port: 3000',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);
    expect(result.ignore).toEqual(['node_modules', '.git']);
  });

  it('should parse build strategy', () => {
    const yaml = `version: 1\nproject: my-app\napps:\n  api:\n    path: .\n    port: 3000\n    build: dockerfile\n`;
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.build).toBe('dockerfile');
  });

  it('should parse dockerfile field', () => {
    const yaml = `version: 1\nproject: my-app\napps:\n  api:\n    path: .\n    port: 3000\n    build: dockerfile\n    dockerfile: Dockerfile.prod\n`;
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.dockerfile).toBe('Dockerfile.prod');
  });

  it('should parse env field', () => {
    const yaml = [
      'version: 1',
      'project: my-app',
      'apps:',
      '  api:',
      '    path: .',
      '    port: 3000',
      '    env:',
      '      NODE_ENV: production',
      '      LOG_LEVEL: info',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.env).toEqual({ NODE_ENV: 'production', LOG_LEVEL: 'info' });
  });

  it('should reject invalid build strategy', () => {
    const yaml = `version: 1\nproject: my-app\napps:\n  api:\n    path: .\n    port: 3000\n    build: invalid\n`;
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.build).toBeUndefined(); // invalid values are ignored
  });

  it('should roundtrip all v2 fields', () => {
    const yaml = [
      'version: 1',
      'project: roundtrip-test',
      'ignore:',
      '  - "*.tmp"',
      'apps:',
      '  api:',
      '    path: apps/api',
      '    framework: express',
      '    type: server',
      '    port: 3000',
      '    build: buildpack',
      '    env:',
      '      NODE_ENV: production',
      '    secrets:',
      '      - DATABASE_URL',
      '  worker:',
      '    path: apps/worker',
      '    framework: node',
      '    type: worker',
      '    port: null',
      '    build: dockerfile',
      '    dockerfile: Dockerfile.worker',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);

    expect(result.version).toBe(1);
    expect(result.project).toBe('roundtrip-test');
    expect(result.ignore).toEqual(['*.tmp']);
    expect(result.apps.api.build).toBe('buildpack');
    expect(result.apps.api.env).toEqual({ NODE_ENV: 'production' });
    expect(result.apps.api.secrets).toEqual(['DATABASE_URL']);
    expect(result.apps.worker.build).toBe('dockerfile');
    expect(result.apps.worker.dockerfile).toBe('Dockerfile.worker');
    expect(result.apps.worker.type).toBe('worker');
  });

  it('should handle legacy yaml without v2 fields', () => {
    const yaml = `project: legacy\napps:\n  api:\n    path: .\n    port: 3000\n`;
    const result = parseLoopZeroYaml(yaml);
    expect(result.version).toBeUndefined();
    expect(result.ignore).toBeUndefined();
    expect(result.apps.api.build).toBeUndefined();
    expect(result.apps.api.env).toBeUndefined();
  });

  // ---------------------------------------------------------------------------
  // Scaling fields
  // ---------------------------------------------------------------------------

  it('should parse replicas field', () => {
    const yaml = [
      'project: my-app',
      'apps:',
      '  api:',
      '    path: .',
      '    port: 3000',
      '    replicas: 3',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.replicas).toBe(3);
  });

  it('should ignore replicas less than 1', () => {
    const yaml = [
      'project: my-app',
      'apps:',
      '  api:',
      '    path: .',
      '    port: 3000',
      '    replicas: 0',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.replicas).toBeUndefined();
  });

  it('should parse autoscaling configuration', () => {
    const yaml = [
      'project: my-app',
      'apps:',
      '  api:',
      '    path: .',
      '    port: 3000',
      '    autoscaling:',
      '      min: 2',
      '      max: 10',
      '      targetCPU: 75',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.autoscaling).toEqual({
      min: 2,
      max: 10,
      targetCPU: 75,
    });
  });

  it('should parse partial autoscaling (only min)', () => {
    const yaml = [
      'project: my-app',
      'apps:',
      '  api:',
      '    path: .',
      '    port: 3000',
      '    autoscaling:',
      '      min: 2',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.autoscaling).toBeDefined();
    expect(result.apps.api.autoscaling!.min).toBe(2);
    expect(result.apps.api.autoscaling!.max).toBeUndefined();
    expect(result.apps.api.autoscaling!.targetCPU).toBeUndefined();
  });

  it('should reject targetCPU outside 1-100 range', () => {
    const yaml = [
      'project: my-app',
      'apps:',
      '  api:',
      '    path: .',
      '    port: 3000',
      '    autoscaling:',
      '      targetCPU: 150',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.autoscaling).toBeDefined();
    expect(result.apps.api.autoscaling!.targetCPU).toBeUndefined();
  });

  it('should not include autoscaling when not specified', () => {
    const yaml = [
      'project: my-app',
      'apps:',
      '  api:',
      '    path: .',
      '    port: 3000',
    ].join('\n');
    const result = parseLoopZeroYaml(yaml);
    expect(result.apps.api.autoscaling).toBeUndefined();
    expect(result.apps.api.replicas).toBeUndefined();
  });
});
