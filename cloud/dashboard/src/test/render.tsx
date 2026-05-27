import { render as rtlRender, type RenderOptions } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { NuqsAdapter } from 'nuqs/adapters/react-router/v7';
import { ThemeProvider, Toaster, violetForge } from '@loopzero/ui';
import { PageMetaProvider } from '@/hooks/use-page-meta';
import type { ReactElement, ReactNode } from 'react';

interface RenderProvidersProps {
  children: ReactNode;
  initialEntries?: string[];
}

/**
 * Test wrapper providing the same context chain as production:
 *   QueryClientProvider (test client) > ThemeProvider > Toaster > MemoryRouter > PageMetaProvider
 *
 * MemoryRouter uses initialEntries to set the active route for tests.
 *
 * Test QueryClient (EC-08 fold): retry:false (deterministic failures, no
 * exponential backoff em test), gcTime:0 (no cache leak entre tests).
 * Fresh client per render call so test isolation is preserved.
 */
function makeTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
      mutations: { retry: false },
    },
  });
}

function AllProviders({ children, initialEntries = ['/'] }: RenderProvidersProps) {
  const client = makeTestQueryClient();
  // NuqsAdapter MUST be inside MemoryRouter (consumes router context).
  return (
    <QueryClientProvider client={client}>
      <ThemeProvider themes={[violetForge]} defaultTheme="violet-forge" defaultMode="dark">
        <Toaster>
          <MemoryRouter initialEntries={initialEntries}>
            <NuqsAdapter>
              <PageMetaProvider>{children}</PageMetaProvider>
            </NuqsAdapter>
          </MemoryRouter>
        </Toaster>
      </ThemeProvider>
    </QueryClientProvider>
  );
}

interface CustomRenderOptions extends Omit<RenderOptions, 'wrapper'> {
  initialEntries?: string[];
}

export function render(ui: ReactElement, options: CustomRenderOptions = {}) {
  const { initialEntries, ...rest } = options;
  return rtlRender(ui, {
    wrapper: ({ children }) => (
      <AllProviders initialEntries={initialEntries}>{children}</AllProviders>
    ),
    ...rest,
  });
}

export * from '@testing-library/react';
