import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

const GO_SERVER = 'http://localhost:8080'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    host: true,
    allowedHosts: true,
    proxy: {
      '/ws': { target: GO_SERVER, ws: true, changeOrigin: true },
      '/health': { target: GO_SERVER, changeOrigin: true },
      '/catalog': { target: GO_SERVER, changeOrigin: true },
      '/maps': { target: GO_SERVER, changeOrigin: true },
      '/matches': { target: GO_SERVER, changeOrigin: true },
    },
  },
})
