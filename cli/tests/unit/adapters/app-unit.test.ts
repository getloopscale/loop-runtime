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

  it('should auto-detect frontend from nextjs framework', () => {
    const unit = normalizeApp('web', {
      path: 'apps/web', framework: 'nextjs', port: null,
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
