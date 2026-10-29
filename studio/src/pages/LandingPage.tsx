import { useEffect, useState } from 'react'
import { ArrowDownRight, ArrowUpRight, Check, ChevronRight, CircleDot, Sparkles } from 'lucide-react'
import { Link } from 'react-router-dom'
import './LandingPage.css'

const capabilities = [
  {
    number: '01',
    eyebrow: 'PROJECT MEMORY',
    title: '先让系统认识你。',
    body: '把品牌、账号定位、受众、风格和素材沉淀成一套可复用的项目画像。每次创作都有上下文，不再从一张白纸开始。',
    accent: 'cyan',
  },
  {
    number: '02',
    eyebrow: 'SIGNAL TO STORY',
    title: '从趋势里找到自己的角度。',
    body: '发现热点，判断它是否适合你的账号，再把模糊的灵感变成清晰的选题方向和创作计划。',
    accent: 'violet',
  },
  {
    number: '03',
    eyebrow: 'AGENT WORKFLOW',
    title: '一次 briefing，得到完整交付。',
    body: '文章、种草笔记、图片和视频工作流在同一个空间里推进。看见进度，也看见每一份可以继续编辑的产物。',
    accent: 'pink',
  },
  {
    number: '04',
    eyebrow: 'FEEDBACK LOOP',
    title: '让下一次做得更好。',
    body: '连接发布记录和表现观察，把验证过的经验带回下一轮选题和创作决策，形成真正的内容飞轮。',
    accent: 'amber',
  },
]

const workflow = [
  { number: '01', label: '选题', english: 'Signal', title: '找到值得被看见的角度。', prompt: '扫描趋势与项目画像，生成一个有明确判断的切入点。', signal: 'SIGNAL DETECTED', output: '3 个可继续验证的方向' },
  { number: '02', label: '内容', english: 'Story', title: '让观点变成可交付的内容。', prompt: '根据当前项目画像，组织一条有节奏的叙事线。', signal: 'STORY IN MOTION', output: '文章结构与首屏开场' },
  { number: '03', label: '视觉', english: 'Frame', title: '让内容拥有自己的画面。', prompt: '保持品牌语气与视觉记忆，生成一组可编辑素材。', signal: 'FRAME COMPOSING', output: '封面方向与视觉提示' },
  { number: '04', label: '发布', english: 'Ship', title: '把交付送到该去的地方。', prompt: '检查渠道规格、发布素材和协作状态，完成最后确认。', signal: 'READY TO SHIP', output: '发布包与交付清单' },
  { number: '05', label: '复盘', english: 'Learn', title: '把结果带回下一轮。', prompt: '记录表现信号，把验证过的经验写回项目上下文。', signal: 'LOOP UPDATED', output: '下一轮的判断依据' },
]

function BrandMark({ className = '' }: { className?: string }) {
  return <img src="/brand/anban-mark.png" alt="Anban AI" className={className} />
}

function WorkspacePreview({ activeStage, onStageChange }: { activeStage: number; onStageChange: (stage: number) => void }) {
  const stage = workflow[activeStage]

  return (
    <div
      className="landing-preview"
      aria-label="Anban Studio 工作流预览"
      onPointerMove={(event) => {
        if (event.pointerType === 'touch') return
        const bounds = event.currentTarget.getBoundingClientRect()
        const x = ((event.clientX - bounds.left) / bounds.width - 0.5) * 5
        const y = ((event.clientY - bounds.top) / bounds.height - 0.5) * -3
        event.currentTarget.style.setProperty('--preview-tilt-x', `${x}deg`)
        event.currentTarget.style.setProperty('--preview-tilt-y', `${y}deg`)
      }}
      onPointerLeave={(event) => {
        event.currentTarget.style.removeProperty('--preview-tilt-x')
        event.currentTarget.style.removeProperty('--preview-tilt-y')
      }}
    >
      <div className="landing-preview__topbar">
        <span className="landing-preview__dots"><i /><i /><i /></span>
        <span className="landing-preview__path">/ studio / new-work</span>
        <span className="landing-preview__status"><CircleDot className="size-3" /> live / synced</span>
      </div>
      <div className="landing-preview__body">
        <aside className="landing-preview__rail">
          <div className="landing-preview__mini-mark"><BrandMark /></div>
          <span className="landing-preview__rail-line is-active" />
          <span className="landing-preview__rail-line" />
          <span className="landing-preview__rail-line" />
          <span className="landing-preview__rail-line" />
          <span className="landing-preview__rail-line" />
        </aside>
        <div className="landing-preview__content">
          <div className="landing-preview__meta"><span>CREATION WORKSPACE</span><span>{stage.number} / 05</span></div>
          <div className="landing-preview__dynamic" key={stage.number}>
            <div className="landing-preview__title">{stage.title}<br /><em>{stage.output}</em></div>
            <div className="landing-preview__prompt" aria-live="polite">
              <Sparkles className="size-4 text-cyan-300" />
              <span>{stage.prompt}</span>
              <ArrowUpRight className="ml-auto size-4 text-white/45" />
            </div>
          </div>
          <div className="landing-preview__progress">
            {workflow.map((item, index) => <span key={item.number} className={index < activeStage ? 'is-done' : index === activeStage ? 'is-current' : ''}>{index < activeStage ? <Check /> : null}</span>)}
          </div>
          <div className="landing-preview__labels" role="tablist" aria-label="切换工作流阶段">
            {workflow.map((item, index) => <button key={item.number} type="button" role="tab" aria-selected={index === activeStage} className={index === activeStage ? 'is-active' : ''} onClick={() => onStageChange(index)}>{item.label}</button>)}
          </div>
        </div>
      </div>
      <div className="landing-preview__signal"><span>{stage.signal}</span><b>{String(activeStage + 1).padStart(2, '0')}</b></div>
      <div className="landing-preview__glow" />
    </div>
  )
}

export default function LandingPage() {
  const [scrolled, setScrolled] = useState(false)
  const [activeStage, setActiveStage] = useState(2)

  useEffect(() => {
    const reveal = Array.from(document.querySelectorAll<HTMLElement>('.anban-reveal'))
    const observer = typeof IntersectionObserver === 'undefined'
      ? null
      : new IntersectionObserver((entries) => {
          entries.forEach((entry) => {
            if (entry.isIntersecting) entry.target.classList.add('is-visible')
          })
        }, { threshold: 0.12 })

    if (observer) {
      reveal.forEach((element) => observer.observe(element))
    } else {
      reveal.forEach((element) => element.classList.add('is-visible'))
    }

    const onScroll = () => setScrolled(window.scrollY > 24)
    window.addEventListener('scroll', onScroll, { passive: true })
    onScroll()
    return () => {
      observer?.disconnect()
      window.removeEventListener('scroll', onScroll)
    }
  }, [])

  return (
    <main className="landing-page">
      <nav className={`landing-nav ${scrolled ? 'is-scrolled' : ''}`} aria-label="主导航">
        <a href="#top" className="landing-nav__brand" aria-label="Anban AI 首页">
          <img src="/brand/anban-horizontal.png" alt="Anban AI" />
        </a>
        <div className="landing-nav__links">
          <a href="#capabilities">产品能力</a>
          <a href="#workflow">工作方式</a>
          <a href="#teams">适合团队</a>
        </div>
        <div className="landing-nav__actions">
          <Link to="/login" className="landing-nav__login">登录</Link>
          <Link to="/register" className="landing-button landing-button--small">注册 <ArrowUpRight className="size-3.5" /></Link>
        </div>
      </nav>

      <section id="top" className="landing-hero">
        <div className="landing-shell landing-hero__inner">
          <div className="landing-hero__copy anban-reveal">
            <p className="landing-kicker"><span /> AI-native content operating system</p>
            <h1>让每一轮内容生产，<br /><span>成为下一轮的依据。</span></h1>
            <p className="landing-hero__description">Anban 把项目画像、选题判断、AI 生产、交付发布和表现复盘，连接在同一个工作空间里。</p>
            <div className="landing-hero__actions">
              <Link to="/register" className="landing-button">注册 <ArrowUpRight className="size-4" /></Link>
              <Link to="/login" className="landing-text-button">登录 <ChevronRight className="size-4" /></Link>
            </div>
            <div className="landing-hero__proof"><span><Check className="size-3.5" /> 从项目画像开始</span><span><Check className="size-3.5" /> 每次交付都有依据</span></div>
          </div>
          <div className="landing-hero__visual anban-reveal anban-reveal--delay">
            <div className="landing-hero__mark-wrap"><BrandMark className="landing-hero__mark" /><span className="landing-hero__mark-label">ANBAN / 01</span></div>
            <WorkspacePreview activeStage={activeStage} onStageChange={setActiveStage} />
          </div>
        </div>
        <a className="landing-scroll-cue" href="#capabilities" aria-label="查看产品能力"><span>SCROLL TO EXPLORE</span><ArrowDownRight className="size-4" /></a>
      </section>

      <section id="capabilities" className="landing-capabilities landing-section-light">
        <div className="landing-shell">
          <div className="landing-section-intro anban-reveal"><p className="landing-kicker landing-kicker--dark"><span /> The system</p><h2>不是又一个<br /><em>写作工具。</em></h2><p>从一次创作，到一套会持续变好的内容系统。</p></div>
          <div className="landing-capability-list">
            {capabilities.map((item) => (
              <article key={item.number} className={`landing-capability anban-reveal landing-capability--${item.accent}`}>
                <span className="landing-capability__number">{item.number}</span>
                <div className="landing-capability__copy"><p className="landing-capability__eyebrow">{item.eyebrow}</p><h3>{item.title}</h3><p>{item.body}</p></div>
                <ArrowUpRight className="landing-capability__arrow" />
              </article>
            ))}
          </div>
        </div>
      </section>

      <section id="workflow" className="landing-workflow">
        <div className="landing-shell">
          <div className="landing-workflow__head anban-reveal"><p className="landing-kicker"><span /> One continuous loop</p><h2>从灵感到影响力，<br /><span>每一步都连得上。</span></h2><p>内容不是一次性的输出，而是一个能被观察、被复用、被改进的循环。</p></div>
          <div className="landing-track anban-reveal" role="tablist" aria-label="工作流阶段">
            <div className="landing-track__line" />
            {workflow.map((item, index) => <button type="button" role="tab" aria-selected={index === activeStage} className={`landing-track__node ${index === activeStage ? 'is-active' : ''}`} key={item.number} onClick={() => setActiveStage(index)}><span className="landing-track__dot"><i /></span><span className="landing-track__number">{item.number}</span><strong>{item.label}</strong><small>{item.english}</small></button>)}
          </div>
          <div className="landing-workflow__note anban-reveal" aria-live="polite"><span>WORKFLOW SIGNAL</span><p>{workflow[activeStage].prompt}</p><strong>{workflow[activeStage].output}</strong><ArrowUpRight className="size-5" /></div>
        </div>
      </section>

      <section id="teams" className="landing-teams landing-section-light">
        <div className="landing-shell">
          <div className="landing-teams__heading anban-reveal"><p className="landing-kicker landing-kicker--dark"><span /> Built for people who publish</p><h2>把内容当成<br /><em>长期资产。</em></h2></div>
          <div className="landing-teams__list anban-reveal">
            <div><span>01</span><h3>创作者</h3><p>把个人风格和灵感沉淀下来，稳定地产出有自己声音的内容。</p></div>
            <div><span>02</span><h3>品牌团队</h3><p>让品牌定位、内容协作和发布节奏共享同一份上下文。</p></div>
            <div><span>03</span><h3>电商与专业服务</h3><p>把产品知识、客户洞察和每次交付，变成下一次创作的起点。</p></div>
          </div>
        </div>
      </section>

      <section className="landing-cta">
        <div className="landing-shell landing-cta__inner anban-reveal"><BrandMark className="landing-cta__mark" /><p className="landing-kicker"><span /> Make the next one better</p><h2>把今天的创作，<br /><span>变成明天的判断。</span></h2><div className="landing-hero__actions"><Link to="/register" className="landing-button">注册 <ArrowUpRight className="size-4" /></Link><Link to="/login" className="landing-text-button">登录 <ChevronRight className="size-4" /></Link></div></div>
      </section>

      <footer className="landing-footer"><div className="landing-shell landing-footer__inner"><img src="/brand/anban-horizontal.png" alt="Anban AI" /><p>© {new Date().getFullYear()} 成都北冕星辰科技有限公司 · 蜀ICP备2024071665号-3</p><a href="https://beian.miit.gov.cn/" target="_blank" rel="noreferrer">备案信息</a></div></footer>
    </main>
  )
}
