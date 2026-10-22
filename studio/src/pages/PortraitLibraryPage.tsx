import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ArrowRight, Plus, Users } from 'lucide-react'
import { api } from '@/lib/api'
import { supportsProfileLab } from '@/lib/project-capabilities'
import { accountPlatformLabels, portraitProgress, portraitStatus, portraitSummary, updatePortraitHref } from '@/lib/portrait-library'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

export default function PortraitLibraryPage() {
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('all')
  const query = useQuery({ queryKey: ['projects', 'all'], queryFn: () => api.projects.list() })
  const identities = (query.data ?? []).filter(supportsProfileLab)
  const filtered = identities.filter(project => (status === 'all' || project.status === status) && `${project.name} ${portraitSummary(project)} ${(project.profile_setup?.platform_accounts ?? []).map(a => a.account_name).join(' ')}`.toLowerCase().includes(search.toLowerCase()))
  return <section className="mx-auto max-w-6xl space-y-6 p-4 md:p-8">
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div><p className="mb-2 text-sm text-muted-foreground">我的创作身份</p><h1 className="text-2xl font-semibold">画像库</h1><p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">老板、师傅、销售或品牌，各有自己的定位。一份画像可以用于多个平台账号，在这里统一查看和完善。</p></div>
      <Link className="inline-flex min-h-11 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground" to="/projects/create?return_to=portraits"><Plus className="size-4" />新增创作身份</Link>
    </header>
    <div className="flex flex-wrap items-center gap-3">
      <Input className="max-w-sm" aria-label="搜索创作身份" placeholder="搜索名称、定位或平台账号" value={search} onChange={e => setSearch(e.target.value)} />
      <select aria-label="身份状态" className="h-10 rounded-md border bg-background px-3 text-sm" value={status} onChange={e => setStatus(e.target.value)}><option value="all">全部（含已归档）</option><option value="active">使用中</option><option value="archived">已归档</option></select>
      <span className="text-sm text-muted-foreground">{filtered.length} 个创作身份</span>
    </div>
    {query.isPending ? <p role="status">正在读取画像…</p> : query.isError ? <div role="alert"><p>画像暂时无法读取，已保存的内容不会因此丢失。</p><Button onClick={() => void query.refetch()} variant="outline">重试</Button></div> : filtered.length === 0 ? <div className="rounded-xl border border-dashed p-10 text-center"><Users className="mx-auto mb-3 size-8 text-muted-foreground" /><p>{identities.length ? '没有找到匹配的身份，请调整搜索条件。' : '从第一个创作身份开始，比如“钢厂老板老王”。'}</p></div> : <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">{filtered.map(project => {
      const progress = portraitProgress(project.profile)
      const accounts = project.profile_setup?.platform_accounts ?? []
      return <article key={project.id} className="flex flex-col rounded-xl border bg-card p-5" aria-label={`创作身份：${project.name}`}>
        <div className="flex items-start justify-between gap-2"><h2 className="text-lg font-semibold"><Link className="hover:underline" to={`/portraits/${project.id}`}>{project.name}</Link></h2>{project.status === 'archived' && <span className="shrink-0 rounded bg-muted px-2 py-1 text-xs">已归档</span>}</div>
        <p className="mt-3 line-clamp-3 text-sm leading-6 text-muted-foreground">{portraitSummary(project)}</p>
        <div className="mt-4 space-y-2"><div className="flex justify-between text-xs"><span>{portraitStatus(project.profile)}</span><span>已填写 {progress.count}/6 项</span></div><div className="h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-label={`${project.name}画像完善度`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={progress.percent}><div className="h-full bg-primary" style={{ width: `${progress.percent}%` }} /></div></div>
        <p className="my-4 text-xs leading-5 text-muted-foreground">{accounts.length ? accounts.map(a => `${accountPlatformLabels[a.platform] ?? a.platform}${a.account_name ? ` · ${a.account_name}` : ''}`).join(' / ') : '尚未添加平台账号 · 可先完善画像'}</p>
        <div className="mt-auto flex flex-wrap gap-3 border-t pt-4 text-sm"><Link className="inline-flex min-h-10 items-center gap-1 font-medium text-primary" to={`/portraits/${project.id}`}>查看与修改<ArrowRight className="size-4" /></Link><Link className="inline-flex min-h-10 items-center text-muted-foreground hover:text-foreground" to={updatePortraitHref(project.id)}>继续访谈</Link></div>
      </article>
    })}</div>}
    <p className="text-xs leading-5 text-muted-foreground">现有人设的画像已经列在这里，无需重新录入。归档只表示暂停使用，画像仍会保留。</p>
  </section>
}
