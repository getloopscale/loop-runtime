// T5.1: Storybook 8 + Vite builder. Reuses dashboard's own vite.config
// so plugin chain (React Compiler, alias @/) works inside stories.
import type { StorybookConfig } from '@storybook/react-vite';

const config: StorybookConfig = {
  stories: ['../src/**/*.stories.@(ts|tsx)'],
  addons: ['@storybook/addon-essentials'],
  framework: {
    name: '@storybook/react-vite',
    options: {},
  },
  docs: { autodocs: 'tag' },
};

export default config;
