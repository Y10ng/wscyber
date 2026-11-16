import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [
    react(),
    {
      name: 'wscyber-react-html-entry',
      enforce: 'post',
      configureServer(server) {
        server.middlewares.use((req: { url?: string }, _res, next) => {
          if (req.url && !req.url.startsWith('/api') && !req.url.startsWith('/healthz') && !req.url.startsWith('/src') && !req.url.startsWith('/@') && !req.url.startsWith('/node_modules') && !req.url.includes('.')) {
            req.url = '/react.html'
          }
          next()
        })
      },
      generateBundle(_options, bundle) {
        const htmlAsset = bundle['react.html']
        if (htmlAsset) {
          htmlAsset.fileName = 'index.html'
          delete bundle['react.html']
          bundle['index.html'] = htmlAsset
        }
      },
    },
  ],
  server: {
    host: '127.0.0.1',
    proxy: {
      '/api': { target: 'http://127.0.0.1:8790', changeOrigin: false },
      '/healthz': { target: 'http://127.0.0.1:8790', changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist-react',
    emptyOutDir: true,
    rollupOptions: {
      input: 'react.html',
    },
  },
})
