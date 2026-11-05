import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { loadConfigFromFile } from 'vite'
import { acceptsLocalVoice } from '../../scripts/local-dictation'
import { acceptsPortraitRequest } from '../../scripts/portrait-chat'
describe('explicit online preview boundary',()=>{
  it('calls the independent HTTPS API and leaves the normal dev config alone',()=>{
    const config=readFileSync(resolve(process.cwd(),'preview.online.config.ts'),'utf8')
    expect(config).toContain("host: '127.0.0.1', port: 5175, strictPort: true")
    expect(config).toContain("'import.meta.env.VITE_API_BASE_URL': JSON.stringify('https://api-creator.anbanai.com/api/v1')")
    expect(config).toContain('**/.secrets/**')
    expect(readFileSync(resolve(process.cwd(),'vite.config.ts'),'utf8')).not.toContain('https://creator.anbanai.com')
  })
  it('loads the online preview with no service proxies', async () => {
    const loaded = await loadConfigFromFile({ command: 'serve', mode: 'development' }, resolve(process.cwd(), 'preview.online.config.ts'))
    expect(loaded?.config.server?.proxy).toBeUndefined()
    expect(loaded?.config.define?.['import.meta.env.VITE_API_BASE_URL']).toBe(JSON.stringify('https://api-creator.anbanai.com/api/v1'))
  })
  it('binds each AI/voice endpoint to its own port and rejects cross-preview POSTs',()=>{
    const request={method:'POST',headers:{host:'127.0.0.1:5175',origin:'http://127.0.0.1:5175','x-anban-portrait-chat':'1','x-anban-local-voice':'1'},socket:{remoteAddress:'127.0.0.1'}}
    expect(acceptsPortraitRequest(request,5175)).toBe(true)
    expect(acceptsLocalVoice(request,5175)).toBe(true)
    expect(acceptsPortraitRequest(request)).toBe(false)
    expect(acceptsLocalVoice(request)).toBe(false)
    const cross={...request,headers:{...request.headers,origin:'http://127.0.0.1:5174'}}
    expect(acceptsPortraitRequest(cross,5175)).toBe(false)
    expect(acceptsLocalVoice(cross,5175)).toBe(false)
  })
})
