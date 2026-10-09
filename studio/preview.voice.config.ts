import { mergeConfig } from 'vite'
import base from './vite.config'
import { localDictation } from './scripts/local-dictation'
import { portraitChat } from './scripts/portrait-chat'

// Explicit, loopback-only development entry. Never imported by production config.
export default mergeConfig(base, {
  plugins: [localDictation(), portraitChat()],
  server: { host: '127.0.0.1', port: 5174, strictPort: true,
    fs: { deny: ['.env', '.env.*', '*.{crt,pem}', '**/.git/**', '**/.secrets/**'] },
  },
})
