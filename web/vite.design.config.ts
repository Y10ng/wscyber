import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [
    react(),
    {
      name: 'wscyber-design-preview-html-entry',
      enforce: 'post',
      configureServer(server) {
        server.middlewares.use((req: { url?: string }, _res, next) => {
          if (
            req.url &&
            !req.url.startsWith('/src') &&
            !req.url.startsWith('/@') &&
            !req.url.startsWith('/node_modules') &&
            !req.url.includes('.')
          ) {
            req.url = '/design-preview.html'
          }
          next()
        })
      },
      generateBundle(_options, bundle) {
        const htmlAsset = bundle['design-preview.html']
        if (htmlAsset) {
          htmlAsset.fileName = 'index.html'
          delete bundle['design-preview.html']
          bundle['index.html'] = htmlAsset
        }
      },
    },
  ],
  server: {
    host: '127.0.0.1',
  },
  preview: {
    host: '127.0.0.1',
  },
  build: {
    outDir: 'dist-design',
    emptyOutDir: true,
    rollupOptions: {
      input: 'design-preview.html',
    },
  },
})
