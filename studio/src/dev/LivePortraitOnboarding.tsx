import { useEffect, useRef, useState } from 'react'
import { ArrowLeft } from 'lucide-react'
import { PortraitConversation, portraitFacets, type PortraitDraft, type PortraitMessage } from '@/components/projects/PortraitConversation'
import { canConfirmPortrait, portraitChatRequest, validatePortraitCandidate, type PortraitCandidate, type PortraitChatMessage } from '@/lib/portrait-chat-contract'

const welcome: PortraitMessage = { id: 'welcome', role: 'assistant', text: '你好，很高兴认识你。你平时在做什么？最近为什么想开始做内容？\n\n打字或用语音说都可以。从你最想聊的地方开始。' }
type Connection = { configured: boolean; model?: string; error?: string }

export default function LivePortraitOnboarding({ initialConversation }: { initialConversation?: PortraitChatMessage[] } = {}) {
  // Optional local-preview recovery input. Never trust a recovered portrait or
  // confirmation; restore text only and wait for the user to send it for analysis.
  const [recovered] = useState(() => initialConversation ? portraitChatRequest.parse({ messages: initialConversation }).messages : null)
  const [messages, setMessages] = useState<PortraitMessage[]>(recovered?.slice(0, -1) ?? [welcome])
  const [recoveredInput, setRecoveredInput] = useState(recovered ? recovered[recovered.length - 1].text : '')
  const [candidate, setCandidate] = useState<PortraitCandidate | null>(null)
  const [connection, setConnection] = useState<Connection>({ configured: false })
  const [checking, setChecking] = useState(false)
  const [localVoice, setLocalVoice] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [creating, setCreating] = useState(false)
  const [resetPrompt, setResetPrompt] = useState(false)
  const [epoch, setEpoch] = useState(0)
  const revision = useRef(Math.max(0, ...(recovered ?? []).map(message => Number(/^[ua]-(\d+)$/.exec(message.id)?.[1] ?? 0))))
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
      const [provider, voice] = await Promise.all([
        fetch('/__local-preview/portrait-chat', { signal: controller.signal }).then(response => response.ok ? response.json() : null),
        fetch('/__local-preview/dictation', { signal: controller.signal }).then(response => response.ok ? response.json() : null).catch(() => null),
      ])
      if (controller.signal.aborted) return
      setConnection(provider?.configured === true ? { configured: true, model: provider.model } : { configured: false, error: provider?.error || '请启用本机对话服务并填写 DeepSeek 配置。' })
      setLocalVoice(voice?.available === true)
    } catch {
      if (!controller.signal.aborted) setConnection({ configured: false, error: '本机对话服务尚未连接，请检查后重试。' })
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
    // An attempted correction cannot leave an older confirmation actionable.
    setConfirmed(false)
    try {
      const response = await fetch('/__local-preview/portrait-chat', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Anban-Portrait-Chat': '1' },
        body: JSON.stringify({ messages: conversation }), signal: controller.signal,
      })
      const result = await response.json()
      if (!response.ok) throw new Error(typeof result.error === 'string' ? result.error : '模型暂时不可用，请重试。')
      let next: PortraitCandidate
      try { next = validatePortraitCandidate(result.candidate, conversation) }
      catch { throw new Error('返回的画像未通过核对，原有文字和画像已保留，请重试。') }
      if (generation.current !== requestGeneration || controller.signal.aborted) throw new Error('cancelled')
      const changed = portraitFacets.filter(([key]) => (candidate?.facets[key]?.text ?? null) !== (next.facets[key]?.text ?? null)).map(([, label]) => label)
      revision.current++
      setCandidate(next)
      setRecoveredInput('')
      setMessages([...conversation, { id: `a-${revision.current}`, role: 'assistant', text: next.reply, update: changed.length ? `已更新：${changed.join('、')}` : undefined }])
      setConnection(previous => ({ ...previous, configured: true }))
    } catch (problem) {
      if (generation.current === requestGeneration) setError(problem instanceof Error && problem.name !== 'AbortError' && problem.message !== 'cancelled' ? problem.message : '这次解析未完成，文字与画像已保留，可以重试。')
      throw problem
    } finally {
      clearTimeout(timeout)
      if (activeRequest.current === controller) { activeRequest.current = null; setBusy(false) }
    }
  }

  function reset() {
    generation.current++
    activeRequest.current?.abort(); activeRequest.current = null
    revision.current = 0
    setMessages([welcome]); setCandidate(null); setConfirmed(false); setBusy(false)
    setRecoveredInput('')
    setError(''); setCreating(false); setResetPrompt(false); setEpoch(value => value + 1)
  }

  const draft: PortraitDraft = {}
  for (const [key] of portraitFacets) {
    const fact = candidate?.facets[key]
    if (fact) draft[key] = { text: fact.text, source: fact.certainty === 'stated' ? '来自你的表述' : confirmed ? 'AI 归纳 · 已确认' : 'AI 推断 · 待确认', evidence: fact.evidence.map(item => item.quote) }
  }
  const ready = canConfirmPortrait(candidate)

  return <div className="portrait-preview">
    <div className="portrait-preview-bar"><span>真实对话 · {connection.configured ? `DeepSeek${connection.model ? ` / ${connection.model}` : ''}` : '等待 API 配置'}</span><span>发送后的文字和本次对话由 DeepSeek 理解；画像仅在本次预览中保存</span></div>
    <nav className="portrait-preview-nav" aria-label="预览导航"><strong>案板 <span>让好内容，从认识你开始</span></strong><button onClick={() => setResetPrompt(true)}>从空白重新开始</button></nav>
    {!connection.configured && <div className="portrait-reset" role="status"><span>{checking ? '正在检查本机 API 配置…' : connection.error || '填写本机 DeepSeek 配置后，即可开始真实对话。'}</span><button disabled={checking || busy} onClick={() => void refreshConfiguration()}>检查 API 配置</button></div>}
    {resetPrompt && <div className="portrait-reset" role="alert"><span>重新开始会清空本次对话、画像和未发送文字。</span><button onClick={() => setResetPrompt(false)}>继续当前对话</button><button onClick={reset}>清空并重新开始</button></div>}
    {creating ? <section className="portrait-next" tabIndex={-1} ref={nextRef} aria-label="已确认的创作简报">
      <span className="portrait-eyebrow">画像已确认 · 下一步创作</span>
      <h2>{candidate?.creationIdea?.text || '一起选择第一篇的选题'}</h2>
      <p>{candidate?.summary}</p>
      <p>面向：{candidate?.facets.audience?.text}<br />发布平台：{candidate?.facets.platforms?.text}</p>
      <p>这份简报来自刚才的真实对话。本机预览已完成画像整理；作品生成和正式项目保存尚未接入，没有创建或发布作品。</p>
      <button className="portrait-primary" onClick={() => setCreating(false)}><ArrowLeft size={15} />回到对话继续调整</button>
    </section> : <PortraitConversation key={epoch} messages={messages} draft={draft} name={candidate?.name ?? undefined} summary={candidate?.summary ?? undefined}
      creationIdea={candidate?.creationIdea?.text} localVoice={localVoice} initialInput={recoveredInput} busy={busy} error={error} ready={ready} confirmed={confirmed}
      onSend={send} onConfirm={() => { if (ready && !busy) setConfirmed(true) }} onCreate={() => { if (ready && confirmed && !busy) setCreating(true) }} />}
  </div>
}
