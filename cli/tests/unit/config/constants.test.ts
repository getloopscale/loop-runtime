// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

describe('CLI config/constants', () => {
  const originalEnv = process.env;

  beforeEach(() => {
    jest.resetModules();
    process.env = { ...originalEnv };
    // Clear all URL env vars for clean test state
    delete process.env.LOOPZERO_URL;
    delete process.env.LOOPZERO_API_URL;
    delete process.env.LOOPZERO_CLOUD_URL;
    delete process.env.LOOPZERO_DASHBOARD_URL;
  });

  afterEach(() => {
    process.env = originalEnv;
  });

  function loadConstants() {
    // Re-import to pick up changed env vars
    return jest.requireActual('../../../src/config/constants');
  }

  describe('envInt (via API_REQUEST_TIMEOUT_MS)', () => {
    it('should return default when env var is missing', () => {
      delete process.env.LOOPZERO_API_TIMEOUT;
      const constants = loadConstants();
      expect(constants.API_REQUEST_TIMEOUT_MS).toBe(30000);
    });

    it('should return default when env var is NaN', () => {
      process.env.LOOPZERO_API_TIMEOUT = 'not-a-number';
      const constants = loadConstants();
      expect(constants.API_REQUEST_TIMEOUT_MS).toBe(30000);
    });

    it('should return parsed value for valid env var', () => {
      process.env.LOOPZERO_API_TIMEOUT = '5000';
      const constants = loadConstants();
      expect(constants.API_REQUEST_TIMEOUT_MS).toBe(5000);
    });
  });

  describe('getDefaultApiUrl (MEET-040: unified LOOPZERO_URL)', () => {
    it('should return LOOPZERO_URL when set', () => {
      process.env.LOOPZERO_URL = 'https://api.loopzero.example';
      const constants = loadConstants();
      expect(constants.getDefaultApiUrl()).toBe('https://api.loopzero.example');
    });

    it('should return LOOPZERO_API_URL as legacy fallback', () => {
      process.env.LOOPZERO_API_URL = 'https://api.loopzero.example';
      const constants = loadConstants();
      expect(constants.getDefaultApiUrl()).toBe('https://api.loopzero.example');
    });

    it('should prefer LOOPZERO_URL over LOOPZERO_API_URL', () => {
      process.env.LOOPZERO_URL = 'https://new.loopzero.example';
      process.env.LOOPZERO_API_URL = 'https://old.loopzero.example';
      const constants = loadConstants();
      expect(constants.getDefaultApiUrl()).toBe('https://new.loopzero.example');
    });

    it('should return fallback when no URL env vars set', () => {
      const constants = loadConstants();
      expect(constants.getDefaultApiUrl()).toBe('http://localhost:8080');
    });

    it('should return fallback when LOOPZERO_URL is empty string', () => {
      process.env.LOOPZERO_URL = '';
      const constants = loadConstants();
      expect(constants.getDefaultApiUrl()).toBe('http://localhost:8080');
    });
  });

  describe('getCloudUrl (MEET-040: defaults to same as API URL)', () => {
    it('should return LOOPZERO_CLOUD_URL when set (legacy)', () => {
      process.env.LOOPZERO_CLOUD_URL = 'https://cloud.loopzero.example';
      const constants = loadConstants();
      expect(constants.getCloudUrl()).toBe('https://cloud.loopzero.example');
    });

    it('should return same as getDefaultApiUrl when LOOPZERO_CLOUD_URL not set', () => {
      process.env.LOOPZERO_URL = 'https://api.loopzero.example';
      const constants = loadConstants();
      expect(constants.getCloudUrl()).toBe('https://api.loopzero.example');
    });

    it('should return fallback when no URL env vars set', () => {
      const constants = loadConstants();
      expect(constants.getCloudUrl()).toBe('http://localhost:8080');
    });
  });

  describe('DEFAULT_API_URL_FALLBACK', () => {
    it('should be localhost:8080', () => {
      const constants = loadConstants();
      expect(constants.DEFAULT_API_URL_FALLBACK).toBe('http://localhost:8080');
    });
  });

  describe('envInt edge cases (via DEPLOY_TIMEOUT_MS)', () => {
    it('should return default for empty env var', () => {
      delete process.env.LOOPZERO_DEPLOY_TIMEOUT;
      const constants = loadConstants();
      expect(constants.DEPLOY_TIMEOUT_MS).toBe(10 * 60 * 1000);
    });

    it('should parse valid integer override', () => {
      process.env.LOOPZERO_DEPLOY_TIMEOUT = '120000';
      const constants = loadConstants();
      expect(constants.DEPLOY_TIMEOUT_MS).toBe(120000);
    });
  });

  describe('getDashboardUrl (T2.1 of plan 2026-06-11-code-review-remediation)', () => {
    it('should return canonical dev-public fallback when no env override', () => {
      const constants = loadConstants();
      expect(constants.getDashboardUrl()).toBe('https://app-dev.loopzero.example');
    });

    it('should NOT contain the pre-T2.1 wrong pattern (app.dev with dot)', () => {
      // R5 regression guard: the original hardcoded literal in login.ts
      // was `https://app.dev.loopzero.example` which is NOT a real DNS name.
      // Canonical pattern is `app-dev.loopzero.example` per
      // dev-public-domain-canonicalization 2026-06-04.
      const constants = loadConstants();
      const url = constants.getDashboardUrl();
      expect(url).not.toMatch(/app\.dev\.loopzero\.dev/);
      expect(url).toMatch(/app-dev\.loopzero\.dev/);
    });

    it('should return LOOPZERO_DASHBOARD_URL override when set', () => {
      process.env.LOOPZERO_DASHBOARD_URL = 'https://my-dashboard.example.com';
      const constants = loadConstants();
      expect(constants.getDashboardUrl()).toBe('https://my-dashboard.example.com');
    });

    it('should accept http:// URLs (local dev override)', () => {
      process.env.LOOPZERO_DASHBOARD_URL = 'http://localhost:5173';
      const constants = loadConstants();
      expect(constants.getDashboardUrl()).toBe('http://localhost:5173');
    });

    it('should throw when override is missing protocol', () => {
      process.env.LOOPZERO_DASHBOARD_URL = 'app-dev.loopzero.example';
      const constants = loadConstants();
      expect(() => constants.getDashboardUrl()).toThrow(/must start with http/);
    });
  });
});
