import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'
import PortraitOnboardingPage from './PortraitOnboardingPage'
import { onboardingApi } from '@/lib/api/onboarding'
import type { PortraitCandidate } from '@/lib/portrait-chat-contract'

let userId = 'user-a'
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: userId } }) }))
vi.mock('@/lib/api/onboarding', () => ({ onboardingApi: { capabilities: vi.fn(), chat: vi.fn(), transcribe: vi.fn() } }))
const intro = '我是花店主，面向上班族，做公众号。'
const fact = { text: intro, certainty: 'stated' as const, evidence: [{messageId:'u-1',quote:intro}] }
const candidate: PortraitCandidate = {reply:'想写什么？',name:null,summary:null,facets:{identity:fact,audience:fact,platforms:fact,style:null,preferences:null,experience:null},creationIdea:null}
const page = () => <MemoryRouter><PortraitOnboardingPage/></MemoryRouter>
beforeEach(() => {
  sessionStorage.clear(); vi.clearAllMocks(); userId = 'user-a'
  vi.mocked(onboardingApi.capabilities).mockResolvedValue({configured:true,speech_available:false})
  vi.mocked(onboardingApi.chat).mockResolvedValue({candidate})
})
async function sendIntro() {
  fireEvent.change(screen.getByRole('textbox'), {target:{value:intro}})
  fireEvent.click(screen.getByRole('button',{name:'发送消息'}))
  await waitFor(()=>expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow','50'))
}
it('uses the formal transport and restores a per-account draft without restoring confirmation', async () => {
  const view=render(page());await sendIntro()
  expect(onboardingApi.chat).toHaveBeenCalledOnce()
  fireEvent.click(screen.getByRole('button',{name:'结束访谈，确认画像'}))
  view.unmount();render(page())
  expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow','50')
  expect(screen.getByRole('button',{name:'结束访谈，确认画像'})).toBeEnabled()
  expect(screen.queryByRole('button',{name:'去创作第一篇'})).not.toBeInTheDocument()
  expect(onboardingApi.chat).toHaveBeenCalledOnce()
})
it('clears the view on account change and ignores a previous account response',async()=>{
  let resolve!: (result:{candidate:PortraitCandidate})=>void
  vi.mocked(onboardingApi.chat).mockImplementation(()=>new Promise(done=>{resolve=done}))
  const view=render(page())
  fireEvent.change(screen.getByRole('textbox'),{target:{value:intro}})
  fireEvent.click(screen.getByRole('button',{name:'发送消息'}))
  userId='user-b';view.rerender(page())
  await act(async()=>resolve({candidate}))
  expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow','0')
  expect(sessionStorage.getItem('anban:interview-draft:user-b')).toBeNull()
})
it('starts an independent interview after reset and does not resurrect the previous portrait',async()=>{
  const view=render(page());await sendIntro()
  const old=JSON.parse(sessionStorage.getItem('anban:interview-draft:user-a')!).id
  fireEvent.click(screen.getByRole('button',{name:'从空白重新开始'}))
  fireEvent.click(screen.getByRole('button',{name:'清空并重新开始'}))
  expect(JSON.parse(sessionStorage.getItem('anban:interview-draft:user-a')!).id).not.toBe(old)
  view.unmount();render(page())
  expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow','0')
})
