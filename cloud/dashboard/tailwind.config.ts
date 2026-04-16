import type { Config } from 'tailwindcss';

const config: Config = {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        loopzero: {
          50: '#e8f4f8',
          100: '#b8dfe8',
          200: '#88c9d8',
          300: '#58b3c8',
          400: '#389eb8',
          500: '#2a8a9e',
          600: '#1f6b7a',
          700: '#154d57',
          800: '#0c3038',
          900: '#04171c',
          950: '#020c0f',
        },
        surface: {
          0: '#09090b',
          1: '#0f0f12',
          2: '#17171c',
          3: '#1f1f26',
          4: '#27272f',
        },
        accent: '#3ecf8e',
        danger: '#ef4444',
        warning: '#f59e0b',
      },
      fontFamily: {
        sans: ['"DM Sans"', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', '"Fira Code"', 'monospace'],
        display: ['"Cabinet Grotesk"', '"DM Sans"', 'system-ui', 'sans-serif'],
      },
      keyframes: {
        'slide-in': {
          '0%': { opacity: '0', transform: 'translateY(8px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        'fade-in': {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        'pulse-subtle': {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0.7' },
        },
      },
      animation: {
        'slide-in': 'slide-in 0.4s ease-out',
        'fade-in': 'fade-in 0.3s ease-out',
        'pulse-subtle': 'pulse-subtle 2s ease-in-out infinite',
      },
    },
  },
  plugins: [],
};

export default config;
