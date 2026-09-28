import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// В режиме разработки запросы к API проксируются на запущенный конфигуратор.
const apiTarget = process.env.API_TARGET || 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1024,
  },
  server: {
    proxy: {
      '/api': apiTarget,
    },
  },
})
