import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { ArrowUpRight, MessageCircle, Plus, UserRound } from 'lucide-react'
import { api } from '@/lib/api'
import { newMobilePersonaState } from '@/lib/mobile-creation'
import { Button } from '@/components/ui/button'

export default function MobilePersonasPage() {
  const projects = useQuery({ queryKey: ['projects', 'active'], queryFn: () => api.projects.list({ status: 'active' }) })
  return <div className="mobile-studio-page"><div className="mobile-page-eyebrow">YOUR CREATIVE IDENTITIES</div><header className="mobile-page-title"><h1>每一种表达，<br />都有自己的你。</h1><p>同一个人设，可以陪你创作不同形式的内容。</p></header>
    <Link to="/" state={newMobilePersonaState} className="mobile-primary-link"><Plus size={18} /> 对话建立新人设</Link>
    {projects.isPending && <p role="status" className="mobile-empty-state">正在读取你的人设…</p>}
    {projects.isError && <div className="mobile-empty-state"><p role="alert">人设暂时无法读取，请重试。</p><Button variant="outline" onClick={() => void projects.refetch()}>重新读取</Button></div>}
    {projects.data?.length === 0 && <div className="mobile-empty-state"><UserRound size={32} strokeWidth={1.2} /><h2>从一段对话开始</h2><p>不用填写长表格，聊聊你的业务与想法，就能逐步建立人设。</p></div>}
    <div className="mobile-persona-list">{projects.data?.map(project => <article key={project.id} className="mobile-persona-card">
      <Link className="mobile-persona-card-body" to={`/portraits/${encodeURIComponent(project.id)}`}><span className="mobile-persona-avatar"><UserRound size={23} /></span><div><h2>{project.name}</h2><p>{project.description || project.instructions || project.positioning || '继续聊聊，让这个人设更清晰。'}</p></div><ArrowUpRight size={17} /></Link>
      <div className="mobile-persona-card-actions"><Link to="/" state={{ modeId: 'profile', projectId: project.id }}><MessageCircle size={15} />继续完善</Link><Link to={`/mobile/create?project_id=${encodeURIComponent(project.id)}`}>用这个人设创作 <ArrowUpRight size={15} /></Link></div>
    </article>)}</div>
    <Link className="mobile-secondary-link" to="/projects">归档与更多管理</Link>
  </div>
}
