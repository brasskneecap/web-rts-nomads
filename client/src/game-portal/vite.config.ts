/// <reference types="vitest" />
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'
import { execSync } from 'node:child_process'

const GO_SERVER = 'http://localhost:8080'

// §15 task 15.8: __APP_VERSION__ injection. Resolved at build time in this
// priority order:
//   1. NOMADS_VERSION env var (CI / release tags)
//   2. short git SHA from git rev-parse --short HEAD if in a git checkout
//   3. literal "unknown" (tarball / no-git builds)
// `npm run dev` and `tauri:dev` override to "dev" via the dev-mode branch below.
function resolveAppVersion(mode: string): string {
  if (mode === 'development') return 'dev'
  if (process.env.NOMADS_VERSION && process.env.NOMADS_VERSION.length > 0) {
    return process.env.NOMADS_VERSION
  }
  try {
    return execSync('git rev-parse --short HEAD', { stdio: ['ignore', 'pipe', 'ignore'] })
      .toString()
      .trim() || 'unknown'
  } catch {
    return 'unknown'
  }
}

export default defineConfig(({ mode }) => ({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  define: {
    __APP_VERSION__: JSON.stringify(resolveAppVersion(mode)),
  },
  // Default chunkSizeWarningLimit (500 kB) is tuned for web delivery. Our
  // packaged build loads the SPA from 127.0.0.1 over loopback inside the
  // Tauri webview, so per-chunk bytes don't matter the same way. Bumped to
  // 1500 kB so the warning fires only on genuinely runaway bundles.
  build: {
    chunkSizeWarningLimit: 1500,
  },
  server: {
    host: true,
    allowedHosts: true,
    proxy: {
      '/ws': { target: GO_SERVER, ws: true, changeOrigin: true },
      '/health': { target: GO_SERVER, changeOrigin: true },
      '/api': { target: GO_SERVER, changeOrigin: true },
      '/catalog': { target: GO_SERVER, changeOrigin: true },
      '/maps': { target: GO_SERVER, changeOrigin: true },
      '/matches': { target: GO_SERVER, changeOrigin: true },
      '/lobbies': { target: GO_SERVER, changeOrigin: true },
    },
  },
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.test.ts'],
  },
}))
