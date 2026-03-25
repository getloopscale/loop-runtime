// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

describe('CLI Integration Tests (requires running API)', () => {
  describe('login flow', () => {
    it.skip('should authenticate with real GitHub token', () => {
      // Requires: GITHUB_TOKEN env var, running API
    });
    it.skip('should reject expired token', () => {
      // Requires: running API
    });
  });

  describe('init flow', () => {
    it.skip('should detect monorepo in real git repo', () => {
      // Requires: temp git repo with package.json
    });
    it.skip('should create loopzero.yaml with correct detection', () => {
      // Requires: temp git repo, running API
    });
  });

  describe('deploy flow', () => {
    it.skip('should submit build and receive SSE events', () => {
      // Requires: running API, build cluster
    });
    it.skip('should handle concurrent deploy rejection', () => {
      // Requires: running API with real advisory locks
    });
    it.skip('should timeout gracefully on long builds', () => {
      // Requires: running API, mock slow build
    });
  });
});
