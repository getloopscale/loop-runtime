# Testing Patterns — cloud/dashboard

> T5.2 of plan `2026-05-27-dashboard-faang-hardening-plan.md`. Authoritative
> reference for how to write tests in this codebase. New contributors should
> read this BEFORE writing the first test.

## Stack

- **Runner:** vitest 4.x with jsdom environment
- **Render helper:** `@/test/render` (wraps QueryClientProvider + NuqsAdapter
  + ThemeProvider + MemoryRouter + PageMetaProvider)
- **Assertions:** `@testing-library/react` + `@testing-library/jest-dom`
- **User events:** `@testing-library/user-event` v14+

## The two-layer mock pattern

There are exactly two kinds of mocks in this codebase. Pick one based on
what the test is proving.

### Layer 1 — hook-level mocks (page + component tests)

**When:** the test renders a page or component and only cares about UI
behavior given known data.

**How:** `vi.mock('@/hooks/use-X')` returns a fake hook. The page renders
deterministic UI from the fake data.

```tsx
const mockUseDeployments = vi.fn();
vi.mock('@/hooks/use-deployments', () => ({
  useDeployments: () => mockUseDeployments(),
}));

it('test_renders_builds_list', () => {
  mockUseDeployments.mockReturnValue({
    builds: [{ build_id: 'b1', status: 'succeeded', /* ... */ }],
    loading: false,
    error: false,
  });
  render(<DeploymentsPage />, { initialEntries: ['/projects/x/deployments'] });
  expect(screen.getByText('b1')).toBeInTheDocument();
});
```

**Why:** zero network surface area, deterministic, fast (<100ms per test).

### Layer 2 — lib-level mocks (integration tests)

**When:** the test proves wiring/contract between layers — mutation
invalidation, AbortSignal propagation, queryKey-aware refetch on filter
change, etc.

**How:** mock `@/lib/api` (or another lib boundary) and assert that hooks
called it correctly.

```tsx
const mockEngineFetch = vi.fn();
vi.mock('@/lib/api', () => ({
  engineFetch: (...args: unknown[]) => mockEngineFetch(...args),
}));

it('test_useSwitchTeam_posts_with_team_id', async () => {
  mockEngineFetch.mockResolvedValueOnce({ ok: true });
  const { result } = renderHook(() => useSwitchTeam(), { wrapper: QueryWrapper });
  result.current.mutate('team-xyz');
  await waitFor(() => expect(mockEngineFetch).toHaveBeenCalledTimes(1));
  // ... assert path + body shape
});
```

**Why:** validates the contract between the hook and the API, not UI.

### Do NOT mix layers in one test

Bad:

```tsx
vi.mock('@/hooks/use-deployments', () => ({ useDeployments: ... }));
vi.mock('@/lib/api', () => ({ engineFetch: ... })); // ← redundant + confusing
```

Pick one. If you need both, split into two tests.

## Render helper

Always use `render` from `@/test/render` — never `render` from
`@testing-library/react` directly. The helper provides the provider chain
required by every component:

- QueryClientProvider (test client: `retry: false`, `gcTime: 0`)
- NuqsAdapter (URL state)
- ThemeProvider (violet-forge theme)
- Toaster
- MemoryRouter (optional `initialEntries` argument for routed tests)
- PageMetaProvider (no-op shell since T2.4)

Without the helper, hooks that consume any of these contexts throw at
first render.

## Error boundary tests

When testing a component that throws to verify boundary behavior, silence
`console.error` to avoid noise. React 19 dev StrictMode logs uncaught
errors even when ErrorBoundary catches them.

```tsx
const originalConsoleError = console.error;
beforeEach(() => { console.error = vi.fn(); });
afterEach(() => { console.error = originalConsoleError; });
```

A component that always throws also needs an explicit return type so TSC
accepts it as JSX:

```tsx
function Bomb(): ReactElement {
  throw new Error('boom');
}
```

## Naming convention

Test names describe behavior, not method names. Prefix `test_` is optional
in vitest but consistent with the LoopZero monorepo convention.

| Bad                  | Good                                                |
| -------------------- | --------------------------------------------------- |
| `test_useSwitchTeam` | `test_clears_ALL_queries_on_success`                |
| `test_render_1`      | `test_renders_fallback_when_query_errors`           |
| `test_handle_click`  | `test_invalidates_builds_cache_after_cancel_button` |

## What NOT to test

- React internals (re-render count, hook order, etc.)
- Third-party library behavior (TanStack Query, Radix, nuqs)
- CSS classes that have no behavioral impact
- Trivial getters that just forward a prop

## What MUST have tests

- Every mutation hook: at minimum a "calls correct endpoint" + "invalidates
  correct cache key" pair
- Every state machine transition exposed to UI
- Every form's happy path + at least one validation failure
- Every error boundary path
- Each ConfirmDialog: requires `confirmationPhrase` when destructive

## Hermetic tests only

Tests must not depend on:
- Network (MSW or vi.mock the API layer)
- Real time (use `vi.useFakeTimers()` if timing matters)
- Random (seed or stub `Math.random` if used)
- Other tests' state (each test starts fresh — `beforeEach: vi.clearAllMocks()`)

If a test is flaky, fix it the same day. Skipping/disabling a test that
fails intermittently is forbidden — flaky tests rot the suite trust.

## VITE_USE_MOCKS — offline dev / emergency rollback

The dashboard supports two modes:

- **Default (no flag set):** all `cloudFetch` / `engineFetch` calls hit the
  real cloud BFF (which in turn proxies the engine API). Use this whenever
  a local Tilt stack is running. Backend-failure dialogs surface real
  TCIA error states.
- **`VITE_USE_MOCKS=true`:** `src/lib/mocks/server.ts` wraps `window.fetch`
  and intercepts every `/api/v1/*` path via the `handlers.ts` route table.
  Use this for offline dev OR as an emergency rollback if the live backend
  is unavailable mid-session.

Mocks are tree-shaken from production builds (T7.1 / EC-10 of plan
2026-05-27-backend-frontend-integration v1.1). The CI gate
`npm run tree-shake-check` greps the prod bundle for `lib/mocks` strings
and fails the build if any chunk leaked.

`npm run tree-shake-check` runs `vite build` then asserts:

```bash
! grep -q 'lib/mocks' dist/assets/*.js
```

This protects against an accidental import on a hot path that would defeat
DCE. Add it to the CI pipeline before any release tag.
