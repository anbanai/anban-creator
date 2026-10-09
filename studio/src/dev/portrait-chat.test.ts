import { describe, expect, it, vi } from 'vitest'
import { acceptsPortraitRequest, analyzePortrait, validateProviderConfig } from '../../scripts/portrait-chat'
import { canConfirmPortrait, portraitChatRequest, validatePortraitCandidate, type PortraitCandidate } from '@/lib/portrait-chat-contract'

const messages = [{ id: 'u-1', role: 'user' as const, text: '我叫小林，经营花店，为上班族服务，先做公众号。' }]
const fact = { text: '经营花店', certainty: 'stated' as const, evidence: [{ messageId: 'u-1', quote: '经营花店' }] }
const candidate: PortraitCandidate = { reply: '想先帮读者解决什么问题？', name: '小林', summary: '经营花店。', facets: { identity: fact, audience: null, style: null, platforms: null, preferences: null, experience: null }, creationIdea: null }
const config = { apiKey: 'unit-test-key', baseUrl: 'https://api.deepseek.com', model: 'deepseek-flash' }
const signal = new AbortController().signal

describe('portrait parsing and provider boundary', () => {
  it('accepts partial facts and keeps optional unknowns empty', () => {
    const parsed = validatePortraitCandidate(candidate, messages)
    expect(parsed.facets.audience).toBeNull()
    expect(canConfirmPortrait(parsed)).toBe(false)
    expect(canConfirmPortrait({ ...parsed, facets: { ...parsed.facets, audience: fact, platforms: fact } })).toBe(true)
  })
  it('rejects invented evidence, assistant evidence, invented names and unreviewed properties', () => {
    expect(() => validatePortraitCandidate({ ...candidate, facets: { ...candidate.facets, identity: { ...fact, evidence: [{ messageId: 'u-1', quote: '经营了十年' }] } } }, messages)).toThrow()
    expect(() => validatePortraitCandidate(candidate, [{ ...messages[0], role: 'assistant' }])).toThrow()
    expect(() => validatePortraitCandidate({ ...candidate, name: '张三' }, messages)).toThrow()
    expect(() => validatePortraitCandidate({ ...candidate, confirmed: true }, messages)).toThrow()
  })
  it('requires distinct message ids, bounded input and a final user message', () => {
    expect(portraitChatRequest.safeParse({ messages }).success).toBe(true)
    expect(portraitChatRequest.safeParse({ messages: [messages[0], messages[0]] }).success).toBe(false)
    expect(portraitChatRequest.safeParse({ messages: [{ ...messages[0], role: 'system' }] }).success).toBe(false)
    expect(portraitChatRequest.safeParse({ messages: [{ ...messages[0], text: '长'.repeat(6001) }] }).success).toBe(false)
  })
  it('accepts a creation brief supported by more than four conversation turns, but still checks every quote', async () => {
    const inputs = ['周末家庭插花', '选花难、搭配不好看', '图文', '引导预约到店', '课程还在筹备', '微信公众号']
    const history = inputs.map((text, i) => ({ id: `u-${i + 2}`, role: 'user' as const, text }))
    const combined = { ...candidate, creationIdea: { text: '公众号图文讲周末插花的选花与搭配，结尾引导预约到店；课程仍在筹备。', certainty: 'stated' as const, evidence: history.map(m => ({messageId:m.id,quote:m.text})) } }
    const conversation = [...messages, ...history]
    const fetcher = vi.fn(async () => new Response(JSON.stringify({ choices: [{ finish_reason: 'stop', message: { content: JSON.stringify(combined) } }] })))
    expect((await analyzePortrait(config, conversation, signal, fetcher)).creationIdea?.evidence).toHaveLength(6)
    combined.creationIdea.evidence[5].quote = '从未说过的平台'
    await expect(analyzePortrait(config, conversation, signal, fetcher)).rejects.toThrow('未能对应到你的原话')
  })
  it('keeps keys server-side and disallows redirects to unofficial API hosts or plaintext', () => {
    expect(validateProviderConfig(config).model).toBe('deepseek-flash')
    for (const baseUrl of ['http://api.deepseek.com', 'https://external.example', 'https://api.deepseek.com@external.example', 'https://api.deepseek.com/?key=secret', 'https://api.deepseek.com:8443']) {
      expect(() => validateProviderConfig({ ...config, baseUrl })).toThrow()
    }
    expect(() => validateProviderConfig({ ...config, apiKey: '' })).toThrow()
    expect(() => validateProviderConfig({ ...config, model: 'arbitrary-model' })).toThrow()
  })
  it('uses bounded JSON output without tools, and validates the returned evidence', async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify({ choices: [{ finish_reason: 'stop', message: { content: JSON.stringify(candidate) } }] })))
    expect(await analyzePortrait(config, messages, signal, fetcher)).toEqual(candidate)
    const call = fetcher.mock.calls[0] as unknown as [string, RequestInit]
    expect(call[0]).toBe('https://api.deepseek.com/chat/completions')
    expect(call[1].redirect).toBe('error')
    const body = JSON.parse(call[1].body as string)
    expect(body.response_format).toEqual({ type: 'json_object' })
    expect(body.thinking).toEqual({ type: 'disabled' })
    expect(body.tools).toBeUndefined()
    expect(body.messages[0].role).toBe('system')
  })
  it('does not return upstream error bodies, secrets, malformed JSON or truncated candidates', async () => {
    await expect(analyzePortrait(config, messages, signal, async () => new Response('unit-test-key', { status: 401 }))).rejects.toThrow('DeepSeek 密钥无效')
    for (const payload of [
      { choices: [{ finish_reason: 'length', message: { content: JSON.stringify(candidate) } }] },
      { choices: [{ finish_reason: 'stop', message: { content: 'bad JSON' } }] },
      { choices: [{ finish_reason: 'stop', message: { content: JSON.stringify({ ...candidate, name: '幻觉姓名' }) } }] },
    ]) await expect(analyzePortrait(config, messages, signal, async () => new Response(JSON.stringify(payload)))).rejects.toThrow('未修改现有画像')
  })
  it('rejects oversized upstream responses', async () => {
    await expect(analyzePortrait(config, messages, signal, async () => new Response('x'.repeat(160001)))).rejects.toThrow('模型返回过长')
  })
  it('rejects cross-origin, remote and header-less POST requests', () => {
    const request = { method: 'POST', headers: { host: '127.0.0.1:5174', origin: 'http://127.0.0.1:5174', 'x-anban-portrait-chat': '1' }, socket: { remoteAddress: '127.0.0.1' } }
    expect(acceptsPortraitRequest(request)).toBe(true)
    expect(acceptsPortraitRequest({ ...request, headers: { ...request.headers, origin: 'https://external.example' } })).toBe(false)
    expect(acceptsPortraitRequest({ ...request, headers: { ...request.headers, 'x-anban-portrait-chat': undefined } })).toBe(false)
    expect(acceptsPortraitRequest({ ...request, socket: { remoteAddress: '192.168.1.2' } })).toBe(false)
  })
})
