import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// El build va a web/dist, que es lo que embebe el binario de Go.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      // En desarrollo el frontend llama al panel en :8080.
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
