import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

export default defineConfig({
  plugins: [vue(), {
    name: 'keep-go-embed-placeholder',
    closeBundle() {
      writeFileSync(fileURLToPath(new URL('./dist/.keep', import.meta.url)), 'Build the frontend before packaging: npm ci && npm run build\n')
    },
  }],
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    port: 5173,
    proxy: { '/api': { target: 'http://127.0.0.1:18990', changeOrigin: false } },
  },
})
