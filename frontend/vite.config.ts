import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const devHost = process.env.VITE_DEV_HOST || '0.0.0.0'
const devPort = Number(process.env.VITE_DEV_PORT || 5172)
const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8081'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  server: {
    host: devHost,
    port: devPort,
    strictPort: true,
    proxy: {
      '/api': {
        target: apiProxyTarget,
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
