import { useEffect, useRef, useState } from 'react'
import { ArrowUp, ArrowRight, Check, MessageCircle, Mic, Square, Sparkles, UserRound } from 'lucide-react'
import { useBrowserDictation } from '@/hooks/useBrowserDictation'
import { useLocalDictation } from '@/hooks/useLocalDictation'
import './portrait-conversation.css'

export const portraitFacets = [
  ['identity', '我是谁'], ['audience', '为谁创作'], ['style', '怎样表达'],
  ['platforms', '在哪里分享'], ['preferences', '坚持与边界'], ['experience', '独有经历'],
] as const

export type PortraitFacetKey = typeof portraitFacets[number][0]
export type PortraitDraft = Partial<Record<PortraitFacetKey, { text: string; source: string }>>
export type PortraitMessage = { id: string; role: 'user' | 'assistant'; text: string; update?: string }

interface PortraitConversationProps {
  messages: PortraitMessage[]
  draft: PortraitDraft
  name?: string
  summary?: string
  creationIdea?: string
  suggestion?: string
  busy?: boolean
  error?: string
  ready: boolean
  confirmed: boolean
  localVoice?: boolean
  onSend: (text: string) => Promise<void>
  onConfirm: () => void
  onCreate: () => void
}

/** Presentation only. The caller owns conversation transport, revisions and persistence. */
export function PortraitConversation({
  messages, draft, name, summary, creationIdea, suggestion, busy = false, error,
  ready, confirmed, localVoice: preferLocalVoice = false, onSend, onConfirm, onCreate,
}: PortraitConversationProps) {
  const [input, setInput] = useState('')
  const [sendError, setSendError] = useState('')
  const [sending, setSending] = useState(false)
  const sendingRef = useRef(false)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const messagesRef = useRef<HTMLDivElement>(null)
  const count = portraitFacets.filter(([key]) => draft[key]).length
  const locked = busy || sending
  const browserVoice = useBrowserDictation(setInput)
  const localDictation = useLocalDictation(setInput)
  const localVoice = preferLocalVoice && !browserVoice.active
  const voice = localVoice ? localDictation : browserVoice

  useEffect(() => {
    const list = messagesRef.current
    if (list) list.scrollTop = list.scrollHeight
  }, [messages, busy])

  useEffect(() => {
    if (!input.trim() && !voice.active) return
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [input, voice.active])

  async function send() {
    const text = input.trim()
    if (!text || busy || sendingRef.current || voice.active) return
    sendingRef.current = true
    setSending(true)
    setSendError('')
    try {
      await onSend(text)
      setInput('')
    } catch {
      setSendError('这句话没有发送成功，文字已保留，可以重新发送。')
    } finally {
      sendingRef.current = false
      setSending(false)
      inputRef.current?.focus()
    }
  }

  return <div className="portrait-workspace">
    <section className="portrait-chat" aria-label="聊出你的 IP">
      <header className="portrait-chat-heading">
        <span className="portrait-eyebrow">从一次聊天开始</span>
        <h1>聊一聊，让你的 IP 清晰起来。</h1>
        <p>说说你的故事。我们一起找到值得被看见的部分。</p>
      </header>
      <div className="portrait-messages" ref={messagesRef} role="log" aria-label="对话记录" aria-live="polite">
        {messages.map(message => <article key={message.id} className={`portrait-message ${message.role}`}>
          <div className="portrait-speaker">{message.role === 'assistant' ? <><Sparkles size={14} /> 案板</> : '你'}</div>
          <p>{message.text}</p>
          {message.update && <span className="portrait-update"><Check size={13} />{message.update}</span>}
        </article>)}
        {locked && <p className="portrait-thinking" role="status">正在整理你刚刚说的内容…</p>}
      </div>
      <div className="portrait-compose">
        {suggestion && <button className="portrait-example" disabled={locked || voice.active} onClick={() => { setInput(suggestion); inputRef.current?.focus() }}>试用示例回答 <ArrowRight size={13} /></button>}
        <form onSubmit={event => { event.preventDefault(); void send() }}>
          <textarea ref={inputRef} aria-label="给案板发消息" placeholder="像聊天一样说就好，也可以随时纠正我的理解…"
            value={input} disabled={locked} readOnly={voice.active} onChange={event => setInput(event.target.value)} rows={3}
            onKeyDown={event => {
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing && event.keyCode !== 229) {
                event.preventDefault(); void send()
              }
            }} />
          <div className="portrait-compose-bottom"><span>Enter 发送 · Shift + Enter 换行</span><div className="portrait-compose-actions">
            <button type="button" className={`portrait-voice ${voice.active ? 'is-listening' : ''}`} aria-label={voice.active ? '停止语音输入' : '开始语音输入'} aria-pressed={voice.active}
              disabled={(!voice.active && locked) || voice.state === 'stopping'} title={voice.supported ? localVoice ? '本机离线中文语音输入' : '浏览器中文语音输入' : '此浏览器不支持，点击查看替代方法'}
              onClick={() => { if (voice.active) voice.stop(); else { voice.start(input); if (!voice.supported) inputRef.current?.focus() } }}>
              {voice.active ? <Square size={14} /> : <Mic size={17} />}{voice.active ? '结束录音' : '语音输入'}
            </button>
            <button className="portrait-send" aria-label="发送消息" disabled={locked || voice.active || !input.trim()} type="submit"><ArrowUp size={19} /></button>
          </div></div>
        </form>
        <p className="portrait-voice-notice" role="status">{voice.active
          ? voice.state === 'starting' ? '正在启动，请允许麦克风权限…' : voice.state === 'stopping' ? localVoice ? '正在本机转成文字，请稍等…' : '正在结束识别…' : localVoice ? '正在录音…点击结束后转成文字，每段最多 55 秒。' : '正在听你说话…结束录音后可以修改文字，不会自动发送。'
          : voice.notice || (localVoice ? '本机离线转写 · 录音不发往外部服务 · 检查文字后再发送' : voice.supported ? '语音由浏览器识别，可能联网处理；文字不会自动发送。' : '当前浏览器不支持网页语音识别，可使用系统或输入法的语音输入。')}</p>
        {(error || sendError) && <p className="portrait-error" role="alert">{error || sendError}</p>}
      </div>
    </section>

    <aside className="portrait-sheet" aria-label="正在形成的 IP 画像">
      <div className="portrait-sheet-top"><span>你的 IP 画像</span><span className="portrait-status">{confirmed ? '已确认' : count ? '逐渐清晰' : '等待认识你'}</span></div>
      <div className={`portrait-figure ${count ? 'has-story' : ''}`} role="img" aria-label={count ? '随着对话逐渐点亮的抽象人物形象，不代表真实外貌' : '尚未填入信息的空白人物轮廓'}>
        <div className="portrait-orbit" />
        <div className="portrait-person"><UserRound size={78} strokeWidth={1} /></div>
        {portraitFacets.map(([key, label], index) => <span key={key} className={`portrait-orbit-label orbit-${index} ${draft[key] ? 'known' : ''}`}>
          {draft[key] ? <Check size={11} /> : <span className="portrait-dot" />}{label}
        </span>)}
      </div>
      <div className="portrait-story" aria-live="polite">
        <h2>{name || '还没认识的你'}</h2>
        <p>{summary || '此刻是一张白纸。你的经历、想法和坚持，会慢慢成为这里的轮廓。'}</p>
      </div>
      <div className="portrait-facts">
        {portraitFacets.map(([key, label]) => <div key={key} className={`portrait-fact ${draft[key] ? 'collected' : ''}`}>
          <span>{label}</span>
          <div>{draft[key] ? <><p>{draft[key].text}</p><small>{draft[key].source}</small></> : <p className="portrait-unknown">聊到时，再慢慢补充</p>}</div>
        </div>)}
      </div>
      {creationIdea && <div className="portrait-creation-idea"><span>这次想创作</span><p>{creationIdea}</p></div>}
      <footer className="portrait-sheet-footer">
        <p><MessageCircle size={14} />{confirmed ? '想调整？继续聊，画像也会跟着更新。' : ready ? '看看这是不是你。有哪里不对，直接告诉我。' : '不用一次说完，也不用把每个维度填满。'}</p>
        {ready && <button className="portrait-primary" disabled={locked || voice.active || !!input.trim()} onClick={confirmed ? onCreate : onConfirm}>
          {confirmed ? '开始创作第一篇' : '这就是我，确认画像'}<ArrowRight size={16} />
        </button>}
        {ready && !!input.trim() && <small>先发送或清空正在输入的话，再继续。</small>}
      </footer>
    </aside>
  </div>
}
