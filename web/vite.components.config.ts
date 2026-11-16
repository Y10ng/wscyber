import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react(), {
    name: 'wscyber-component-preview-entry',
    enforce: 'post',
    configureServer(server) {
      server.middlewares.use((req: { url?: string }, _res, next) => {
        if (req.url === '/') req.url = '/component-preview.html'
        next()
      })
    },
    generateBundle(_options, bundle) {
      const html = bundle['component-preview.html']
      if (html) { html.fileName = 'index.html'; delete bundle['component-preview.html']; bundle['index.html'] = html }
    },
  }],
  server: { host: '127.0.0.1' },
  preview: { host: '127.0.0.1' },
  build: { outDir: 'dist-components', emptyOutDir: true, rollupOptions: { input: 'component-preview.html' } },
})
