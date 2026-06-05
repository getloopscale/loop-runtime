// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { normalizeApp, normalizeApps } from '../../../src/adapters/app-unit';

describe('normalizeApp', () => {
  it('should normalize express server with defaults', () => {
    const unit = normalizeApp('api', {
      path: 'apps/api', framework: 'express', port: 3000,
    });
    expect(unit.name).toBe('api');
    expect(unit.type).toBe('server');
    expect(unit.build).toBe('buildpack');
    expect(unit.port).toBe(3000);
    expect(unit.dockerfile).toBeNull();
    expect(unit.env).toEqual({});
    expect(unit.secrets).toEqual([]);
  });

  // Plan B v3.1 T-B.1 (2026-06-05) — framework-string auto-detect removed;
  // explicit `type: "frontend"` is now the only path to frontend rendering.
  it('respects explicit type: frontend on nextjs framework', () => {
    const unit = normalizeApp('web', {
      path: 'apps/web', framework: 'nextjs', type: 'frontend', port: null,
    });
    expect(unit.type).toBe('frontend');
    expect(unit.build).toBe('buildpack');
  });

  it('should respect explicit worker type', () => {
    const unit = normalizeApp('worker', {
      path: 'apps/worker', framework: 'node', type: 'worker', port: null,
    });
    expect(unit.type).toBe('worker');
    expect(unit.port).toBeNull();
    expect(unit.build).toBe('buildpack');
  });

  it('should resolve dockerfile build strategy', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'custom', port: 3000, build: 'dockerfile', dockerfile: 'Dockerfile.prod',
    });
    expect(unit.build).toBe('dockerfile');
    expect(unit.dockerfile).toBe('Dockerfile.prod');
  });

  it('should default to dockerfile when framework is custom', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'custom', port: 3000,
    });
    expect(unit.build).toBe('dockerfile');
    expect(unit.dockerfile).toBe('Dockerfile');
  });

  it('should default server port to 8080 when missing', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: null,
    });
    expect(unit.port).toBe(8080);
  });

  it('should preserve env and secrets', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: 3000,
      env: { NODE_ENV: 'production' },
      secrets: ['DATABASE_URL'],
    });
    expect(unit.env).toEqual({ NODE_ENV: 'production' });
    expect(unit.secrets).toEqual(['DATABASE_URL']);
  });

  it('should normalize go framework', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'gin', port: 8080,
    });
    expect(unit.type).toBe('server');
    expect(unit.build).toBe('buildpack');
  });

  // V2: Frontend auto-infer render: static
  it('should auto-infer render: static for frontends (no explicit render needed)', () => {
    const unit = normalizeApp('web', {
      path: 'web', framework: 'vite', type: 'frontend', port: null,
    });
    expect(unit.type).toBe('frontend');
    expect(unit.rendering).toBe('static');
  });

  // Plan B v3.1 T-B.1 — framework-string auto-detect removed; explicit type
  // is required for frontend rendering. The previous "detected from framework"
  // path no longer fires (defaults to "server" now).
  it('without explicit type, defaults to server even when framework is "react"', () => {
    const unit = normalizeApp('web', {
      path: 'web', framework: 'react', port: 3000,
    });
    expect(unit.type).toBe('server');
    expect(unit.rendering).toBe('server');
  });

  it('should force render: server for server type regardless of user input', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: 3000, type: 'server',
    });
    expect(unit.rendering).toBe('server');
  });

  it('should force render: server for worker type', () => {
    const unit = normalizeApp('bg', {
      path: '.', framework: 'node', type: 'worker', port: null,
    });
    expect(unit.rendering).toBe('server');
  });

  // Plan B v3.1 T-B.1 (2026-06-05) — framework-string heuristic removed post
  // LoopZeroKit-only cutover. Frontends now declare `type: "frontend"` explicitly
  // em `.loopzero/services.json` (or em the AppSpec migration path). Test
  // updated to assert the explicit-type contract: absent `type` defaults
  // to "server"; explicit `type: "frontend"` still resolves to frontend.
  it('defaults to type: server when type and framework are unspecified', () => {
    const unit = normalizeApp('web', { path: '.', framework: 'nextjs', port: 3000 });
    expect(unit.type).toBe('server');
  });

  it('honors explicit type: frontend regardless of framework string', () => {
    const unit = normalizeApp('web', {
      path: '.',
      framework: 'nextjs',
      type: 'frontend',
      port: null,
    });
    expect(unit.type).toBe('frontend');
    expect(unit.rendering).toBe('static');
  });

  // ---------------------------------------------------------------------------
  // Scaling fields
  // ---------------------------------------------------------------------------

  it('should default replicas to 1 when not specified', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: 3000,
    });
    expect(unit.replicas).toBe(1);
    expect(unit.autoscaling).toBeNull();
  });

  it('should preserve explicit replicas', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: 3000, replicas: 3,
    });
    expect(unit.replicas).toBe(3);
  });

  it('should default replicas to 1 when invalid', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: 3000, replicas: 0,
    });
    expect(unit.replicas).toBe(1);
  });

  it('should normalize autoscaling with defaults', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: 3000,
      autoscaling: { min: 2 },
    });
    expect(unit.autoscaling).toEqual({
      min: 2,
      max: 3,
      targetCPU: 80,
    });
  });

  it('should normalize full autoscaling config', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: 3000,
      autoscaling: { min: 2, max: 10, targetCPU: 75 },
    });
    expect(unit.autoscaling).toEqual({
      min: 2,
      max: 10,
      targetCPU: 75,
    });
  });

  it('should set autoscaling to null when not specified', () => {
    const unit = normalizeApp('api', {
      path: '.', framework: 'express', port: 3000,
    });
    expect(unit.autoscaling).toBeNull();
  });
});

describe('normalizeApps', () => {
  it('should normalize all apps', () => {
    const units = normalizeApps({
      api: { path: 'apps/api', framework: 'express', port: 3000 },
      web: { path: 'apps/web', framework: 'nextjs', port: null },
    });
    expect(units).toHaveLength(2);
    expect(units[0].name).toBe('api');
    expect(units[1].name).toBe('web');
  });

  it('should filter by selected names', () => {
    const units = normalizeApps(
      {
        api: { path: 'apps/api', framework: 'express', port: 3000 },
        web: { path: 'apps/web', framework: 'nextjs', port: null },
      },
      ['api'],
    );
    expect(units).toHaveLength(1);
    expect(units[0].name).toBe('api');
  });
});
