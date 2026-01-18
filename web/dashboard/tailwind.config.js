/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './index.html',
    './src/**/*.{js,ts,jsx,tsx}',
  ],
  darkMode: 'class',
  theme: {
    extend: {
      fontFamily: {
        // Modern monospace for titles, code, and technical elements
        // IBM Plex Mono - contemporary, technical feel with excellent readability
        mono: ['"IBM Plex Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'Liberation Mono', 'Courier New', 'monospace'],
        // IBM Plex Sans - sister font for visual harmony
        sans: ['"IBM Plex Sans"', 'ui-sans-serif', 'system-ui', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'Helvetica Neue', 'Arial', 'sans-serif'],
      },
      letterSpacing: {
        // Custom tracking for better readability
        'tighter': '-0.02em',
        'tight': '-0.01em',
        'normal': '0',
        'wide': '0.02em',
        'wider': '0.04em',
        'widest': '0.08em',
        // Special tracking for monospace titles
        'mono-tight': '0.01em',
        'mono-normal': '0.02em',
        'mono-wide': '0.04em',
      },
      fontSize: {
        // Extended scale for finer control
        '2xs': ['0.625rem', { lineHeight: '0.875rem' }],  // 10px
        'xs': ['0.75rem', { lineHeight: '1.125rem' }],    // 12px
        'sm': ['0.875rem', { lineHeight: '1.375rem' }],   // 14px
        'base': ['1rem', { lineHeight: '1.5rem' }],       // 16px
        'lg': ['1.125rem', { lineHeight: '1.75rem' }],    // 18px
        'xl': ['1.25rem', { lineHeight: '1.875rem' }],    // 20px
        '2xl': ['1.5rem', { lineHeight: '2rem' }],        // 24px
      },
    },
  },
  plugins: [],
}
