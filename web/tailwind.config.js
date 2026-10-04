/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        mnema: {
          canvas: '#0F1110',
          raised: '#171918',
          surface: '#1A1E1C',
          elevated: '#1C1E1D',
          hover: '#202423',
          hairline: '#242826',
          border: '#2B2F2D',
          'border-strong': '#3B413E',
          'border-field': '#6E7672',
          text: '#E6EAE8',
          muted: '#A3B3AA',
          tertiary: '#8E9C94',
          'body-ink': '#C4C9CE',
          accent: '#2DA771',
          'accent-hover': '#36BD81',
          'accent-subtle': '#17231D',
          'accent-ink': '#0A0A0B',
          band: '#143D2E',
          'band-mark': '#8CCBAA',
          mint: '#8CCBAA',
          danger: '#F35549',
          warning: '#D97706',
          amber: '#FCE4A8',
        }
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'BlinkMacSystemFont', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      }
    },
  },
  plugins: [],
}
