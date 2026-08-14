import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export function manualChunks(id: string) {
  const normalizedId = id.replace(/\\/g, '/')

  if (!normalizedId.includes('node_modules')) {
    return undefined
  }

  if (
    normalizedId.includes('/@base-ui/react/') ||
    normalizedId.includes('/@base-ui/utils/') ||
    normalizedId.includes('/@floating-ui/') ||
    normalizedId.includes('/cmdk/') ||
    normalizedId.includes('/vaul/') ||
    normalizedId.includes('/sonner/') ||
    normalizedId.includes('/react-remove-scroll/') ||
    normalizedId.includes('/react-remove-scroll-bar/') ||
    normalizedId.includes('/aria-hidden/') ||
    normalizedId.includes('/use-callback-ref/') ||
    normalizedId.includes('/use-sidecar/') ||
    normalizedId.includes('/react-style-singleton/') ||
    normalizedId.includes('/get-nonce/') ||
    normalizedId.includes('/@radix-ui/')
  ) {
    return 'vendor-ui'
  }

  if (
    normalizedId.includes('/motion/') ||
    normalizedId.includes('/motion-dom/') ||
    normalizedId.includes('/motion-utils/') ||
    normalizedId.includes('/framer-motion/') ||
    normalizedId.includes('/gsap/') ||
    normalizedId.includes('/@gsap/react/')
  ) {
    return 'vendor-animation'
  }

  if (
    normalizedId.includes('/react/') ||
    normalizedId.includes('/react-dom/') ||
    normalizedId.includes('/react-router/') ||
    normalizedId.includes('/react-router-dom/') ||
    normalizedId.includes('/react-is/') ||
    normalizedId.includes('/scheduler/') ||
    normalizedId.includes('/use-sync-external-store/')
  ) {
    return 'vendor-react'
  }

  if (
    normalizedId.includes('/@tanstack/react-query/') ||
    normalizedId.includes('/@tanstack/query-core/') ||
    normalizedId.includes('/axios/')
  ) {
    return 'vendor-data'
  }

  if (
    normalizedId.includes('/react-hook-form/') ||
    normalizedId.includes('/@hookform/resolvers/') ||
    normalizedId.includes('/zod/')
  ) {
    return 'vendor-forms'
  }

  if (
    normalizedId.includes('/recharts/') ||
    normalizedId.includes('/d3-') ||
    normalizedId.includes('/victory-vendor/') ||
    normalizedId.includes('/@reduxjs/toolkit/') ||
    normalizedId.includes('/react-redux/') ||
    normalizedId.includes('/redux/') ||
    normalizedId.includes('/reselect/') ||
    normalizedId.includes('/immer/') ||
    normalizedId.includes('/decimal.js-light/') ||
    normalizedId.includes('/eventemitter3/') ||
    normalizedId.includes('/tiny-invariant/')
  ) {
    return 'vendor-charts'
  }

  if (
    normalizedId.includes('/streamdown/') ||
    normalizedId.includes('/react-markdown/') ||
    normalizedId.includes('/remark-gfm/') ||
    normalizedId.includes('/remark-') ||
    normalizedId.includes('/rehype-') ||
    normalizedId.includes('/mdast-util-') ||
    normalizedId.includes('/hast-util-') ||
    normalizedId.includes('/micromark') ||
    normalizedId.includes('/unist-util-') ||
    normalizedId.includes('/property-information/') ||
    normalizedId.includes('/vfile/') ||
    normalizedId.includes('/unified/') ||
    normalizedId.includes('/marked/') ||
    normalizedId.includes('/hastscript/') ||
    normalizedId.includes('/@ungap/structured-clone/')
  ) {
    return 'vendor-markdown'
  }

  if (normalizedId.includes('/mermaid/') || normalizedId.includes('/ai/')) {
    return 'vendor-ai'
  }

  if (normalizedId.includes('/lucide-react/')) {
    return 'vendor-icons'
  }

  if (
    normalizedId.includes('/embla-carousel/') ||
    normalizedId.includes('/embla-carousel-react/') ||
    normalizedId.includes('/react-resizable-panels/') ||
    normalizedId.includes('/input-otp/') ||
    normalizedId.includes('/next-themes/')
  ) {
    return 'vendor-interaction'
  }

  if (
    normalizedId.includes('/date-fns/') ||
    normalizedId.includes('/es-toolkit/') ||
    normalizedId.includes('/clsx/') ||
    normalizedId.includes('/tailwind-merge/') ||
    normalizedId.includes('/class-variance-authority/')
  ) {
    return 'vendor-utils'
  }

  return 'vendor'
}

export function rewriteAgentGuideURL(url: string | undefined): string | undefined {
  const match = url?.match(/^\/(claude|codex)\/?(\?.*)?$/)
  if (!match) return url
  return `/${match[1]}/index.html${match[2] ?? ''}`
}

function agentGuideRoutes(): Plugin {
  const rewrite = (req: { url?: string }, _res: unknown, next: () => void) => {
    req.url = rewriteAgentGuideURL(req.url)
    next()
  }
  return {
    name: 'agent-guide-routes',
    configureServer(server) {
      server.middlewares.use(rewrite)
    },
    configurePreviewServer(server) {
      server.middlewares.use(rewrite)
    },
  }
}

export default defineConfig({
  plugins: [agentGuideRoutes(), react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
  build: {
    rolldownOptions: {
      output: {
        manualChunks,
      },
    },
  },
})
