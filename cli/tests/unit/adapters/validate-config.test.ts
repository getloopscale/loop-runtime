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

  // B15 (MEET-2026-04-23-059): `replicas` and `autoscaling` are now known.
  // A truly unknown field (e.g. `foo`) still produces the warning.
  it('should warn for unknown fields', () => {
    const dir = tmpDir();
    fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
    const config = {
      project: 'test',
      apps: {
        api: {
          path: 'api', framework: 'express', type: 'server', port: 3000,
          foo: 'bar',  // genuinely unknown field
        },
      },
    } as unknown as LoopZeroYaml;
    const { warnings } = validateConfig(config, dir);
    expect(warnings.some(w => w.includes("Unknown field 'apps.api.foo'"))).toBe(true);
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
          api: { path: 'api', framework: 'cobol', type: 'server', port: 4567 },
        },
      };
      const { errors } = validateConfig(config, dir);
      expect(errors.some(e => e.message.includes("unknown framework 'cobol'"))).toBe(true);
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

    it('should accept Go, Python, Ruby, Java, PHP, Rust frameworks', () => {
      for (const fw of ['go', 'gin', 'fastapi', 'django', 'python', 'sinatra', 'rails', 'ruby', 'spring', 'spring-boot', 'java', 'laravel', 'slim', 'php', 'axum', 'actix', 'rocket', 'rust']) {
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
      const { errors } = validateConfig(config, dir);
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

  // ============================================================
  // Edge cases: bypass attempts
  // ============================================================

  describe('bypass attempts', () => {
    it('should allow type: server on a frontend framework (user wants container, not CDN)', () => {
      // User explicitly sets type: server on a React app — they want SSR/container
      // This is valid: user overrides detection. But render stays "server" (container).
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          web: { path: '.', framework: 'react', type: 'server', port: 3000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      // Should pass — user explicitly chose server type
      const typeErrors = errors.filter(e => e.field.includes('type') || e.field.includes('render'));
      expect(typeErrors).toHaveLength(0);
    });

    it('should count omitted type as server (default) for pod limit', () => {
      // 3 apps without type — all default to server → exceeds limit
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', port: 3000 },
          gateway: { path: '.', framework: 'express', port: 4000 },
          admin: { path: '.', framework: 'express', port: 5000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.message.includes('3 API services'))).toBe(true);
    });

    it('should treat empty string framework as custom', () => {
      const dir = tmpDir();
      fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', framework: '' as any, type: 'server', port: 8080 },
        },
      };
      const { errors } = validateConfig(config, dir);
      // Empty framework → treated as custom → needs Dockerfile
      expect(errors.some(e => e.message.includes('Dockerfile'))).toBe(true);
    });

    it('should treat omitted framework as custom', () => {
      const dir = tmpDir();
      fs.mkdirSync(path.join(dir, 'api'), { recursive: true });
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: 'api', type: 'server', port: 8080 } as any,
        },
      };
      const { errors } = validateConfig(config, dir);
      // No framework → custom → needs Dockerfile
      expect(errors.some(e => e.message.includes('Dockerfile'))).toBe(true);
    });

    it('should reject port: 0 on server', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 0 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.message.includes('requires port'))).toBe(true);
    });

    it('should reject port: -1 on server', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: -1 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.message.includes('requires port'))).toBe(true);
    });

    it('should allow type: frontend on Go framework (unusual but valid user choice)', () => {
      // User wants to serve Go output as static files (e.g., WASM)
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          wasm: { path: '.', framework: 'go', type: 'frontend', port: null },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      // Should pass — user explicitly chose frontend
      const typeErrors = errors.filter(e => e.field.includes('type'));
      expect(typeErrors).toHaveLength(0);
    });

    it('should silently ignore render: static on server type (normalizeApp forces server)', () => {
      // User tries to set render: static on a server app — normalizeApp ignores it
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000, render: 'static' as any },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      // Should not error — render is ignored for server type
      const renderErrors = errors.filter(e => e.field.includes('render'));
      expect(renderErrors).toHaveLength(0);
    });

    it('should handle path traversal attempt (validated server-side by SEC-3)', () => {
      // Path traversal is blocked by api/internal/validation/path.go (SEC-3), not CLI.
      // CLI only checks if path exists. ../../etc resolves to /etc which exists.
      // This test documents the defense-in-depth: CLI doesn't crash, server rejects.
      const dir = tmpDir();
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '../../etc', framework: 'express', type: 'server', port: 3000 },
        },
      };
      // Should not crash — path validation is best-effort in CLI
      const { errors } = validateConfig(config, dir);
      expect(errors).toBeDefined(); // no crash
    });

    it('should handle app with no path (defaults to .)', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '', framework: 'express', type: 'server', port: 3000 } as any,
        },
      };
      // Empty path should not crash
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      // No crash = pass (path validation is skipped with skipFs)
      expect(errors.filter(e => e.field.includes('path'))).toHaveLength(0);
    });

    it('should handle type: frontend + framework: express (user mismatch)', () => {
      // User sets frontend type but uses a server framework
      // This is allowed — user knows what they're doing (maybe serving static from express build)
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          web: { path: '.', framework: 'express', type: 'frontend', port: null },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      // No type error — user explicitly chose frontend
      const typeErrors = errors.filter(e => e.field.includes('type'));
      expect(typeErrors).toHaveLength(0);
    });
  });

  describe('app name RFC 1123 validation', () => {
    it('should error on app name starting with a digit', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          '1api': { path: '.', framework: 'express', type: 'server', port: 3000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.field === 'apps.1api' && e.message.includes('invalid'))).toBe(true);
    });

    it('should error on app name with uppercase letters', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          MyApp: { path: '.', framework: 'express', type: 'server', port: 3000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.field === 'apps.MyApp' && e.message.includes('invalid'))).toBe(true);
    });

    it('should error on app name ending with a hyphen', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          'api-': { path: '.', framework: 'express', type: 'server', port: 3000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.field === 'apps.api-' && e.message.includes('invalid'))).toBe(true);
    });

    it('should error on app name with underscores', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          my_app: { path: '.', framework: 'express', type: 'server', port: 3000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.field === 'apps.my_app' && e.message.includes('invalid'))).toBe(true);
    });

    it('should accept valid app names', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000 },
          web: { path: '.', framework: 'vite', type: 'frontend', port: null },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const nameErrors = errors.filter(e => e.message.includes('invalid'));
      expect(nameErrors).toHaveLength(0);
    });

    it('should error on app name exceeding 63 characters', () => {
      const longName = 'a' + 'b'.repeat(63);
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          [longName]: { path: '.', framework: 'express', type: 'server', port: 3000 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.some(e => e.field === `apps.${longName}` && e.message.includes('invalid'))).toBe(true);
    });
  });

  describe('error message quality', () => {
    it('unknown framework error should list all supported frameworks', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'fortran', type: 'server', port: 3000 },
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

  // B15 (CHG-2026-04-23-B15, MEET-2026-04-23-059): replicas + autoscaling
  // are valid known fields; invalid values must be rejected with actionable errors.
  describe('B15: replicas and autoscaling scaling fields', () => {
    it('accepts replicas: 2 without any warning or error', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000, replicas: 2 },
        },
      };
      const { errors, warnings } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors).toHaveLength(0);
      expect(warnings.find(w => w.includes('replicas'))).toBeUndefined();
    });

    it('accepts autoscaling: {min:1, max:3} without warning', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 1, max: 3, targetCPU: 70 },
          },
        },
      };
      const { errors, warnings } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors).toHaveLength(0);
      expect(warnings.find(w => w.includes('autoscaling'))).toBeUndefined();
    });

    it('rejects replicas: 0 with exit-1 error', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000, replicas: 0 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const repErr = errors.find(e => e.field.includes('replicas'));
      expect(repErr).toBeDefined();
      expect(repErr!.message).toMatch(/replicas/);
      expect(repErr!.message).toMatch(/1/);
    });

    it('rejects replicas: 101 (over service cap)', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: { path: '.', framework: 'express', type: 'server', port: 3000, replicas: 101 },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const repErr = errors.find(e => e.field.includes('replicas'));
      expect(repErr).toBeDefined();
      expect(repErr!.message).toMatch(/100|exceed/i);
    });

    it('rejects autoscaling.max < autoscaling.min', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 5, max: 2 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const asErr = errors.find(e => e.field.includes('autoscaling'));
      expect(asErr).toBeDefined();
      expect(asErr!.message).toMatch(/max/);
      expect(asErr!.message).toMatch(/min/);
    });

    it('rejects autoscaling.targetCPU: 0', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 1, max: 3, targetCPU: 0 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.find(e => e.field.includes('targetCPU'))).toBeDefined();
    });

    it('rejects autoscaling.targetCPU: 101', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 1, max: 3, targetCPU: 101 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(errors.find(e => e.field.includes('targetCPU'))).toBeDefined();
    });

    it('rejects both replicas AND autoscaling set (mutual exclusion)', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            replicas: 2,
            autoscaling: { min: 1, max: 3 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      // Must cite BOTH field names so user knows what to remove.
      const mutualErr = errors.find(
        e => e.message.includes('replicas') && e.message.includes('autoscaling')
      );
      expect(mutualErr).toBeDefined();
    });

    it('still warns on genuinely unknown fields (regression)', () => {
      const config = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            foo: 'bar', // genuinely unknown
          },
        },
      } as unknown as LoopZeroYaml;
      const { warnings } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      expect(warnings.find(w => w.includes('apps.api.foo'))).toBeDefined();
    });
  });

  // KEDA scale-to-zero validation
  describe('scale-to-zero (KEDA)', () => {
    it('accepts autoscaling.min: 0 for server apps', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 0, max: 5, targetCPU: 80 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const minErr = errors.find(e => e.field.includes('autoscaling.min'));
      expect(minErr).toBeUndefined();
    });

    it('accepts autoscaling.min: 0 for frontend apps', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          web: {
            path: '.',
            framework: 'react',
            type: 'frontend',
            port: 80,
            autoscaling: { min: 0, max: 3, targetCPU: 80 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const minErr = errors.find(e => e.field.includes('autoscaling.min'));
      expect(minErr).toBeUndefined();
    });

    it('rejects autoscaling.min: 0 for workers (no HTTP wake-up)', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          processor: {
            path: '.',
            framework: 'express',
            type: 'worker',
            port: null,
            autoscaling: { min: 0, max: 5, targetCPU: 80 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const minErr = errors.find(e => e.field.includes('autoscaling.min'));
      expect(minErr).toBeDefined();
      expect(minErr!.message).toContain('scale-to-zero');
      expect(minErr!.message).toContain('worker');
    });

    it('rejects autoscaling.min: -1', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: -1, max: 5, targetCPU: 80 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const minErr = errors.find(e => e.field.includes('autoscaling.min'));
      expect(minErr).toBeDefined();
    });

    it('accepts cooldown: 300 (valid)', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 0, max: 5, targetCPU: 80, cooldown: 300 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const cdErr = errors.find(e => e.field.includes('cooldown'));
      expect(cdErr).toBeUndefined();
    });

    it('rejects cooldown: 10 (below minimum 30)', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 0, max: 5, targetCPU: 80, cooldown: 10 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const cdErr = errors.find(e => e.field.includes('cooldown'));
      expect(cdErr).toBeDefined();
      expect(cdErr!.message).toContain('30');
      expect(cdErr!.message).toContain('3600');
    });

    it('rejects cooldown: 5000 (above maximum 3600)', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 0, max: 5, targetCPU: 80, cooldown: 5000 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const cdErr = errors.find(e => e.field.includes('cooldown'));
      expect(cdErr).toBeDefined();
    });

    it('accepts missing cooldown (defaults to 300 server-side)', () => {
      const config: LoopZeroYaml = {
        project: 'test',
        apps: {
          api: {
            path: '.',
            framework: 'express',
            type: 'server',
            port: 3000,
            autoscaling: { min: 0, max: 5, targetCPU: 80 },
          },
        },
      };
      const { errors } = validateConfig(config, '/tmp', { skipFilesystemChecks: true });
      const cdErr = errors.find(e => e.field.includes('cooldown'));
      expect(cdErr).toBeUndefined();
    });
  });
});
