import PortraitOnboarding from '@/components/projects/PortraitOnboarding'
import type { ComponentProps } from 'react'
import type { PortraitTransport } from '@/lib/portrait-transport'
const transport: PortraitTransport = {
  async capabilities(signal) {
    const [provider, voice] = await Promise.all([
      fetch('/__local-preview/portrait-chat', { signal }).then(r=>r.ok ? r.json() : null),
      fetch('/__local-preview/dictation', { signal }).then(r=>r.ok ? r.json() : null).catch(()=>null),
    ])
    return { configured: provider?.configured === true, model: provider?.model, speech_available: voice?.available === true }
  },
  async chat(messages, signal) {
    const response = await fetch('/__local-preview/portrait-chat', { method:'POST', headers:{'Content-Type':'application/json','X-Anban-Portrait-Chat':'1'}, body:JSON.stringify({messages}), signal })
    const result = await response.json()
    if (!response.ok) throw new Error(result.error || '模型暂时不可用，请重试。')
    return result
  },
}
export default function LivePortraitOnboarding(props: Omit<ComponentProps<typeof PortraitOnboarding>, 'transport'> = {}) {
  return <PortraitOnboarding {...props} transport={transport}/>
}
