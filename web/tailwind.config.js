/** @type {import('tailwindcss').Config} */
export default {
  content: ['./public.html', './admin.html', './src/**/*.{svelte,ts}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#effcfb',
          100: '#d7f7f5',
          200: '#b0eeeb',
          300: '#79e0dc',
          400: '#3ec9c6',
          500: '#22aeac',
          600: '#178c8c',
          700: '#166f70',
          800: '#17595a',
          900: '#174a4b',
          950: '#072c2e'
        }
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif']
      }
    }
  },
  plugins: []
};
