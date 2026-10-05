import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// In dev the panel runs on :8080 and Vite proxies the API to it. Start the panel with
// MECHON_PUBLIC_URL=http://localhost:5173 so the origin check matches the browser.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  server: {
    proxy: { '/api': 'http://localhost:8080' },
  },
  build: {
    // The Go binary embeds dist/; keep the placeholder so `go build` works without a web build.
    emptyOutDir: true,
  },
})
