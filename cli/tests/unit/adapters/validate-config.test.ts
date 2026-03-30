// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { validateConfig } from '../../../src/adapters/validate-config';
import { LoopZeroYaml } from '../../../src/adapters/loopzero-yaml';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';

function tmpDir(): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-test-'));
}

describe('validateConfig', () => {
  it('should pass for valid config with existing paths', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'apps', 'api'), { recursive: true });
    const config: LoopZeroYaml = {
      version: 1,
      project: 'test',
      apps: {
        api: { path: 'apps/api', framework: 'express', type: 'server', port: 3000 },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors).toHaveLength(0);
  });

  it('should error when path does not exist', () => {
    const dir = tmpDir();
    const config: LoopZeroYaml = {
      project: 'test',
      apps: {
        api: { path: 'apps/nonexistent', framework: 'express', port: 3000 },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors).toHaveLength(1);
    expect(errors[0].message).toContain("path 'apps/nonexistent' does not exist");
  });

  it('should error when server has no port', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
    const config: LoopZeroYaml = {
      project: 'test',
      apps: {
        api: { path: 'api', framework: 'express', type: 'server', port: null },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors.some(e => e.message.includes("requires port"))).toBe(true);
  });

  it('should error when worker has port', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'worker'), { recursive: true });
    const config: LoopZeroYaml = {
      project: 'test',
      apps: {
        worker: { path: 'worker', framework: 'node', type: 'worker', port: 3000 },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors.some(e => e.message.includes("should not have a port"))).toBe(true);
  });

  it('should pass for frontend without port', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'web'), { recursive: true });
    const config: LoopZeroYaml = {
      project: 'test',
      apps: {
        web: { path: 'web', framework: 'nextjs', type: 'frontend', port: null, render: 'static' },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors).toHaveLength(0);
  });

  it('should error when dockerfile build but Dockerfile missing', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
    const config: LoopZeroYaml = {
      project: 'test',
      apps: {
        api: { path: 'api', framework: 'custom', type: 'server', port: 3000, build: 'dockerfile' },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors.some(e => e.message.includes("Dockerfile not found"))).toBe(true);
  });

  it('should pass when dockerfile build and Dockerfile exists', () => {
    const dir = tmpDir();
    const apiDir = path.join(dir, 'api');
    fs.mkdirSync(apiDir, { recursive: true });
    fs.writeFileSync(path.join(apiDir, 'Dockerfile'), 'FROM alpine');
    const config: LoopZeroYaml = {
      project: 'test',
      apps: {
        api: { path: 'api', framework: 'custom', type: 'server', port: 3000, build: 'dockerfile' },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors).toHaveLength(0);
  });

  it('should error for invalid env key', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
    const config: LoopZeroYaml = {
      project: 'test',
      apps: {
        api: {
          path: 'api', framework: 'express', type: 'server', port: 3000,
          env: { 'invalid-key': 'value' },
        },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors.some(e => e.message.includes("uppercase"))).toBe(true);
  });

  it('should warn for unknown fields', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
    const config: LoopZeroYaml = {
      project: 'test',
      apps: {
        api: {
          path: 'api', framework: 'express', type: 'server', port: 3000,
          replicas: 3,  // unknown field
        } as unknown as LoopZeroYaml['apps'][string],
      },
    };
    const { warnings } = validateConfig(config, dir);
    expect(warnings.some(w => w.includes("Unknown field 'apps.api.replicas'"))).toBe(true);
  });

  it('should error for unsupported version', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
    const config: LoopZeroYaml = {
      version: 99,
      project: 'test',
      apps: {
        api: { path: 'api', framework: 'express', type: 'server', port: 3000 },
      },
    };
    const { errors } = validateConfig(config, dir);
    expect(errors.some(e => e.field === 'version')).toBe(true);
  });
});
