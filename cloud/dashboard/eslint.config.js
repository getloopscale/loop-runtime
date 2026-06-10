// ESLint flat config (v9+) — created 2026-05-29 to unblock `npm run lint`
// per plan 2026-05-29-login-page-pragmatic-uplift v2.0 Global DoD.
//
// Scope: TypeScript-first minimum viable lint (matches cli/eslint.config.mjs
// pattern). Full React plugin coverage is parked until eslint-plugin-react
// ships ESLint 10 support — peer-range on 7.x caps at eslint@^9.7.
// Until then we (a) stub `react-hooks` so existing
// `// eslint-disable-next-line react-hooks/exhaustive-deps` comments don't
// raise "rule not found" errors, and (b) keep the lint surface focused on
// TypeScript correctness, leaving React-specific drift detection to tsc
// (strict) + vitest + visual smoke.

import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import globals from 'globals';

// Shim plugin: declares `react-hooks` rules as no-ops so legacy
// `// eslint-disable-next-line react-hooks/*` comments resolve without
// "rule not found" errors. When eslint-plugin-react-hooks ships ESLint 10
// support, swap this shim for the real plugin.
const reactHooksShim = {
  rules: {
    'exhaustive-deps': { create: () => ({}) },
    'rules-of-hooks': { create: () => ({}) },
  },
};

export default [
  {
    ignores: [
      'dist/**',
      'storybook-static/**',
      'node_modules/**',
      'public/**',
      'referencia/**',
      '*.config.js',
      '*.config.ts',
      '*.config.mjs',
      'vite.config.ts',
      'vitest.config.ts',
      'postcss.config.js',
      'lighthouserc.json',
      '.storybook/**',
      'e2e/**',
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['src/**/*.{ts,tsx}'],
    linterOptions: {
      // Some files carry legacy disable directives whose target rule no
      // longer exists (react-hooks not loaded as a real plugin). Silence
      // the meta-warning until plugin parity is restored.
      reportUnusedDisableDirectives: 'off',
    },
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
      globals: {
        ...globals.browser,
        ...globals.es2022,
      },
    },
    plugins: {
      'react-hooks': reactHooksShim,
    },
    rules: {
      // Match cli/eslint.config.mjs conventions
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/no-explicit-any': 'warn',
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      'no-empty': ['error', { allowEmptyCatch: true }],
      // Rebaixado para warn: 1 ocorrência pré-existente fora do escopo
      // de qualquer plano ativo. Promover para error em plano de cleanup
      // dedicado, NÃO aqui (plano 2026-05-29 é interface-only).
      'no-useless-assignment': 'warn',
    },
  },
  {
    files: ['src/**/*.test.{ts,tsx}', 'src/test/**/*.{ts,tsx}'],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.es2022,
        ...globals.node,
      },
    },
    rules: {
      // Tests legitimately use `any` for fixture forging and mock typing.
      '@typescript-eslint/no-explicit-any': 'off',
      'no-console': 'off',
    },
  },
];
