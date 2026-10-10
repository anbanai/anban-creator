import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { getApiErrorMessage } from '@/lib/http-client'
import type { PortraitTransport } from '@/lib/portrait-transport'
import { ArrowLeft } from 'lucide-react'
import { PortraitConversation, portraitFacets, type PortraitDraft, type PortraitMessage } from '@/components/projects/PortraitConversation'
import { isPortraitReadyForCreation, emptyPortraitCandidate, portraitChatRequest, portraitConversationSchema, validatePortraitCandidate, type PortraitCandidate, type PortraitChatMessage } from '@/lib/portrait-chat-contract'

const OnlinePortraitDelivery = lazy(()=>import('./PortraitDelivery'))

const welcome: PortraitMessage = { id: 'welcome', role: 'assistant', text: '你好，很高兴认识你。你平时在做什么？最近为什么想开始做内容？\n\n打字或用语音说都可以。从你最想聊的地方开始。' }
type Connection = { configured: boolean; model?: string; error?: string }

export default function PortraitOnboarding({ initialConversation, initialSession, onlineDelivery = false, transport, production = false, sessionId, onSessionChange, onReset }: {
  transport: PortraitTransport
  production?: boolean
  sessionId?: string
  onSessionChange?: (session: { messages: PortraitChatMessage[]; candidate: PortraitCandidate }) => void
  onReset?: () => void
  initialConversation?: PortraitChatMessage[]
  initialSession?: { messages: PortraitChatMessage[]; candidate: PortraitCandidate }
  onlineDelivery?: boolean
}) {
  // Recovery inputs are local only: text is re-analysed, or a full candidate is
  // validated against its source quotes. Neither path restores confirmation.
  const [recovered] = useState(() => initialConversation ? portraitChatRequest.parse({ messages: initialConversation }).messages : null)
  const [session] = useState(()=>{
    if(!initialSession) return null
    const restored = portraitConversationSchema.parse(initialSession.messages)
    return {messages:restored,candidate:validatePortraitCandidate(initialSession.candidate,restored)}
  })
  const [messages, setMessages] = useState<PortraitMessage[]>(session?.messages ?? recovered?.slice(0, -1) ?? [welcome])
  const [recoveredInput, setRecoveredInput] = useState(recovered ? recovered[recovered.length - 1].text : '')
  const [candidate, setCandidate] = useState<PortraitCandidate | null>(session?.candidate ?? null)
  const [connection, setConnection] = useState<Connection>({ configured: false })
  const [checking, setChecking] = useState(false)
  const [localVoice, setLocalVoice] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [creationIdeaInput, setCreationIdeaInput] = useState<string | undefined>()
  const [resetPrompt, setResetPrompt] = useState(false)
  const [epoch, setEpoch] = useState(0)
  const revision = useRef(Math.max(0, ...(session?.messages ?? recovered ?? []).map(message => Number(/^[ua]-(\d+)$/.exec(message.id)?.[1] ?? 0))))
  const generation = useRef(0)
  const activeRequest = useRef<AbortController | null>(null)
  const statusRequest = useRef<AbortController | null>(null)
  const nextRef = useRef<HTMLElement>(null)

  async function refreshConfiguration() {
    statusRequest.current?.abort()
    const controller = new AbortController()
    statusRequest.current = controller
    const timeout = setTimeout(() => controller.abort(), 10000)
    setChecking(true)
    try {
      const provider = await transport.capabilities(controller.signal)
      if (controller.signal.aborted) return
      setConnection(provider.configured ? { configured: true, model: provider.model } : { configured: false, error: production ? '访谈服务暂未开放，可以先从项目页创建。' : '请启用本机对话服务并填写 DeepSeek 配置。' })
      setLocalVoice(provider.speech_available)

    } catch {
      if (!controller.signal.aborted) setConnection({ configured: false, error: '对话服务尚未连接，请稍后重试。' })
    } finally {
      clearTimeout(timeout)
      if (statusRequest.current === controller) { statusRequest.current = null; setChecking(false) }
    }
  }

  useEffect(() => {
    void refreshConfiguration()
    return () => { generation.current++; activeRequest.current?.abort(); statusRequest.current?.abort() }
  }, [])

  useEffect(() => {
    if (messages.length === 1 && !busy) return
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [messages.length, busy])
  useEffect(() => { if (creating) nextRef.current?.focus() }, [creating])

  async function send(text: string) {
    if (activeRequest.current) throw new Error('busy')
    const controller = new AbortController()
    activeRequest.current = controller
    const requestGeneration = generation.current
    const timeout = setTimeout(() => controller.abort(), 105000)
    const conversation: PortraitChatMessage[] = [...messages.map(({ id, role, text: content }) => ({ id, role, text: content })), { id: `u-${revision.current + 1}`, role: 'user', text }]
    setError(''); setBusy(true)
    try {
      if (!portraitChatRequest.safeParse({ messages: conversation }).success) throw new Error('输入或对话已较长，请缩短本次文字，或先确认保存画像。')
      const result = await transport.chat(conversation, controller.signal)
      let next: PortraitCandidate
      try { next = validatePortraitCandidate(result.candidate, conversation) }
      catch { throw new Error('返回的画像未通过核对，原有文字和画像已保留，请重试。') }
      if (generation.current !== requestGeneration || controller.signal.aborted) throw new Error('cancelled')
      const changed = portraitFacets.filter(([key]) => (candidate?.facets[key]?.text ?? null) !== (next.facets[key]?.text ?? null)).map(([, label]) => label)
      revision.current++
      setCandidate(next)
      if (next.creationIdea?.text !== candidate?.creationIdea?.text) setCreationIdeaInput(undefined)
      setRecoveredInput('')
      const updated: PortraitMessage[] = [...conversation, { id: `a-${revision.current}`, role: 'assistant', text: next.reply, update: changed.length ? `已更新：${changed.join('、')}` : undefined }]
      setMessages(updated)
      onSessionChange?.({ messages: updated.map(({id, role, text}) => ({id, role, text})), candidate: next })
      setConnection(previous => ({ ...previous, configured: true }))
    } catch (problem) {
      if (generation.current === requestGeneration) setError(getApiErrorMessage(problem, '这次解析未完成，文字与画像已保留，可以重试。'))
      throw problem
    } finally {
      clearTimeout(timeout)
      if (activeRequest.current === controller) { activeRequest.current = null; setBusy(false) }
    }
  }

  function reset() {
    onReset?.()
    generation.current++
    activeRequest.current?.abort(); activeRequest.current = null
    revision.current = 0
    setMessages([welcome]); setCandidate(null); setBusy(false)
    setRecoveredInput('')
    setError(''); setCreating(false); setCreationIdeaInput(undefined); setResetPrompt(false); setEpoch(value => value + 1)
  }

  const draft: PortraitDraft = {}
  for (const [key] of portraitFacets) {
    const fact = candidate?.facets[key]
    if (fact) draft[key] = { text: fact.text, source: fact.certainty === 'stated' ? '来自你的表述' : 'AI 归纳 · 可纠正', evidence: fact.evidence.map(item => item.quote) }
  }
  const ready = isPortraitReadyForCreation(candidate)

  return <div className="portrait-preview">
    <div className="portrait-preview-bar"><span>{production ? '认识你 · 整理画像 · 开始创作' : `真实对话 · DeepSeek${connection.model ? ` / ${connection.model}` : ''}`}</span><span>{production ? '画像随对话更新；点击保存才会写入项目。' : '本机新界面 · 确认后可保存项目并生成作品'}</span></div>
    <nav className="portrait-preview-nav" aria-label="画像访谈导航"><strong>Anban <span>让好内容，从认识你开始</span></strong><button onClick={() => setResetPrompt(true)}>从空白重新开始</button></nav>
    {!connection.configured && <div className="portrait-reset" role="status"><span>{checking ? '正在连接…' : connection.error || (production ? '正在准备访谈服务…' : '填写本机 DeepSeek 配置后，即可开始真实对话。')}</span><button disabled={checking || busy} onClick={() => void refreshConfiguration()}>重新连接</button></div>}
    {resetPrompt && <div className="portrait-reset" role="alert"><span>重新开始会清空本次对话、画像和未发送文字。</span><button onClick={() => setResetPrompt(false)}>继续当前对话</button><button onClick={reset}>清空并重新开始</button></div>}
    {creating ? <section className="portrait-next" tabIndex={-1} ref={nextRef} aria-label="本次创作简报">
      <span className="portrait-eyebrow">带着当前画像 · 开始创作</span>
      <h2>{candidate?.creationIdea?.text || '一起选择第一篇的选题'}</h2>
      <p>{candidate?.summary || '还没有完整的画像也没关系，先说说这次想创作什么。'}</p>
      <p>面向：{candidate?.facets.audience?.text || '待补充'}<br />发布平台：{candidate?.facets.platforms?.text || '可在下方选择'}</p>
      {onlineDelivery && candidate ? <Suspense fallback={<p>正在连接交付流程…</p>}><OnlinePortraitDelivery candidate={candidate} messages={messages} sessionId={sessionId} production={production} initialIdea={creationIdeaInput} onIdeaChange={setCreationIdeaInput} /></Suspense>
        : <p>这份简报来自刚才的真实对话。本机预览已完成画像整理；作品生成和正式项目保存尚未接入，没有创建或发布作品。</p>}
      <button className="portrait-primary" onClick={() => setCreating(false)}><ArrowLeft size={15} />回到对话继续调整</button>
    </section> : <PortraitConversation key={epoch} messages={messages} draft={draft} name={candidate?.name ?? undefined} summary={candidate?.summary ?? undefined}
      creationIdea={candidate?.creationIdea?.text} localVoice={!production && localVoice} transcribe={production && localVoice ? transport.transcribe : undefined} initialInput={recoveredInput} busy={busy} error={error} ready={ready}
      onSend={send} onCreate={() => { if (!busy) { if (!candidate) setCandidate(emptyPortraitCandidate()); setCreating(true) } }} />}
  </div>
}
