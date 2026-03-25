// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

describe('CLI config/constants', () => {
  const originalEnv = process.env;

  beforeEach(() => {
    jest.resetModules();
    process.env = { ...originalEnv };
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

  describe('getDefaultApiUrl', () => {
    it('should return env var when LOOPZERO_API_URL is set', () => {
      process.env.LOOPZERO_API_URL = 'https://api.loopzero.example';
      const constants = loadConstants();
      expect(constants.getDefaultApiUrl()).toBe('https://api.loopzero.example');
    });

    it('should return fallback when LOOPZERO_API_URL is empty', () => {
      delete process.env.LOOPZERO_API_URL;
      const constants = loadConstants();
      expect(constants.getDefaultApiUrl()).toBe('http://localhost:13001');
    });

    it('should return fallback when LOOPZERO_API_URL is empty string', () => {
      process.env.LOOPZERO_API_URL = '';
      const constants = loadConstants();
      expect(constants.getDefaultApiUrl()).toBe('http://localhost:13001');
    });
  });

  describe('isUsingDefaultApiUrl', () => {
    it('should return true when LOOPZERO_API_URL is not set', () => {
      delete process.env.LOOPZERO_API_URL;
      const constants = loadConstants();
      expect(constants.isUsingDefaultApiUrl()).toBe(true);
    });

    it('should return false when LOOPZERO_API_URL is set', () => {
      process.env.LOOPZERO_API_URL = 'https://api.loopzero.example';
      const constants = loadConstants();
      expect(constants.isUsingDefaultApiUrl()).toBe(false);
    });
  });

  describe('DEFAULT_API_URL_FALLBACK', () => {
    it('should be localhost:13001', () => {
      const constants = loadConstants();
      expect(constants.DEFAULT_API_URL_FALLBACK).toBe('http://localhost:13001');
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
});
