import { describe, expect, it } from 'vitest'

import { manualChunks } from '../../vite.config'

describe('manualChunks', () => {
  it.each([
    ['/project/node_modules/react/index.js', 'vendor-react'],
    ['/project/node_modules/react-dom/client.js', 'vendor-react'],
    ['/project/node_modules/react-is/index.js', 'vendor-react'],
    ['/project/node_modules/use-sync-external-store/shim/index.js', 'vendor-react'],
    ['/project/node_modules/@tanstack/react-query/build/modern/index.js', 'vendor-data'],
    ['/project/node_modules/axios/index.js', 'vendor-data'],
    ['/project/node_modules/recharts/es6/index.js', 'vendor-charts'],
    ['/project/node_modules/d3-scale/src/index.js', 'vendor-charts'],
    ['/project/node_modules/react-markdown/index.js', 'vendor-markdown'],
    ['/project/node_modules/streamdown/dist/index.js', 'vendor-markdown'],
    ['/project/node_modules/mermaid/dist/mermaid.core.mjs', 'vendor-ai'],
    ['/project/node_modules/lucide-react/dist/esm/index.js', 'vendor-icons'],
    ['/project/node_modules/motion/dist/es/index.mjs', 'vendor-animation'],
    ['/project/node_modules/motion-dom/dist/es/index.mjs', 'vendor-animation'],
    ['/project/node_modules/motion-utils/dist/es/index.mjs', 'vendor-animation'],
    ['/project/node_modules/framer-motion/dist/es/index.mjs', 'vendor-animation'],
    ['/project/node_modules/gsap/index.js', 'vendor-animation'],
    ['/project/node_modules/@base-ui/react/dist/index.js', 'vendor-ui'],
    ['/project/node_modules/@base-ui/utils/dist/index.js', 'vendor-ui'],
    ['/project/node_modules/@floating-ui/react-dom/dist/floating-ui.react-dom.mjs', 'vendor-ui'],
    ['/project/node_modules/cmdk/dist/index.mjs', 'vendor-ui'],
    ['/project/node_modules/vaul/dist/index.mjs', 'vendor-ui'],
    ['/project/node_modules/sonner/dist/index.mjs', 'vendor-ui'],
    ['/project/node_modules/react-remove-scroll/dist/es5/index.js', 'vendor-ui'],
    ['/project/node_modules/react-remove-scroll-bar/dist/es2015/index.js', 'vendor-ui'],
    ['/project/node_modules/aria-hidden/dist/es2015/index.js', 'vendor-ui'],
    ['/project/node_modules/react-hook-form/dist/index.esm.mjs', 'vendor-forms'],
    ['/project/node_modules/zod/v4/index.js', 'vendor-forms'],
    ['/project/node_modules/date-fns/index.js', 'vendor-utils'],
    ['/project/node_modules/es-toolkit/dist/index.js', 'vendor-utils'],
    ['/project/node_modules/@tanstack/query-core/build/modern/index.js', 'vendor-data'],
    ['/project/node_modules/mdast-util-to-markdown/lib/index.js', 'vendor-markdown'],
    ['/project/node_modules/micromark/index.js', 'vendor-markdown'],
    ['/project/node_modules/hast-util-raw/lib/index.js', 'vendor-markdown'],
    ['/project/node_modules/embla-carousel/esm/embla-carousel.esm.js', 'vendor-interaction'],
    ['/project/node_modules/embla-carousel-react/esm/embla-carousel-react.esm.js', 'vendor-interaction'],
    ['/project/node_modules/react-resizable-panels/dist/react-resizable-panels.js', 'vendor-interaction'],
    ['/project/node_modules/input-otp/dist/index.mjs', 'vendor-interaction'],
    ['/project/node_modules/next-themes/dist/index.mjs', 'vendor-interaction'],
  ])('assigns %s to %s', (id, expected) => {
    expect(manualChunks(id)).toBe(expected)
  })

  it('leaves application modules in route chunks', () => {
    expect(manualChunks('/project/src/pages/DashboardPage.tsx')).toBeUndefined()
  })

  it('normalizes Windows module paths before assigning chunks', () => {
    expect(manualChunks('C:\\project\\node_modules\\react\\index.js')).toBe('vendor-react')
  })
})
