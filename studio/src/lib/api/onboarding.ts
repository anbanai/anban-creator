import { http, unwrap } from '@/lib/http-client'
import type { PortraitTransport } from '@/lib/portrait-transport'

// Same-origin authenticated Server requests; no provider credentials in Studio.
export const onboardingApi: PortraitTransport = {
  capabilities: signal => unwrap(http.get('/onboarding/capabilities', { signal })),
  chat: (messages, signal) => unwrap(http.post('/onboarding/chat', { messages }, { signal, timeout: 105000 })),
  async transcribe(audio, signal) {
    const result = await unwrap<{ text: string }>(http.post('/onboarding/transcribe', audio, {
      headers: { 'Content-Type': audio.type }, signal, timeout: 105000,
    }))
    if (typeof result.text !== 'string') throw new Error('语音转文字未完成，请重试。')
    return result.text
  },
}
