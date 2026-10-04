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
        discord: {
          darkest: '#1e1f22',
          darker: '#2b2d31',
          dark: '#313338',
          light: '#383a40',
          hover: '#35373c',
          text: '#dbdee1',
          muted: '#949ba4',
          accent: '#5865f2',
          green: '#23a55a',
          red: '#f23f43'
        }
      }
    },
  },
  plugins: [],
}
