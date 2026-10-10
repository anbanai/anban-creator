import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, Sparkles } from 'lucide-react'
import { PortraitConversation, type PortraitDraft, type PortraitMessage } from '@/components/projects/PortraitConversation'

// Scripted dev-only sample. Never infer customer facts from keyword matching.
// Only imported by the local preview entry, not by the application router.
const sampleTurns: { answer: string; reply: string; update: string; facts: PortraitDraft }[] = [
  {
    answer: '我叫小林，开了一家社区花店。想在网上分享一些养花的经验，主要给刚开始养花、总担心养不活的人看。',
    reply: '我认识你一点了：你是社区里的花店主小林，想用自己的经验，帮刚开始养花的人少一点挫败感。\n\n平时有人拿着一盆快枯的花来找你，你会怎么跟他说？按你平常的语气说就好。',
    update: '画像里有了你的身份和读者',
    facts: {
      identity: { text: '小林，一家社区花店的经营者。', source: '示例中的用户自述' },
      audience: { text: '刚开始养花，担心养不活的人。', source: '示例中的用户自述' },
    },
  },
  {
    answer: '我会说，先别急着扔，拍张照片我看看。我想像邻居一样聊天，别全是专业术语，也不要为了卖花夸大效果。',
    reply: '“先别急着扔，拍张照片我看看。”这句话很有你的感觉。温和、实在，也愿意帮人解决眼前的问题。\n\n你想先在哪里分享？第一篇有没有想聊的事？没想好也没关系，我们可以一起找。',
    update: '补上了你的表达方式和坚持',
    facts: {
      style: { text: '像邻居一样聊天，先安抚，再讲具体做法，少用术语。', source: '根据示例归纳' },
      preferences: { text: '不为卖花夸大效果，不用专业术语吓退新手。', source: '示例中的用户自述' },
    },
  },
  {
    answer: '先做小红书吧。我最常被问到的是：为什么刚买回家的花，没几天就蔫了？想先讲讲这个。',
    reply: '可以，你的第一篇就从这个真实问题开始。\n\n右边是我们一起聊出来的你：社区花店的小林，像邻居一样，帮养花新手把问题弄明白。看看有没有哪里理解偏了？你可以直接告诉我，也可以确认后开始第一篇。',
    update: '补上了分享平台和第一篇的方向',
    facts: {
      platforms: { text: '先在小红书分享。', source: '示例中的用户自述' },
    },
  },
]
const correction = '读者再改一下，不是所有养花新手，主要是想给家里添点花、又没太多时间打理的上班族。'
const welcome: PortraitMessage = {
  id: 'welcome', role: 'assistant',
  text: '你好，很高兴认识你。\n\n先不用想“定位”这种大问题。你平时在做什么？最近为什么想开始做内容？从你最想聊的地方说起就好。',
}

export default function PortraitOnboardingPreview() {
  const [messages, setMessages] = useState<PortraitMessage[]>([welcome])
  const [draft, setDraft] = useState<PortraitDraft>({})
  const [step, setStep] = useState(0)
  const [corrected, setCorrected] = useState(false)
  const [customPending, setCustomPending] = useState(false)
  const [creating, setCreating] = useState(false)
  const [resetPrompt, setResetPrompt] = useState(false)
  const [epoch, setEpoch] = useState(0)
  const [localVoice, setLocalVoice] = useState(false)
  const nextRef = useRef<HTMLElement>(null)

  useEffect(() => {
    if (window.location.hostname !== '127.0.0.1' || window.location.port !== '5174') return
    const controller = new AbortController()
    void fetch('/__local-preview/dictation', { signal: controller.signal })
      .then(response => response.ok ? response.json() : null)
      .then(result => { if (!controller.signal.aborted && result?.available === true) setLocalVoice(true) })
      .catch(() => {})
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (messages.length === 1) return
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [messages.length])

  useEffect(() => { if (creating) nextRef.current?.focus() }, [creating])

  function reset() {
    setMessages([welcome]); setDraft({}); setStep(0); setCorrected(false)
    setCustomPending(false); setCreating(false); setResetPrompt(false)
    setEpoch(value => value + 1)
  }

  async function send(text: string) {
    const turn = sampleTurns[step]
    let reply: string
    let update: string | undefined
    if (turn && text === turn.answer) {
      setDraft(previous => ({ ...previous, ...turn.facts }))
      setStep(value => value + 1)
      reply = turn.reply; update = turn.update
    } else if (step >= sampleTurns.length && text === correction) {
      setDraft(previous => ({ ...previous, audience: { text: '想给家里添点花、没太多时间打理的上班族。', source: '根据示例中的用户纠正更新' } }))
      setCorrected(true)
      reply = '明白，读者已经收窄为“想给家里添点花、又没太多时间打理的上班族”。后面的内容也应当更关注省心、好操作。\n\n原来的宽泛描述已替换，你再看看右边是不是更像你想服务的人。'
      update = '已修改读者，等待重新确认'
    } else {
      setCustomPending(true)
      reply = '这句话已留在当前预览里。但这里还没有接入真实 AI，我不能理解并自动整理你的自由输入，所以右边没有采用这句话。\n\n你可以继续体验预设的花店示例；正式版本接通后，会根据你真实说的话追问、更新画像。'
    }
    setMessages(previous => [...previous,
      { id: `user-${previous.length}`, role: 'user', text },
      { id: `assistant-${previous.length}`, role: 'assistant', text: reply, update },
    ])
  }


  return <div className="portrait-preview">
    <div className="portrait-preview-bar"><span>交互样例 · 花店故事为虚构演示，回复按示例预设，未接入真实 AI</span><span>不连接账号，不生成作品；刷新后重置</span></div>
    <nav className="portrait-preview-nav" aria-label="预览导航"><strong>Anban <span>让好内容，从认识你开始</span></strong><button onClick={() => setResetPrompt(true)}>从空白重新体验</button></nav>
    {resetPrompt && <div className="portrait-reset" role="alert"><span>重新开始会清空本次演示对话和未发送文字。</span><button onClick={() => setResetPrompt(false)}>继续当前对话</button><button onClick={reset}>清空并重新开始</button></div>}
    {customPending && <div className="portrait-reset" role="status"><span>你的自由输入尚未纳入画像，当前只能演示预设故事。</span><button onClick={() => {
      setCustomPending(false)
      setMessages(previous => [...previous, { id: `resume-${previous.length}`, role: 'assistant', text: '继续预设示例。此前的自由输入保留在对话中，但不纳入示例画像。' }])
    }}>继续示例，不采用自由输入</button></div>}
    {creating ? <section className="portrait-next" ref={nextRef} tabIndex={-1} aria-label="第一篇创作交接示例">
      <span className="portrait-eyebrow"><Sparkles size={18} /> 第一篇，已经有了方向</span>
      <h2>刚买回家的花，为什么没几天就蔫了？</h2>
      <p>给{corrected ? '没太多时间打理花的上班族' : '担心把花养不活的新手'}，写一篇像邻居聊天一样的小红书图文。先解答困惑，再给出能照着做的建议。</p>
      <p>这是创作交接示例。正式接入后，将带入已确认的画像与本篇要求，进入创作流程；本预览到此结束，没有创建任务、生成正文或发布内容。</p>
      <button className="portrait-primary" onClick={() => setCreating(false)}><ArrowLeft size={15} />回到对话，继续调整</button>
    </section> : <PortraitConversation key={epoch} messages={messages} draft={draft} localVoice={localVoice}
      name={step ? '小林 · 社区花店主' : undefined}
      creationIdea={step >= sampleTurns.length ? '第一篇：刚买回家的花，为什么没几天就蔫了？' : undefined}
      summary={step ? (corrected ? '陪忙碌的上班族，把一点花香带回家。' : step >= 2 ? '像邻居一样，陪你把第一盆花养好。' : '从社区花店出发，分享让新手安心的养花经验。') : undefined}
      suggestion={sampleTurns[step]?.answer ?? (!corrected ? correction : undefined)}
      ready={step >= sampleTurns.length && !customPending}
      onSend={send} onCreate={() => { if (!customPending) setCreating(true) }} />}
  </div>
}
