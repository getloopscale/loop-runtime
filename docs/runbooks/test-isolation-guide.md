# Test Isolation Guide

> **Plan T1.1 reference.** Per-test isolation patterns to eliminate parallel
> test flakes in Jest. Phase 1 (`jest.config.js maxWorkers=2`) is mitigation;
> Phase 2 patterns below are the root-cause fix.

## Problem

CLI test suite has 1250 tests. In default-parallel mode (4 workers), ~41 fail intermittently. In `--runInBand` (sequential), 0 fail. Symptom: tests share global state (ports, FS, env, module-state) that races under parallel execution.

## Phase 1 mitigation (active)

`cli/jest.config.js` sets `maxWorkers: 2`. Reduces contention from 4-worker default to 2 workers. Empirical: cuts flake from ~3% to <0.5%. Trade-off: ~2x runtime vs 4-worker, still 2x faster than `--runInBand`.

## Phase 2 root-cause patterns (pending iter2)

### Pattern A: Dynamic port allocation

**Bad (causes flake):**
```typescript
const PORT = 8765;
const server = createServer(...);
server.listen(PORT);
```

**Good:**
```typescript
import type { AddressInfo } from 'net';
const server = createServer(...);
await new Promise<void>((r) => server.listen(0, '127.0.0.1', () => r()));
const port = (server.address() as AddressInfo).port;
```

Use the assigned port via `port` variable. Each parallel worker gets a unique port.

### Pattern B: Temp dir per test

**Bad:**
```typescript
const tmpDir = '/tmp/test-fixtures';  // shared across all tests
fs.writeFileSync(`${tmpDir}/file.txt`, 'data');
```

**Good:**
```typescript
import { mkdtemp, rm } from 'fs/promises';
import { tmpdir } from 'os';
import { join } from 'path';

let tmpDir: string;
beforeEach(async () => {
  tmpDir = await mkdtemp(join(tmpdir(), 'loopzero-test-'));
});
afterEach(async () => {
  await rm(tmpDir, { recursive: true, force: true });
});
```

`mkdtemp` returns unique path per call. No collision between workers.

### Pattern C: Env snapshot + restore

**Bad:**
```typescript
process.env.LOOPZERO_API_URL = 'http://localhost:13001';
// test uses env var; another worker test sees stale value
```

**Good:**
```typescript
let originalEnv: NodeJS.ProcessEnv;
beforeEach(() => {
  originalEnv = { ...process.env };
});
afterEach(() => {
  process.env = originalEnv;
});
```

Each test starts with pristine env. Mutations in one test don't leak to another.

### Pattern D: Module-state isolation

If a module holds singleton state (caches, connections):

**Bad:**
```typescript
import { apiClient } from '../api/client';  // singleton
apiClient.setToken('test-token');
// next test sees token='test-token' even if it didn't set it
```

**Good:**
```typescript
beforeEach(() => {
  jest.resetModules();
});

it('does X', async () => {
  const { apiClient } = await import('../api/client');  // fresh instance
  apiClient.setToken('test-token');
});
```

Or convert singleton to factory function and create per-test.

## Migration order (Phase 2)

Priority by flake frequency (run `for i in {1..10}; do npm test 2>&1 | grep "Tests:" | tail -1; done`):

1. **`tests/integration/exit-code-contract.test.ts`** — observed flake in iter1 measurement (line 166 timeout). Likely Pattern A (mock-api-server port).
2. **`tests/integration/_fixtures/mock-api-server.ts`** — fixture used by many tests; fix here cascades.
3. **`tests/integration/_fixtures/spawn-helper.ts`** — uses /tmp paths; Pattern B candidate.
4. **Any test that does `process.env.X = ...`** — Pattern C.

After Phase 2 complete, remove `maxWorkers: 2` from `jest.config.js`.

## Validation

```bash
# 3 consecutive runs at default parallelism — must pass all 3.
for i in 1 2 3; do
  cd cli && npm test || exit 1
done

# Optional sequential check (guarantees no race).
cd cli && npx jest --runInBand
```

When all 3 default-parallel runs are GREEN consecutive, Phase 2 is done.

## Why not just `--runInBand` forever

- `--runInBand` is 4-5x slower than parallel (348s vs 72s observed in iter1).
- Time-to-CI-feedback is critical metric. Sequential CI = developer waits longer.
- Forces per-test isolation as standard, not loophole.

## References

- Plan `pre-launch-gaps-plan.md` T1.1
- ADR D4 (parallel flakes root-cause)
- `cli/jest.config.js` (Phase 1 mitigation)
- ADR-0022 (governance for any DoD relaxation)
