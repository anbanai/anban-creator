import { mergeConfig } from 'vite'
import base from './vite.config'
import { localDictation } from './scripts/local-dictation'

// Explicit, loopback-only development entry. Never imported by production config.
export default mergeConfig(base, {
  plugins: [localDictation()],
  server: { host: '127.0.0.1', port: 5174, strictPort: true },
})
