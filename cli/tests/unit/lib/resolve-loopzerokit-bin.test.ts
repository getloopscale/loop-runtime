// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.1 T2.1 (loopzero-cli-vercel-like-ux) — resolveLoopZerokitBin tests.

import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import {
  resolveLoopZerokitBin,
  validateCandidate,
  LoopZerokitBinaryNotFoundError,
} from '../../../src/lib/resolve-loopzerokit-bin';

let tmpRoot: string;

beforeEach(() => {
  tmpRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'resolve-bin-test-'));
  delete process.env.LOOPZEROKIT_BIN;
});

afterEach(() => {
  fs.rmSync(tmpRoot, { recursive: true, force: true });
  delete process.env.LOOPZEROKIT_BIN;
});

function writeExecutable(dir: string, contents = '#!/bin/sh\necho mock'): string {
  fs.mkdirSync(dir, { recursive: true });
  const filePath = path.join(dir, 'loopzerokit');
  fs.writeFileSync(filePath, contents);
  fs.chmodSync(filePath, 0o755);
  return filePath;
}

describe('validateCandidate', () => {
  it('returns false for a directory', () => {
    expect(validateCandidate(tmpRoot)).toBe(false);
  });
  it('returns false for a missing path', () => {
    expect(validateCandidate(path.join(tmpRoot, 'nope'))).toBe(false);
  });
  it('returns false for a non-executable file', () => {
    const filePath = path.join(tmpRoot, 'file');
    fs.writeFileSync(filePath, 'x');
    fs.chmodSync(filePath, 0o644);
    expect(validateCandidate(filePath)).toBe(false);
  });
  it('returns true for an executable file', () => {
    const bin = writeExecutable(tmpRoot);
    expect(validateCandidate(bin)).toBe(true);
  });
});

describe('resolveLoopZerokitBin — happy paths', () => {
  it('finds node_modules/.bin/loopzerokit at cwd', () => {
    const expected = writeExecutable(path.join(tmpRoot, 'node_modules', '.bin'));
    expect(resolveLoopZerokitBin(tmpRoot)).toBe(expected);
  });

  it('finds node_modules/.bin/loopzerokit in ancestor (monorepo workspace)', () => {
    const expected = writeExecutable(path.join(tmpRoot, 'node_modules', '.bin'));
    const deep = path.join(tmpRoot, 'apps', 'api', 'src');
    fs.mkdirSync(deep, { recursive: true });
    expect(resolveLoopZerokitBin(deep)).toBe(expected);
  });

  it('honors LOOPZEROKIT_BIN env var with a valid executable', () => {
    const bin = writeExecutable(path.join(tmpRoot, 'custom'));
    process.env.LOOPZEROKIT_BIN = bin;
    expect(resolveLoopZerokitBin(tmpRoot)).toBe(bin);
  });
});

describe('resolveLoopZerokitBin — error paths', () => {
  it('throws LoopZerokitBinaryNotFoundError when nothing resolves', () => {
    // PATH cleared so `which loopzerokit` cannot succeed.
    const originalPath = process.env.PATH;
    process.env.PATH = '/this/path/does/not/exist';
    try {
      expect(() => resolveLoopZerokitBin(tmpRoot)).toThrow(LoopZerokitBinaryNotFoundError);
    } finally {
      process.env.PATH = originalPath;
    }
  });

  it('LoopZerokitBinaryNotFoundError message includes install hint', () => {
    try {
      throw new LoopZerokitBinaryNotFoundError();
    } catch (err) {
      expect((err as LoopZerokitBinaryNotFoundError).message).toContain('npm install loopzerokit');
      expect((err as LoopZerokitBinaryNotFoundError).message).toContain('npm install -g loopzerokit');
    }
  });
});

describe('resolveLoopZerokitBin — EC-1 (LOOPZEROKIT_BIN points to directory or missing)', () => {
  it('rejects LOOPZEROKIT_BIN pointing at a directory', () => {
    process.env.LOOPZEROKIT_BIN = tmpRoot; // tmpRoot is a directory
    expect(() => resolveLoopZerokitBin(tmpRoot)).toThrow(/LOOPZEROKIT_BIN/);
  });

  it('rejects LOOPZEROKIT_BIN pointing at a missing path', () => {
    process.env.LOOPZEROKIT_BIN = path.join(tmpRoot, 'nope');
    expect(() => resolveLoopZerokitBin(tmpRoot)).toThrow(/LOOPZEROKIT_BIN/);
  });
});

describe('resolveLoopZerokitBin — EC-6 (local bin not executable)', () => {
  it('throws actionable error when no executable found anywhere (local bin without +x)', () => {
    // Local bin without executable bit set.
    const binDir = path.join(tmpRoot, 'node_modules', '.bin');
    fs.mkdirSync(binDir, { recursive: true });
    fs.writeFileSync(path.join(binDir, 'loopzerokit'), 'x');
    fs.chmodSync(path.join(binDir, 'loopzerokit'), 0o644);
    const originalPath = process.env.PATH;
    process.env.PATH = '/this/path/does/not/exist';
    try {
      expect(() => resolveLoopZerokitBin(tmpRoot)).toThrow(LoopZerokitBinaryNotFoundError);
    } finally {
      process.env.PATH = originalPath;
    }
  });

  it('falls through when local bin is not executable (continues to system PATH)', () => {
    // Non-executable local + executable global (simulate via LOOPZEROKIT_BIN as the global).
    const binDir = path.join(tmpRoot, 'node_modules', '.bin');
    fs.mkdirSync(binDir, { recursive: true });
    fs.writeFileSync(path.join(binDir, 'loopzerokit'), 'x');
    fs.chmodSync(path.join(binDir, 'loopzerokit'), 0o644);
    const global = writeExecutable(path.join(tmpRoot, 'global'));
    process.env.LOOPZEROKIT_BIN = global;
    expect(resolveLoopZerokitBin(tmpRoot)).toBe(global);
  });
});
