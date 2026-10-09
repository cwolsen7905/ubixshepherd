/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { daemonProxy } from './daemon-proxy'

// Relative base and relative API URLs, so the built app works unchanged when the daemon
// serves it.
export default defineConfig({
  base: './',
  plugins: [react(), daemonProxy()],
  server: { port: 5178, strictPort: true },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
    passWithNoTests: true,
  },
})
