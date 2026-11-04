import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { ArrowRight, ArrowUpRight, Plus, Sparkles, UserRound } from 'lucide-react'
import { api } from '@/lib/api'
import { newMobilePersonaState } from '@/lib/mobile-creation'
import { MobileCreationCards } from '@/components/mobile/MobileCreationCards'
import { Button } from '@/components/ui/button'

export default function MobileStudioPage() {
  const projects = useQuery({ queryKey: ['projects', 'active'], queryFn: () => api.projects.list({ status: 'active' }) })
  const [selectedId, setSelectedId] = useState('')
  const selected = projects.data?.find(project => project.id === selectedId) ?? projects.data?.[0]
  return <div className="mobile-studio-page">
    <div className="mobile-page-eyebrow"><span className="mobile-live-dot" />ANBAN / CREATIVE STUDIO</div>
    <header className="mobile-studio-heading"><h1>好内容，<br /><span>从认识你开始。</span></h1><p>一个人设，让每一次创作都有自己的表达。</p></header>
    <Link to="/" state={newMobilePersonaState} className="mobile-interview-card">
      <span className="mobile-interview-orbit" aria-hidden="true"><Sparkles size={27} strokeWidth={1.3} /></span>
      <span className="mobile-interview-kicker">YOUR VOICE, YOUR IDENTITY</span>
      <strong>聊一聊，建立你的人设</strong><span>说说你是谁、做什么、想打动谁。<br />我们一起把方向理清楚。</span>
      <span className="mobile-interview-cta">开始对话 <ArrowRight size={16} /></span>
    </Link>
    <section aria-label="选择创作人设" className="mobile-persona-strip">
      <span className="mobile-persona-avatar"><UserRound size={19} /></span>
      <div className="min-w-0 flex-1"><label htmlFor="mobile-home-persona">本次创作，使用谁的表达？</label>
        {projects.isPending ? <p role="status">正在读取人设…</p> : projects.isError ? <><p role="alert">人设暂时无法读取</p><Button size="sm" variant="ghost" onClick={() => void projects.refetch()}>重试</Button></> : projects.data?.length ? <select id="mobile-home-persona" value={selected?.id ?? ''} onChange={event => setSelectedId(event.target.value)}>{projects.data.map(project => <option value={project.id} key={project.id}>{project.name}</option>)}</select> : <Link to="/" state={newMobilePersonaState}>先建立第一个人设 <ArrowUpRight size={13} /></Link>}
      </div><Link to="/mobile/personas" aria-label="管理人设" className="mobile-icon-action"><Plus size={18} /></Link>
    </section>
    <section aria-label="开始创作"><div className="mobile-section-heading"><h2>今天，想做点什么？</h2><span>CREATE SOMETHING</span></div><MobileCreationCards projectId={selected?.id} /></section>
    <Link to="/tasks" className="mobile-works-link"><span>灵感有了去处，作品也都在这里。</span><span>查看作品 <ArrowRight size={15} /></span></Link>
  </div>
}
