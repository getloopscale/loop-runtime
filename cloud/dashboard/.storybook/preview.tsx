// T5.1: Storybook preview — wraps every story em provider chain matching
// production (theme + query client + router). Stories must NOT fetch real
// data; mock at hook layer or pass mock data as args.
import type { Preview } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { LoopZeroUIProvider, violetForge } from '@loopzero/ui';
import '../src/index.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, gcTime: 0, staleTime: Infinity },
    mutations: { retry: false },
  },
});

const preview: Preview = {
  parameters: {
    backgrounds: {
      default: 'loopzero-dark',
      values: [{ name: 'loopzero-dark', value: '#09090b' }],
    },
    controls: {
      matchers: { color: /(background|color)$/i, date: /Date$/ },
    },
  },
  decorators: [
    (Story) => (
      <QueryClientProvider client={queryClient}>
        <LoopZeroUIProvider
          theme={{
            themes: [violetForge],
            defaultTheme: 'violet-forge',
            defaultMode: 'dark',
          }}
        >
          <MemoryRouter>
            <div style={{ padding: 16 }}>
              <Story />
            </div>
          </MemoryRouter>
        </LoopZeroUIProvider>
      </QueryClientProvider>
    ),
  ],
};

export default preview;
