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
        darkBg: '#0f172a',
        cardBg: '#1e293b',
        cardBorder: '#334155',
        primary: '#38bdf8',
        accent: '#818cf8',
      }
    },
  },
  plugins: [],
}
