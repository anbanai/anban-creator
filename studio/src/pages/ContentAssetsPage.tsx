import { Link, useSearchParams } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, ArrowRight, FileText, Pause, Play, Plus, Search, Settings2, Sparkles, X } from 'lucide-react'
import PageHeader from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { api } from '@/lib/api'
import type { ContentAsset } from '@/types'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const statusLabels: Record<string, string> = { candidate: '待研究', researched: '已研究', validated: '已验证', ready: '可复用', in_progress: '生产中', exhausted: '已耗尽', archived: '已归档' }
const lifecycleLabels: Record<string, string> = { evergreen: '长期', seasonal: '季节性', expiring: '时效性' }
const runStatusLabels: Record<string, string> = { queued: '等待自动生产', running: '自动生产中', paused: '已暂停', completed: '已完成', failed: '生产失败', cancelled: '已取消' }
const runStageLabels: Record<string, string> = { research: '选题研究', scoring: '平台评分', brief: '生成平台 Brief', produce: '内容生成', review: '质量检查', schedule: '排期', publish: '发布', observe: '数据复盘' }

export default function ContentAssetsPage() {
  const client = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [sourceTaskID, setSourceTaskID] = useState(searchParams.get('source_task_id') ?? '')
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [platformFilter, setPlatformFilter] = useState('')
  const [sourceFilter, setSourceFilter] = useState('')
  const [lifecycleFilter, setLifecycleFilter] = useState('')
  const [projectFilter, setProjectFilter] = useState('')
  const [page, setPage] = useState(0)
  const [activeTab, setActiveTab] = useState<'assets' | 'runs'>('assets')
  const [newTitle, setNewTitle] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [reuseAsset, setReuseAsset] = useState<ContentAsset | null>(null)
  const [targetProjectId, setTargetProjectId] = useState('')
  const [selectedRunId, setSelectedRunId] = useState(searchParams.get('run_id') ?? '')
  const [detailAssetId, setDetailAssetId] = useState(searchParams.get('asset_id') ?? searchParams.get('reuse_asset_id') ?? '')
  const [scoreProjectId, setScoreProjectId] = useState('')
  const assets = useQuery({ queryKey: ['content-assets', search, statusFilter, platformFilter, sourceFilter, lifecycleFilter, projectFilter, page], queryFn: () => api.contentAssets.list({ q: search || undefined, status: statusFilter || undefined, platform: platformFilter || undefined, source: sourceFilter || undefined, lifecycle: lifecycleFilter || undefined, project_id: projectFilter || undefined, offset: page * 25, limit: 25 }), staleTime: 30000 })
  const projects = useQuery({ queryKey: ['projects', 'reuse'], queryFn: () => api.projects.list({ status: 'active' }), staleTime: 30000 })
  const detail = useQuery({ queryKey: ['content-assets', detailAssetId], queryFn: () => api.contentAssets.get(detailAssetId), enabled: !!detailAssetId })
  const runs = useQuery({ queryKey: ['content-reuse-runs', 'list'], queryFn: () => api.contentAssets.listRuns({ offset: 0, limit: 50 }), refetchInterval: query => (query.state.data?.items ?? []).some(run => ['queued', 'running'].includes(run.status)) ? 5000 : false })
  const runQuery = useQuery({ queryKey: ['content-reuse-runs', selectedRunId], queryFn: () => api.contentAssets.getRun(selectedRunId), enabled: !!selectedRunId, refetchInterval: query => ['queued', 'running'].includes(query.state.data?.status ?? '') ? 5000 : false })
  const displayedRun = runQuery.data
  const create = useMutation({
    mutationFn: () => api.contentAssets.create({ title: newTitle }),
    onSuccess: () => { setNewTitle(''); setShowCreate(false); void client.invalidateQueries({ queryKey: ['content-assets'] }) },
  })
  const createReuseRun = useMutation({
    mutationFn: () => {
      const project = (projects.data ?? []).find(item => item.id === targetProjectId)
      if (!reuseAsset || !project) throw new Error('请选择目标项目')
      const requestId = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
      return api.contentAssets.createReuseRun(reuseAsset.id, { target_project_id: project.id, target_platform: project.platform || 'unknown', ...(sourceTaskID ? { source_content_kind: 'task', source_content_id: sourceTaskID } : {}), idempotency_key: `reuse:${reuseAsset.id}:${project.id}:${requestId}` })
    },
    onSuccess: run => { setSelectedRunId(run.id); setSearchParams(current => { const next = new URLSearchParams(current); next.set('run_id', run.id); return next }, { replace: true }); setActiveTab('runs'); client.setQueryData(['content-reuse-runs', run.id], run); setReuseAsset(null); void client.invalidateQueries({ queryKey: ['content-reuse-runs'] }); void client.invalidateQueries({ queryKey: ['content-assets'] }) },
  })
  const updateRun = useMutation({
    mutationFn: ({ id, status }: { id: string; status: 'paused' | 'queued' | 'cancelled' }) => api.contentAssets.setRunStatus(id, status),
    onSuccess: run => { client.setQueryData(['content-reuse-runs', run.id], run); void client.invalidateQueries({ queryKey: ['content-reuse-runs', 'list'] }) },
  })
  const scoreAsset = useMutation({
    mutationFn: () => api.contentAssets.score(detailAssetId, scoreProjectId),
    onSuccess: () => { void client.invalidateQueries({ queryKey: ['content-assets', detailAssetId] }) },
  })
  useEffect(() => {
    const reuseID = searchParams.get('reuse_asset_id')
    if (!reuseID || detail.data?.asset.id !== reuseID) return
    setReuseAsset(detail.data.asset)
    setSearchParams({ asset_id: reuseID }, { replace: true })
  }, [detail.data, searchParams, setSearchParams])
  const items = assets.data?.items ?? []

  return <div className="mx-auto w-full max-w-6xl space-y-6 pb-10">
    <PageHeader title="选题资产" description="把一个好选题持续转化成公众号、小红书和视频内容。">
      <Link to="/content-assets/settings" className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border px-3.5 text-sm font-medium transition-colors hover:bg-muted"><Settings2 className="size-4" />自动生产设置</Link>
      <Button onClick={() => setShowCreate(value => !value)}><Plus />新增选题资产</Button>
    </PageHeader>

    {showCreate && <section className="flex flex-wrap items-end gap-3 border-b border-border pb-5" aria-label="新增选题资产">
      <label className="min-w-0 flex-1 space-y-1.5 text-sm"><span>选题标题</span><Input value={newTitle} onChange={event => setNewTitle(event.target.value)} placeholder="例如：为什么越努力越没有结果" /></label>
      <Button disabled={!newTitle.trim() || create.isPending} onClick={() => create.mutate()}><Sparkles />创建</Button>
    </section>}

    <div className="flex gap-2 border-b border-border" role="tablist" aria-label="选题资产工作区">
      <button role="tab" aria-selected={activeTab === 'assets'} className={`border-b-2 px-3 py-2 text-sm ${activeTab === 'assets' ? 'border-primary font-medium' : 'border-transparent text-muted-foreground'}`} onClick={() => setActiveTab('assets')}>选题资产</button>
      <button role="tab" aria-selected={activeTab === 'runs'} className={`border-b-2 px-3 py-2 text-sm ${activeTab === 'runs' ? 'border-primary font-medium' : 'border-transparent text-muted-foreground'}`} onClick={() => setActiveTab('runs')}>自动生产</button>
    </div>

    {activeTab === 'assets' && <>
      <div className="grid gap-2 border-b border-border pb-4 sm:grid-cols-2 lg:grid-cols-3">
        <label className="flex items-center gap-2 text-sm"><Search className="size-4 text-muted-foreground" /><Input aria-label="搜索选题资产" value={search} onChange={event => { setSearch(event.target.value); setPage(0) }} placeholder="搜索选题、核心问题" /></label>
        <select aria-label="选题状态" className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={statusFilter} onChange={event => { setStatusFilter(event.target.value); setPage(0) }}><option value="">全部状态</option>{Object.entries(statusLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
        <select aria-label="内容平台筛选" className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={platformFilter} onChange={event => { setPlatformFilter(event.target.value); setPage(0) }}><option value="">全部平台</option>{['wechat', 'seednote', 'hypit', 'montage', 'whiteboard-animation'].map(value => <option key={value} value={value}>{value}</option>)}</select>
        <select aria-label="证据来源筛选" className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={sourceFilter} onChange={event => { setSourceFilter(event.target.value); setPage(0) }}><option value="">全部证据来源</option><option value="external">外部研究</option><option value="internal_published">已发布内容</option><option value="imported">导入内容</option></select>
        <select aria-label="生命周期筛选" className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={lifecycleFilter} onChange={event => { setLifecycleFilter(event.target.value); setPage(0) }}><option value="">全部生命周期</option>{Object.entries(lifecycleLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
        <select aria-label="所属项目筛选" className="h-9 rounded-md border border-input bg-background px-3 text-sm" value={projectFilter} onChange={event => { setProjectFilter(event.target.value); setPage(0) }}><option value="">全部项目</option>{(projects.data ?? []).map(project => <option key={project.id} value={project.id}>{project.name}</option>)}</select>
      </div>
      {assets.isPending && <p role="status" className="py-12 text-center text-sm text-muted-foreground">正在加载选题资产…</p>}
      {assets.isError && <div role="alert" className="flex items-center justify-center gap-3 py-10 text-sm text-destructive"><span>选题资产加载失败。</span><Button variant="outline" size="sm" onClick={() => void assets.refetch()}>重试</Button></div>}
      {!assets.isPending && !assets.isError && items.length === 0 && <div className="border border-dashed border-border py-16 text-center"><Archive className="mx-auto size-8 text-muted-foreground" /><p className="mt-3 text-sm text-muted-foreground">没有符合筛选条件的选题资产。</p></div>}
      <section className="grid gap-3 md:grid-cols-2" aria-label="选题资产列表">
      {items.map((asset: ContentAsset) => <article key={asset.id} className="border border-border bg-card p-4 transition-colors hover:border-primary/40">
        <div className="flex items-start justify-between gap-3"><div className="min-w-0"><h2 className="truncate font-medium">{asset.title}</h2><p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{asset.core_question || '还没有补充核心问题'}</p></div><Badge variant="secondary">{statusLabels[asset.status] || asset.status}</Badge></div>
        <div className="mt-4 flex items-center justify-between text-xs text-muted-foreground"><span>{lifecycleLabels[asset.lifecycle] || asset.lifecycle}</span><span>{new Date(asset.updated_at).toLocaleDateString('zh-CN')}</span></div>
        <div className="mt-4 flex justify-end gap-1"><Button variant="ghost" size="sm" onClick={() => setDetailAssetId(value => value === asset.id ? '' : asset.id)}><FileText />内容链路</Button><Button variant="ghost" size="sm" onClick={() => { setSourceTaskID(''); setReuseAsset(asset); setTargetProjectId('') }}><ArrowRight />跨平台复用</Button></div>
      </article>)}
      </section>
      <div className="flex items-center justify-between border-t border-border pt-3 text-sm"><span className="text-muted-foreground">共 {assets.data?.total ?? 0} 个选题资产</span><div className="flex gap-2"><Button variant="outline" size="sm" disabled={page === 0 || assets.isPending} onClick={() => setPage(value => value - 1)}>上一页</Button><Button variant="outline" size="sm" disabled={(page + 1) * 25 >= (assets.data?.total ?? 0) || assets.isPending} onClick={() => setPage(value => value + 1)}>下一页</Button></div></div>
    </>}

    {activeTab === 'runs' && <section className="space-y-3" aria-label="自动生产运行列表">
      {runs.isPending && <p role="status" className="py-10 text-center text-sm text-muted-foreground">正在加载自动生产记录…</p>}
      {runs.isError && <div role="alert" className="flex items-center justify-center gap-3 py-10 text-sm text-destructive"><span>自动生产记录加载失败。</span><Button variant="outline" size="sm" onClick={() => void runs.refetch()}>重试</Button></div>}
      {runs.data?.items.map(run => <button key={run.id} type="button" onClick={() => { setSelectedRunId(run.id); setSearchParams(current => { const next = new URLSearchParams(current); next.set('run_id', run.id); return next }, { replace: true }) }} className={`flex w-full items-center justify-between gap-4 border-b border-border px-2 py-3 text-left ${selectedRunId === run.id ? 'bg-muted/50' : 'hover:bg-muted/30'}`}>
        <span className="min-w-0"><span className="block truncate text-sm font-medium">{run.target_platform} · {run.asset_id}</span><span className="mt-1 block text-xs text-muted-foreground">{runStatusLabels[run.status] || run.status} · {runStageLabels[run.stage] || run.stage} · {run.target_project_id}</span></span>
        <span className="shrink-0 text-xs text-muted-foreground">{new Date(run.updated_at).toLocaleString('zh-CN')}</span>
      </button>)}
      {runs.data && runs.data.total === 0 && <p className="py-12 text-center text-sm text-muted-foreground">还没有自动生产记录。</p>}
      {displayedRun && <div className="border-t border-border pt-4"><div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="text-sm font-semibold">运行详情</h2><p className="mt-1 text-sm text-muted-foreground">{runStatusLabels[displayedRun.status] || displayedRun.status} · {runStageLabels[displayedRun.stage] || displayedRun.stage} · 已用 {displayedRun.cost_credits} 积分 · 预留 {displayedRun.budget_reserved} 积分</p>{displayedRun.error_message && <p role="alert" className="mt-1 text-sm text-destructive">{displayedRun.error_message}</p>}</div><div className="flex gap-2">{['queued', 'running'].includes(displayedRun.status) && <Button variant="outline" size="sm" disabled={updateRun.isPending} onClick={() => updateRun.mutate({ id: displayedRun.id, status: 'paused' })}><Pause />暂停</Button>}{displayedRun.status === 'paused' && <Button variant="outline" size="sm" disabled={updateRun.isPending} onClick={() => updateRun.mutate({ id: displayedRun.id, status: 'queued' })}><Play />恢复</Button>}{['queued', 'running', 'paused'].includes(displayedRun.status) && <Button variant="ghost" size="sm" disabled={updateRun.isPending} onClick={() => updateRun.mutate({ id: displayedRun.id, status: 'cancelled' })}><X />取消</Button>}</div></div><div className="mt-4 grid gap-3 md:grid-cols-2"><div className="border border-border p-3"><h3 className="text-xs font-medium text-muted-foreground">生产阶段</h3>{(displayedRun.stage_executions ?? []).length === 0 ? <p className="mt-2 text-sm text-muted-foreground">阶段记录尚未生成</p> : <ul className="mt-2 divide-y divide-border">{displayedRun.stage_executions?.map(stage => <li key={stage.id} className="py-2 text-sm"><div className="flex justify-between gap-2"><span>{runStageLabels[stage.stage] || stage.stage}</span><span className="text-muted-foreground">{stage.status}</span></div>{stage.error_message && <p className="mt-1 text-xs text-destructive">{stage.error_message}</p>}<p className="mt-1 text-xs text-muted-foreground">重试 {stage.retry_count} 次 · 已结算 {stage.settled_credits} 积分</p></li>)}</ul>}</div><div className="border border-border p-3"><h3 className="text-xs font-medium text-muted-foreground">审计记录</h3>{(displayedRun.audit_events ?? []).length === 0 ? <p className="mt-2 text-sm text-muted-foreground">暂无状态变更记录</p> : <ul className="mt-2 max-h-52 divide-y divide-border overflow-auto">{displayedRun.audit_events?.slice().reverse().map(event => <li key={event.id} className="py-2 text-xs"><div className="flex justify-between gap-2"><span>{event.reason_code || event.entity_kind}</span><span className="text-muted-foreground">{new Date(event.created_at).toLocaleString('zh-CN')}</span></div><p className="mt-1 text-muted-foreground">{event.from_state || '初始'} → {event.to_state}</p></li>)}</ul>}</div></div></div>}
    </section>}

    {detailAssetId && <section className="space-y-4 border-t border-border pt-5" aria-label="选题资产内容链路">
      <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-sm font-semibold">{detail.data?.asset.title || '内容链路'}</h2><div className="flex items-center gap-2"><select aria-label="评分目标项目" className="h-9 max-w-64 rounded-md border border-input bg-background px-3 text-sm" value={scoreProjectId} onChange={event => setScoreProjectId(event.target.value)}><option value="">选择评分项目</option>{(projects.data ?? []).map(project => <option key={project.id} value={project.id}>{project.name} · {project.platform}</option>)}</select><Button size="sm" variant="outline" disabled={!scoreProjectId || scoreAsset.isPending} onClick={() => scoreAsset.mutate()}><Sparkles />更新平台评分</Button></div></div>
      {detail.isPending && <p className="text-sm text-muted-foreground">正在加载内容链路…</p>}
      {detail.data && <>
        <div className="grid gap-3 md:grid-cols-2">
          <div className="border border-border p-3"><h3 className="text-xs font-medium text-muted-foreground">关联内容</h3>{detail.data.links.length === 0 ? <p className="mt-2 text-sm text-muted-foreground">暂无来源或派生内容</p> : <ul className="mt-2 divide-y divide-border">{detail.data.links.map(link => <li key={link.id} className="flex flex-wrap justify-between gap-2 py-2 text-sm"><span>{link.relation === 'origin' ? '来源' : link.relation === 'derivative' ? '派生' : '参考'} · {link.content_kind}</span><span className="text-muted-foreground">{link.platform || '未标平台'}{link.angle ? ` · ${link.angle}` : ''}</span></li>)}</ul>}</div>
          <div className="border border-border p-3"><h3 className="text-xs font-medium text-muted-foreground">平台评分记录</h3>{detail.data.scores.length === 0 ? <p className="mt-2 text-sm text-muted-foreground">尚无评分记录</p> : <ul className="mt-2 divide-y divide-border">{detail.data.scores.slice(0, 5).map(score => { const missing = Array.isArray(score.snapshot.missing) ? score.snapshot.missing.join('、') : ''; const label = score.status === 'scored' ? `${score.score.toFixed(0)} 分` : score.status === 'exploratory' ? `探索信号 ${score.score.toFixed(0)}` : '数据不足'; return <li key={score.id} className="py-2 text-sm"><div className="flex justify-between gap-2"><span>{score.platform} · {label}</span><span className="text-muted-foreground">{score.data_as_of ? `截至 ${new Date(score.data_as_of).toLocaleDateString('zh-CN')}` : '无统计日期'}</span></div><p className="mt-1 text-xs text-muted-foreground">revision {score.analytics_revision} · {score.confidence === 'high' ? '高置信度' : score.confidence === 'medium' ? '中置信度' : '低置信度'}{missing ? ` · 缺少 ${missing}` : ''}</p></li> })}</ul>}</div>
        </div>
        {scoreAsset.isError && <p role="alert" className="text-sm text-destructive">评分失败，请检查目标项目和分析数据状态。</p>}
      </>}
    </section>}

    <Dialog open={!!reuseAsset} onOpenChange={open => { if (!open) setReuseAsset(null) }}>
      <DialogContent>
        <DialogHeader><DialogTitle>跨平台复用</DialogTitle><DialogDescription>为“{reuseAsset?.title}”创建自动生产运行。系统会按目标项目的平台生成独立内容版本。</DialogDescription></DialogHeader>
        <label className="space-y-1.5 text-sm"><span>目标项目</span><select aria-label="目标项目" className="h-10 w-full rounded-md border border-input bg-background px-3" value={targetProjectId} onChange={event => setTargetProjectId(event.target.value)}>
          <option value="">选择项目</option>{(projects.data ?? []).map(project => <option key={project.id} value={project.id}>{project.name} · {project.platform}</option>)}
        </select></label>
        {projects.isPending && <p className="text-sm text-muted-foreground">正在加载项目…</p>}
        {projects.data?.length === 0 && <p className="text-sm text-muted-foreground">请先创建可用于内容生产的项目。</p>}
        {createReuseRun.isError && <p role="alert" className="text-sm text-destructive">创建自动生产运行失败，请稍后重试。</p>}
        <DialogFooter><Button variant="outline" onClick={() => setReuseAsset(null)}>取消</Button><Button disabled={!targetProjectId || createReuseRun.isPending || projects.isPending} onClick={() => createReuseRun.mutate()}><Sparkles />开始复用</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
}
