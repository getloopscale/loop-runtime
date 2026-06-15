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
  // T4.3 of plan 2026-06-15-code-review-49-findings-remediation (R3-F5 HIGH):
  // establish a coverage floor — engaged only via `npm run test:coverage`.
  // The default suite (`npm test`) runs unchanged (no coverage collection).
  // The 60% threshold at landing is conservative on purpose: existing 1,260
  // test cases cover the canonical paths but lots of error branches are
  // unmeasured today; bumping past 60% needs a deliberate fill-the-gaps
  // sprint. The intent is regression-defense: adding new production code
  // with zero tests drops the floor.
  collectCoverageFrom: [
    'src/**/*.{ts,tsx}',
    '!src/**/*.d.ts',
    '!src/**/index.ts',
  ],
  coverageThreshold: {
    global: {
      branches: 60,
      functions: 60,
      lines: 60,
      statements: 60,
    },
  },
};
