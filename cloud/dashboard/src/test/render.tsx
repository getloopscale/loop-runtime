import { render as rtlRender, type RenderOptions } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ThemeProvider, Toaster, violetForge } from '@loopzero/ui';
import { PageMetaProvider } from '@/hooks/use-page-meta';
import type { ReactElement, ReactNode } from 'react';

interface RenderProvidersProps {
  children: ReactNode;
  initialEntries?: string[];
}

/**
 * Test wrapper providing the same context chain as production:
 *   ThemeProvider (single-theme violet-forge) > Toaster > MemoryRouter > PageMetaProvider
 *
 * MemoryRouter uses initialEntries to set the active route for tests.
 */
function AllProviders({ children, initialEntries = ['/'] }: RenderProvidersProps) {
  return (
    <ThemeProvider themes={[violetForge]} defaultTheme="violet-forge" defaultMode="dark">
      <Toaster>
        <MemoryRouter initialEntries={initialEntries}>
          <PageMetaProvider>{children}</PageMetaProvider>
        </MemoryRouter>
      </Toaster>
    </ThemeProvider>
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
