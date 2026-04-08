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

  // ============================================================
  // V1: Framework Recognition
  // ============================================================

  describe('framework validation', () => {
    it('should ERROR on unknown framework (not just warning)', () => {
      const dir = tmpDir();
      fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', framework: 'sinatra', type: 'server', port: 4567 },
        },
      };
      const { errors, warnings } = validateConfig(config, dir);
      expect(errors.some(e => e.message.includes("unknown framework 'sinatra'"))).toBe(true);
      expect(errors.some(e => e.message.includes('Supported:'))).toBe(true);
      expect(errors.some(e => e.message.includes("framework: custom"))).toBe(true);
    });

    it('should accept all known Node.js frameworks', () => {
      const nodeFrameworks = ['express', 'nestjs', 'fastify', 'nextjs', 'next', 'vite', 'remix', 'node', 'react', 'vue', 'svelte', 'astro', 'angular'];
      for (const fw of nodeFrameworks) {
        const config: LoopZeroYaml = {
          project: 'test',
          apps: {
            app: { path: '.', framework: fw, type: 'server', port: 3000 },
          },
        };
        const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
        const fwErrors = errors.filter(e => e.field.includes('framework'));
        expect(fwErrors).toHaveLength(0);
      }
    });

    it('should accept Go and Python frameworks', () => {
      for (const fw of ['go', 'gin', 'fastapi', 'django', 'python']) {
        const config: LoopZeroYaml = {
          project: 'test',
          apps: {
            app: { path: '.', framework: fw, type: 'server', port: 8080 },
          },
        };
        const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
        const fwErrors = errors.filter(e => e.field.includes('framework'));
        expect(fwErrors).toHaveLength(0);
      }
    });

    it('should accept framework: custom (with Dockerfile)', () => {
      const dir = tmpDir();
      const apiDir = path.join(dir, 'api');
      fs.mkdirSync(apiDir, { recursive: true });
      fs.writeFileSync(path.join(apiDir, 'Dockerfile'), 'FROM alpine');
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', framework: 'custom', type: 'server', port: 8080 },
        },
      };
      const { errors } = validateConfig(config, dir);
      expect(errors).toHaveLength(0);
    });
  });

  // ============================================================
  // V2: Frontend = static (auto-infer)
  // ============================================================

  describe('frontend rendering validation', () => {
    it('should accept frontend without explicit render (auto-inferred as static)', () => {
      const dir = tmpDir();
      fs.mkdirSync(path.join(dir, 'web'), { recursive: true });
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          web: { path: 'web', framework: 'vite', type: 'frontend', port: null },
        },
      };
      const { errors } = validateConfig(config, dir);
      // Should NOT error — render: static is auto-inferred in normalizeApp
      const renderErrors = errors.filter(e => e.field.includes('render'));
      expect(renderErrors).toHaveLength(0);
    });

    it('should ERROR when frontend explicitly sets render: server', () => {
      const dir = tmpDir();
      fs.mkdirSync(path.join(dir, 'web'), { recursive: true });
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          web: { path: 'web', framework: 'react', type: 'frontend', port: null, render: 'server' },
        },
      };
      const { errors } = validateConfig(config, dir);
      expect(errors.some(e => e.message.includes('always static'))).toBe(true);
    });

    it('should accept frontend with explicit render: static', () => {
      const dir = tmpDir();
      fs.mkdirSync(path.join(dir, 'web'), { recursive: true });
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          web: { path: 'web', framework: 'vite', type: 'frontend', port: null, render: 'static' },
        },
      };
      const { errors } = validateConfig(config, dir);
      const renderErrors = errors.filter(e => e.field.includes('render'));
      expect(renderErrors).toHaveLength(0);
    });
  });

  // ============================================================
  // V3: Max pods validation
  // ============================================================

  describe('pod limit validation', () => {
    it('should allow 1 server + 1 worker + N frontends', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000 },
          worker: { path: '.', framework: 'node', type: 'worker', port: null },
          web: { path: '.', framework: 'vite', type: 'frontend', port: null },
          admin: { path: '.', framework: 'react', type: 'frontend', port: null },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const podErrors = errors.filter(e => e.message.includes('API services') || e.message.includes('workers'));
      expect(podErrors).toHaveLength(0);
    });

    it('should ERROR on 2 servers', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000 },
          gateway: { path: '.', framework: 'express', type: 'server', port: 4000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.message.includes('2 API services'))).toBe(true);
    });

    it('should ERROR on 2 workers', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          w1: { path: '.', framework: 'node', type: 'worker', port: null },
          w2: { path: '.', framework: 'node', type: 'worker', port: null },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.message.includes('2 workers'))).toBe(true);
    });

    it('should NOT count frontends as pods', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000 },
          web1: { path: '.', framework: 'vite', type: 'frontend', port: null },
          web2: { path: '.', framework: 'react', type: 'frontend', port: null },
          web3: { path: '.', framework: 'vue', type: 'frontend', port: null },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const podErrors = errors.filter(e => e.message.includes('API services'));
      expect(podErrors).toHaveLength(0);
    });
  });

  // ============================================================
  // V4: Custom framework Dockerfile detection
  // ============================================================

  describe('custom framework Dockerfile validation', () => {
    it('should ERROR when framework: custom without Dockerfile', () => {
      const dir = tmpDir();
      fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
      // No Dockerfile created
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', framework: 'custom', type: 'server', port: 8080 },
        },
      };
      const { errors } = validateConfig(config, dir);
      expect(errors.some(e => e.message.includes("requires a Dockerfile"))).toBe(true);
    });

    it('should pass when framework: custom with Dockerfile present', () => {
      const dir = tmpDir();
      const apiDir = path.join(dir, 'api');
      fs.mkdirSync(apiDir, { recursive: true });
      fs.writeFileSync(path.join(apiDir, 'Dockerfile'), 'FROM golang:1.25');
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', framework: 'custom', type: 'server', port: 8080 },
        },
      };
      const { errors } = validateConfig(config, dir);
      expect(errors).toHaveLength(0);
    });
  });

  // ============================================================
  // V6: Buildpack + user Dockerfile → warning
  // ============================================================

  describe('buildpack with user Dockerfile detection', () => {
    it('should WARN when buildpack app has a Dockerfile in its path', () => {
      const dir = tmpDir();
      const apiDir = path.join(dir, 'api');
      fs.mkdirSync(apiDir, { recursive: true });
      fs.writeFileSync(path.join(apiDir, 'Dockerfile'), 'FROM node:20');
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', framework: 'express', type: 'server', port: 3000, build: 'buildpack' },
        },
      };
      const { warnings } = validateConfig(config, dir);
      expect(warnings.some(w => w.includes("Dockerfile found") && w.includes("build: dockerfile"))).toBe(true);
    });

    it('should NOT warn when build: dockerfile is explicitly set', () => {
      const dir = tmpDir();
      const apiDir = path.join(dir, 'api');
      fs.mkdirSync(apiDir, { recursive: true });
      fs.writeFileSync(path.join(apiDir, 'Dockerfile'), 'FROM node:20');
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', framework: 'express', type: 'server', port: 3000, build: 'dockerfile' },
        },
      };
      const { warnings } = validateConfig(config, dir);
      expect(warnings.some(w => w.includes("Dockerfile found") && w.includes("build: dockerfile"))).toBe(false);
    });
  });

  // ============================================================
  // Real-world scenario: aquim.ai monorepo
  // ============================================================

  describe('real-world: aquim.ai monorepo', () => {
    it('should pass: Python API (dockerfile) + React frontend (auto-static)', () => {
      const dir = tmpDir();
      const apiDir = path.join(dir, 'api');
      const webDir = path.join(dir, 'web');
      fs.mkdirSync(apiDir, { recursive: true });
      fs.mkdirSync(webDir, { recursive: true });
      fs.writeFileSync(path.join(apiDir, 'Dockerfile'), 'FROM python:3.12');
      const config: LoopZeroYaml = {
        project: 'aquim-test',
        apps: {
          api: { path: 'api', framework: 'python', type: 'server', port: 8000, build: 'dockerfile' },
          web: { path: 'web', framework: 'vite', type: 'frontend', port: null },
        },
      };
      const { errors, warnings } = validateConfig(config, dir);
      expect(errors).toHaveLength(0);
    });

    it('should fail: both apps as server (no type: frontend)', () => {
      const config: LoopZeroYaml = {
        project: 'aquim-test',
        apps: {
          api: { path: 'api', framework: 'python', type: 'server', port: 8000 },
          web: { path: 'web', framework: 'vite', type: 'server', port: 3000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.message.includes('2 API services'))).toBe(true);
    });

    it('should fail: unknown framework react without mapping', () => {
      // This test verifies that 'react' IS now a known framework
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          web: { path: '.', framework: 'react', type: 'frontend', port: null },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const fwErrors = errors.filter(e => e.field.includes('framework'));
      expect(fwErrors).toHaveLength(0); // react IS known now
    });
  });

  // ============================================================
  // loopzero-stacks templates validation
  // ============================================================

  describe('loopzero-stacks templates', () => {
    it('monorepo-turbo: api (server) + web (frontend) should pass', () => {
      const config: LoopZeroYaml = {
        project: 'my-app',
        apps: {
          api: { path: 'apps/api', framework: 'express', type: 'server', port: 3001 },
          web: { path: 'apps/web', framework: 'nextjs', type: 'frontend', port: null },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors).toHaveLength(0);
    });

    it('monorepo-python: api (server) + worker should pass', () => {
      const config: LoopZeroYaml = {
        project: 'my-app',
        apps: {
          api: { path: 'apps/api', framework: 'fastapi', type: 'server', port: 8000 },
          worker: { path: 'apps/worker', framework: 'custom', type: 'worker', port: null },
        },
      };
      // Worker with custom framework needs Dockerfile — skip fs check
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors).toHaveLength(0);
    });

    it('single app: python-fastapi should pass', () => {
      const config: LoopZeroYaml = {
        project: 'my-api',
        apps: {
          api: { path: '.', framework: 'fastapi', type: 'server', port: 8000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors).toHaveLength(0);
    });
  });

  // ============================================================
  // Error message quality
  // ============================================================

  describe('error message quality', () => {
    it('unknown framework error should list all supported frameworks', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'rails', type: 'server', port: 3000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const fwError = errors.find(e => e.field.includes('framework'));
      expect(fwError).toBeDefined();
      expect(fwError!.message).toContain('express');
      expect(fwError!.message).toContain('fastapi');
      expect(fwError!.message).toContain('go');
      expect(fwError!.message).toContain("framework: custom");
    });

    it('pod limit error should name the offending apps', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000 },
          gateway: { path: '.', framework: 'node', type: 'server', port: 4000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const podError = errors.find(e => e.message.includes('API services'));
      expect(podError).toBeDefined();
      expect(podError!.message).toContain('api');
      expect(podError!.message).toContain('gateway');
    });

    it('custom Dockerfile error should suggest alternatives', () => {
      const dir = tmpDir();
      fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', framework: 'custom', type: 'server', port: 8080 },
        },
      };
      const { errors } = validateConfig(config, dir);
      const dfError = errors.find(e => e.message.includes('Dockerfile'));
      expect(dfError).toBeDefined();
      expect(dfError!.message).toContain('supported framework');
    });
  });
});
