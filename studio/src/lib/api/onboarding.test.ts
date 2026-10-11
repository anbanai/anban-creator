import { expect, it, vi } from 'vitest'
import { http } from '@/lib/http-client'
import { onboardingApi } from './onboarding'
vi.mock('@/lib/http-client',()=>({http:{get:vi.fn(),post:vi.fn()},unwrap:async (p:Promise<{data:{data:unknown}}>)=>(await p).data.data}))
it('uses the authenticated client and preserves cancellation for speech and chat',async()=>{
  const signal=new AbortController().signal
  vi.mocked(http.get).mockResolvedValue({data:{data:{configured:true,speech_available:true}}})
  await onboardingApi.capabilities(signal)
  expect(http.get).toHaveBeenCalledWith('/onboarding/capabilities',{signal})
  vi.mocked(http.post).mockResolvedValue({data:{data:{text:'测试文字'}}})
  const audio=new Blob(['audio'],{type:'audio/webm'})
  expect(await onboardingApi.transcribe!(audio,signal)).toBe('测试文字')
  expect(http.post).toHaveBeenCalledWith('/onboarding/transcribe',audio,expect.objectContaining({signal,timeout:105000,headers:{'Content-Type':'audio/webm'}}))
  await onboardingApi.chat([],signal)
  expect(http.post).toHaveBeenCalledWith('/onboarding/chat',{messages:[]},expect.objectContaining({signal,timeout:105000}))
})
