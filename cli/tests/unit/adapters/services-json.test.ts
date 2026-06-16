// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.1 T1.1 (loopzero-cli-vercel-like-ux) — services.json discovery tests.

import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import {
  findServicesJson,
  parseServicesRuntimes,
  ServicesJsonOverrideError,
} from '../../../src/adapters/services-json';

// simplify-deploy-loopzerokit-runtime-wiring T3.1a (F-dom-2): the deploy payload
// must carry the per-app runtime parsed from services.json, since the API now
// dispatches the build strategy on `runtime` (not framework).
describe('parseServicesRuntimes', () => {
  it('maps each service name to its declared runtime', () => {
    const json = JSON.stringify({
      version: 2,
      project: 'shop',
      services: [
        { name: 'api', runtime: 'python', type: 'server' },
        { name: 'web', runtime: 'node', type: 'server' },
        { name: 'queue', runtime: 'node', type: 'worker' },
      ],
    });
    expect(parseServicesRuntimes(json)).toEqual({ api: 'python', web: 'node', queue: 'node' });
  });

  it('skips services missing name or runtime, and tolerates malformed JSON', () => {
    expect(
      parseServicesRuntimes(JSON.stringify({ services: [{ name: 'api' }, { runtime: 'node' }] })),
    ).toEqual({});
    expect(parseServicesRuntimes('not json {')).toEqual({});
    expect(parseServicesRuntimes('{}')).toEqual({});
  });
});

let tmpRoot: string;

beforeEach(() => {
  tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'services-json-test-'));
});

afterEach(() => {
  fs.rmSync(tmpRoot, { recursive: true, force: true });
});

function writeServicesJson(dir: string, content = '{}'): string {
  const loopzeroDir = path.join(dir, '.loopzero');
  fs.mkdirSync(loopzeroDir, { recursive: true });
  const filePath = path.join(loopzeroDir, 'services.json');
  fs.writeFileSync(filePath, content);
  return filePath;
}

describe('findServicesJson — walk-up discovery', () => {
  it('finds .loopzero/services.json in cwd', () => {
    const expected = writeServicesJson(tmpRoot);
    const result = findServicesJson(tmpRoot, tmpRoot);
    expect(result).not.toBeNull();
    expect(result?.path).toBe(expected);
  });

  it('finds .loopzero/services.json in ancestor before gitRoot', () => {
    const expected = writeServicesJson(tmpRoot);
    const deep = path.join(tmpRoot, 'apps', 'api', 'src');
    fs.mkdirSync(deep, { recursive: true });
    const result = findServicesJson(deep, tmpRoot);
    expect(result?.path).toBe(expected);
  });

  it('stops at gitRoot — returns null when .loopzero/services.json sits above gitRoot', () => {
    const repo = path.join(tmpRoot, 'repo');
    const above = tmpRoot; // ancestor of repo
    fs.mkdirSync(repo);
    writeServicesJson(above);
    const result = findServicesJson(repo, repo);
    expect(result).toBeNull();
  });

  it('returns null when no services.json exists anywhere in scope', () => {
    const cwd = path.join(tmpRoot, 'src');
    fs.mkdirSync(cwd);
    const result = findServicesJson(cwd, tmpRoot);
    expect(result).toBeNull();
  });

  it('stops at filesystem root (no infinite loop)', () => {
    // Use the actual filesystem root as both cwd and gitRoot equivalent.
    const result = findServicesJson('/', '/');
    expect(result === null || result?.path === '/.loopzero/services.json').toBe(true);
  });
});

describe('findServicesJson — --services-json override (EC-4)', () => {
  it('respects --services-json override pointing at custom distDir', () => {
    const outDir = path.join(tmpRoot, 'out');
    fs.mkdirSync(outDir);
    const customPath = path.join(outDir, 'services.json');
    fs.writeFileSync(customPath, '{"version":2}');
    const result = findServicesJson(tmpRoot, tmpRoot, customPath);
    expect(result?.path).toBe(customPath);
  });

  it('throws ServicesJsonOverrideError when override target is absent', () => {
    const missing = path.join(tmpRoot, 'out', 'services.json');
    expect(() => findServicesJson(tmpRoot, tmpRoot, missing)).toThrow(
      ServicesJsonOverrideError,
    );
  });
});
