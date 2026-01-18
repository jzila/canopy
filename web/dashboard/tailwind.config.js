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
        // Light 300 for titles, Regular 400 for bold/emphasis
        mono: ['"IBM Plex Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'Liberation Mono', 'Courier New', 'monospace'],
        // IBM Plex Sans - sister font for visual harmony
        sans: ['"IBM Plex Sans"', 'ui-sans-serif', 'system-ui', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'Helvetica Neue', 'Arial', 'sans-serif'],
      },
      fontWeight: {
        // Explicit weights for IBM Plex Mono usage
        light: '300',    // Titles
        normal: '400',   // Bold text (relative to light)
        medium: '500',
        semibold: '600',
        bold: '700',
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
        // Extended scale for finer control - slightly larger for readability
        '2xs': ['0.6875rem', { lineHeight: '1rem' }],     // 11px - was 10px, more readable
        'xs': ['0.8125rem', { lineHeight: '1.25rem' }],   // 13px - was 12px, better for body text
        'sm': ['0.9375rem', { lineHeight: '1.5rem' }],    // 15px - was 14px, improved readability
        'base': ['1rem', { lineHeight: '1.625rem' }],     // 16px - slightly more line height
        'lg': ['1.125rem', { lineHeight: '1.875rem' }],   // 18px
        'xl': ['1.25rem', { lineHeight: '2rem' }],        // 20px
        '2xl': ['1.5rem', { lineHeight: '2.25rem' }],     // 24px
      },
    },
  },
  plugins: [],
}
