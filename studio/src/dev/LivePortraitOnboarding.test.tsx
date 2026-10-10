import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import LivePortraitOnboarding from './LivePortraitOnboarding'
import type { PortraitCandidate } from '@/lib/portrait-chat-contract'

const intro = '我叫小林，开花店，面向上班族，先做公众号。'
const fact = (text: string) => ({ text, certainty: 'stated' as const, evidence: [{ messageId: 'u-1', quote: intro }] })
const first: PortraitCandidate = { reply: '第一篇想聊什么？', name: '小林', summary: '为上班族服务的花店主。', facets: { identity: fact('花店主'), audience: fact('上班族'), style: null, platforms: fact('公众号'), preferences: null, experience: null }, creationIdea: null }
let fetcher: ReturnType<typeof vi.fn>
let reply: () => Promise<{ ok: boolean; json: () => Promise<unknown> }>

async function send(text: string) {
  fireEvent.change(screen.getByRole('textbox'), { target: { value: text } })
  fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(''))
}

describe('live model-backed portrait onboarding', () => {
  beforeEach(() => {
    reply = async () => ({ ok: true, json: async () => ({ candidate: structuredClone(first) }) })
    fetcher = vi.fn(async (url: string, options?: RequestInit) => {
      if (options?.method === 'POST') return reply()
      return { ok: true, json: async () => url.includes('dictation') ? { available: false } : { configured: true, model: 'deepseek-flash' } }
    })
    vi.stubGlobal('fetch', fetcher)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('maps free text to sourced portrait fields and does not show scripted sample answers', async () => {
    render(<LivePortraitOnboarding />)
    await screen.findByText(/DeepSeek \/ deepseek-flash/)
    expect(screen.getByRole('progressbar',{name:'画像信息完整度'})).toHaveAttribute('aria-valuenow','0')
    expect(screen.queryByRole('button', { name: '试用示例回答' })).not.toBeInTheDocument()
    await send(intro)
    const portrait = screen.getByRole('complementary', { name: '正在形成的 IP 画像' })
    expect(within(portrait).getByText('上班族')).toBeInTheDocument()
    expect(within(portrait).getAllByText('来自你的表述')).toHaveLength(3)
    expect(screen.getByRole('button', { name: '这就是我，确认画像' })).toBeEnabled()
    expect(screen.getByRole('progressbar',{name:'画像信息完整度'})).toHaveAttribute('aria-valuenow','50')
    expect(screen.getByText('已足够开始，可以结束访谈了')).toBeInTheDocument()
    expect(screen.getByRole('button',{name:'结束访谈，确认画像'})).toBeEnabled()
    const request = fetcher.mock.calls.find(([, options]) => options?.method === 'POST')
    expect(JSON.parse(request![1].body).messages[1].text).toBe(intro)
  })
  it('replaces corrections, clears withdrawals and invalidates confirmation', async () => {
    render(<LivePortraitOnboarding />)
    await send(intro)
    fireEvent.click(screen.getByRole('button', { name: '这就是我，确认画像' }))
    expect(screen.getByRole('button', { name: '开始创作第一篇' })).toBeEnabled()
    const correction = '改为面向退休老人，平台先撤回。'
    reply = async () => ({ ok: true, json: async () => ({ candidate: { ...first, reply: '想先在哪个平台分享？', facets: { ...first.facets, audience: { text: '退休老人', certainty: 'stated', evidence: [{ messageId: 'u-2', quote: correction }] }, platforms: null } } }) })
    await send(correction)
    const portrait = screen.getByRole('complementary')
    expect(within(portrait).getByText('退休老人')).toBeInTheDocument()
    expect(within(portrait).queryByText('上班族')).not.toBeInTheDocument()
    expect(within(portrait).queryByText('公众号')).not.toBeInTheDocument()
    expect(screen.getByRole('progressbar',{name:'画像信息完整度'})).toHaveAttribute('aria-valuenow','33')
    expect(screen.queryByRole('button',{name:'结束访谈，确认画像'})).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '开始创作第一篇' })).not.toBeInTheDocument()
  })
  it('preserves input and prior portrait when the provider fails', async () => {
    render(<LivePortraitOnboarding />)
    await send(intro)
    reply = async () => ({ ok: false, json: async () => ({ error: 'DeepSeek 暂时不可用' }) })
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '读者改一下' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await screen.findByRole('alert')
    expect(screen.getByRole('textbox')).toHaveValue('读者改一下')
    expect(within(screen.getByRole('complementary')).getByText('上班族')).toBeInTheDocument()
  })
  it('rejects a response with fabricated evidence without clearing the user input', async () => {
    reply = async () => ({ ok: true, json: async () => ({ candidate: { ...first, name: '不存在的名字' } }) })
    render(<LivePortraitOnboarding />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: intro } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await screen.findByRole('alert')
    expect(screen.getByRole('textbox')).toHaveValue(intro)
    expect(screen.getByText('还没认识的你')).toBeInTheDocument()
  })
  it('ignores late responses after restarting the conversation', async () => {
    let resolve!: (value: { ok: boolean; json: () => Promise<unknown> }) => void
    reply = () => new Promise(done => { resolve = done })
    render(<LivePortraitOnboarding />)
    fireEvent.change(screen.getByRole('textbox'), { target: { value: intro } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    fireEvent.click(screen.getByRole('button', { name: '从空白重新开始' }))
    fireEvent.click(screen.getByRole('button', { name: '清空并重新开始' }))
    await act(async () => resolve({ ok: true, json: async () => ({ candidate: first }) }))
    expect(screen.getByText('还没认识的你')).toBeInTheDocument()
    expect(screen.queryByText('上班族')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveValue('')
  })
  it('can recover local conversation text without auto-sending, confirming or reusing stale portrait data', async () => {
    render(<LivePortraitOnboarding initialConversation={[
      { id: 'u-1', role: 'user', text: intro },
      { id: 'a-1', role: 'assistant', text: '还有什么补充？' },
      { id: 'u-2', role: 'user', text: '再补充一点' },
    ]} />)
    await screen.findByText(/DeepSeek \/ deepseek-flash/)
    expect(screen.getByRole('textbox')).toHaveValue('再补充一点')
    expect(screen.getByText(intro)).toBeInTheDocument()
    expect(fetcher.mock.calls.filter(([, options]) => options?.method === 'POST')).toHaveLength(0)
    expect(screen.getByText('还没认识的你')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(''))
    const request = fetcher.mock.calls.find(([, options]) => options?.method === 'POST')
    const sent = JSON.parse(request![1].body).messages
    expect(new Set(sent.map((m: {id:string}) => m.id)).size).toBe(sent.length)
    expect(sent.at(-1).text).toBe('再补充一点')
    expect(screen.getByRole('button', {name:'这就是我，确认画像'})).toBeEnabled()
    fireEvent.click(screen.getByRole('button', {name:'这就是我，确认画像'}))
    fireEvent.click(screen.getByRole('button', {name:'开始创作第一篇'}))
    fireEvent.click(screen.getByRole('button', {name:'回到对话继续调整'}))
    expect(screen.getByRole('textbox')).toHaveValue('')
    fireEvent.click(screen.getByRole('button', { name: '从空白重新开始' }))
    fireEvent.click(screen.getByRole('button', { name: '清空并重新开始' }))
    expect(screen.getByRole('textbox')).toHaveValue('')
    expect(screen.queryByText(intro)).not.toBeInTheDocument()
  })
})
