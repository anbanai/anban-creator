import { defineConfig, mergeConfig } from 'vite'
import base from './vite.config'
import { localDictation } from './scripts/local-dictation'
import { portraitChat } from './scripts/portrait-chat'

// Explicit opt-in: this development entry writes to the real Anban account.
// Authentication stays in the normal Studio flow; no credentials in this file.
export default defineConfig((env) => mergeConfig(base(env), {
  define: { 'import.meta.env.VITE_API_BASE_URL': JSON.stringify('https://api-creator.anbanai.com/api/v1') },
  plugins: [localDictation(5175), portraitChat(5175)],
  server: {
    host: '127.0.0.1', port: 5175, strictPort: true,
    // Keep an in-progress conversation stable while editing source files.
    hmr: false,
    fs: { deny: ['.env', '.env.*', '*.{crt,pem}', '**/.git/**', '**/.secrets/**'] },
  },
}))
