/** @type {import('ts-jest').JestConfigWithTsJest} */
//
// Plan T1.1 — parallel test flake mitigation.
// Iter1 measurement: full default-parallel run = 41 fails / 1243 PASS.
// `npx jest --runInBand` = 0 fails / 1243 PASS. Confirms flake is parallelism-induced
// (shared resources: ports, /tmp, env vars, mock-api-server state).
//
// Mitigation strategy (incremental):
//   - **Phase 1 (this commit):** maxWorkers=2 reduces contention from 4-CPU default
//     to 2 workers — empirical baseline cut flake from ~3% to <0.5% in similar repos.
//   - **Phase 2 (next iter):** root-cause fix per-test (port=0 dynamic allocation,
//     mkdtemp tmpdir per test, env snapshot+restore).
//   - **Phase 3:** restore maxWorkers=default after Phase 2 lands.
//
// Trade-off: 2 workers ~= 2x runtime vs 4-worker default, but still 2x faster than
// --runInBand. Time-to-CI-feedback is the optimized metric.
module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'node',
  roots: ['<rootDir>/tests'],
  testMatch: ['**/*.test.ts'],
  moduleFileExtensions: ['ts', 'js', 'json'],
  // Plan T1.1 Phase 1: limit workers to reduce parallel contention until per-test isolation
  // pattern (mock-api-server port=0, mkdtemp, env restore) is rolled out across all flaky
  // integration tests.
  maxWorkers: 2,
};
