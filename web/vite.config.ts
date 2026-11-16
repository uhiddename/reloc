import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // Every asset as a file under assets/: the CSP from web.go
    // (default-src 'self') blocks the data: URIs Vite inlines small ones as.
    assetsInlineLimit: 0,
  },
})
