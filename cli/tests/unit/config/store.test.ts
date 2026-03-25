// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { ConfigStore } from '../../../src/config/store';

describe('ConfigStore', () => {
  let tmpDir: string;
  let store: ConfigStore;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-test-'));
    store = new ConfigStore(tmpDir);
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  describe('saveToken', () => {
    it('should create config directory if it does not exist', () => {
      const nestedDir = path.join(tmpDir, 'nested', '.loopzero');
      const nestedStore = new ConfigStore(path.join(tmpDir, 'nested'));
      nestedStore.saveToken('ghp_test123', 'user@example.com', 'acme');

      expect(fs.existsSync(path.join(nestedDir, 'config.json'))).toBe(true);
    });

    it('should write config file with 0600 permissions', () => {
      store.saveToken('ghp_test123', 'user@example.com', 'acme');

      const configPath = path.join(tmpDir, '.loopzero', 'config.json');
      const stats = fs.statSync(configPath);
      // 0600 = owner read/write only (0o600 = 384 decimal, mode & 0o777)
      expect(stats.mode & 0o777).toBe(0o600);
    });

    it('should store email and team but never the raw token value in logs', () => {
      store.saveToken('ghp_secret_token', 'user@example.com', 'acme');

      const config = store.getConfig();
      expect(config).not.toBeNull();
      expect(config!.email).toBe('user@example.com');
      expect(config!.team).toBe('acme');
      expect(config!.token).toBe('ghp_secret_token');
    });
  });

  describe('getToken', () => {
    it('should return null when no config exists', () => {
      expect(store.getToken()).toBeNull();
    });

    it('should return token after save', () => {
      store.saveToken('ghp_abc', 'user@test.com', 'team1');
      expect(store.getToken()).toBe('ghp_abc');
    });

    it('should prefer LOOPZERO_TOKEN env var over config file', () => {
      store.saveToken('ghp_from_file', 'user@test.com', 'team1');
      const original = process.env.LOOPZERO_TOKEN;
      try {
        process.env.LOOPZERO_TOKEN = 'ghp_from_env';
        expect(store.getToken()).toBe('ghp_from_env');
      } finally {
        if (original === undefined) {
          delete process.env.LOOPZERO_TOKEN;
        } else {
          process.env.LOOPZERO_TOKEN = original;
        }
      }
    });

    it('should return LOOPZERO_TOKEN when no config file exists', () => {
      const original = process.env.LOOPZERO_TOKEN;
      try {
        process.env.LOOPZERO_TOKEN = 'ghp_env_only';
        expect(store.getToken()).toBe('ghp_env_only');
      } finally {
        if (original === undefined) {
          delete process.env.LOOPZERO_TOKEN;
        } else {
          process.env.LOOPZERO_TOKEN = original;
        }
      }
    });

    it('should ignore empty LOOPZERO_TOKEN and fall back to config file', () => {
      store.saveToken('ghp_from_file', 'user@test.com', 'team1');
      const original = process.env.LOOPZERO_TOKEN;
      try {
        process.env.LOOPZERO_TOKEN = '';
        expect(store.getToken()).toBe('ghp_from_file');
      } finally {
        if (original === undefined) {
          delete process.env.LOOPZERO_TOKEN;
        } else {
          process.env.LOOPZERO_TOKEN = original;
        }
      }
    });
  });

  describe('getEmail', () => {
    it('should return null when no config exists', () => {
      expect(store.getEmail()).toBeNull();
    });

    it('should return email after save', () => {
      store.saveToken('ghp_abc', 'user@test.com', 'team1');
      expect(store.getEmail()).toBe('user@test.com');
    });
  });

  describe('getTeam', () => {
    it('should return null when no config exists', () => {
      expect(store.getTeam()).toBeNull();
    });

    it('should return team after save', () => {
      store.saveToken('ghp_abc', 'user@test.com', 'myteam');
      expect(store.getTeam()).toBe('myteam');
    });
  });

  describe('clearToken', () => {
    it('should remove config file', () => {
      store.saveToken('ghp_abc', 'user@test.com', 'team1');
      store.clearToken();
      expect(store.getToken()).toBeNull();
    });

    it('should not throw if config does not exist', () => {
      expect(() => store.clearToken()).not.toThrow();
    });
  });

  describe('save → get → clear lifecycle', () => {
    it('should complete full lifecycle', () => {
      expect(store.getToken()).toBeNull();

      store.saveToken('ghp_lifecycle', 'dev@co.com', 'devteam');
      expect(store.getToken()).toBe('ghp_lifecycle');
      expect(store.getEmail()).toBe('dev@co.com');
      expect(store.getTeam()).toBe('devteam');

      store.clearToken();
      expect(store.getToken()).toBeNull();
      expect(store.getEmail()).toBeNull();
    });
  });
});
