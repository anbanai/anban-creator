import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import PageHeader from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from '@/components/ui/sheet'
import { TopicDiscovery, TopicDetail } from '@/components/topics/TopicPanels'
import ContentAutopilotSettingsPage from './ContentAutopilotSettingsPage'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { allTopicPersonas, invalidateTopics, topicFilters, topicPageSize, topicUsage, isTopicScript, topicOutputHref, topicOutputSummaries } from '@/lib/topics'
const selectClass = 'h-10 rounded-md border bg-background px-3 text-sm'
const scrollPositions = new Map<string, number>()
export default function TopicsPage() {
  const [params, setParams] = useSearchParams()
  const paramsRef = useRef(params)
  paramsRef.current = params
  const location = useLocation()
  const client = useQueryClient()
  const filters = topicFilters(params)
  const assetId = params.get('asset_id') ?? ''
  const settings = params.get('settings') ?? ''
  const panel = params.get('detail') ?? ''
  const runId = params.get('run') ?? ''
  const [title, setTitle] = useState('')
  const [angle, setAngle] = useState('')
  const listRef = useRef<HTMLElement>(null)
  const assets = useQuery({ queryKey: queryKeys.topics.list(filters), queryFn: () => api.contentAssets.list(filters), staleTime: 30000, placeholderData: keepPreviousData })
  const projectsQuery = useQuery({ queryKey: queryKeys.projects.list({ status: 'active' }), queryFn: () => api.projects.list({ status: 'active' }) })
  const projects = projectsQuery.data ?? []
  const projectId = params.has('target_project_id')
    ? params.get('target_project_id') === allTopicPersonas ? '' : params.get('target_project_id') ?? ''
    : filters.project_id || ''
  const detail = useQuery({ queryKey: queryKeys.topics.detail(assetId), queryFn: () => api.contentAssets.get(assetId), enabled: !!assetId })
  function update(values: Record<string, string | undefined>, replace = false) {
    const viewport = listRef.current?.closest('main') ?? document.scrollingElement
    if (viewport) scrollPositions.set(location.key, viewport.scrollTop)
    const current = paramsRef.current
    const next = new URLSearchParams(current); if ('asset_id' in values && values.asset_id !== current.get('asset_id')) { next.delete('source_task_id'); next.delete('script_id'); next.delete('reuse_run') } for (const [key, value] of Object.entries(values)) { if (value) next.set(key, value); else next.delete(key) } paramsRef.current = next
    setParams(next, { replace, preventScrollReset: true })
  }
  const restoredKey = useRef<string | null>(null)
  useLayoutEffect(() => {
    const viewport = listRef.current?.closest('main') ?? document.scrollingElement
    if (!viewport) return
    const save = () => { if (restoredKey.current === location.key) scrollPositions.set(location.key, viewport.scrollTop) }
    viewport.addEventListener('scroll', save)
    return () => { save(); viewport.removeEventListener('scroll', save) }
  }, [location.key])
  useLayoutEffect(() => {
    if (!assets.isSuccess || assets.isPlaceholderData || restoredKey.current === location.key) return
    const viewport = listRef.current?.closest('main') ?? document.scrollingElement
    if (viewport && scrollPositions.has(location.key)) viewport.scrollTop = scrollPositions.get(location.key)!
    restoredKey.current = location.key
  }, [location.key, assets.isSuccess, assets.isPlaceholderData])
  const close = () => update({ asset_id: undefined, detail: undefined, settings: undefined, run: undefined, script_id: undefined, reuse_run: undefined, source_task_id: undefined })
  const create = useMutation({ mutationFn: () => api.contentAssets.create({ title: title.trim(), core_question: angle.trim(), project_ids: projectId ? [projectId] : [] }), onSuccess: async asset => { setTitle(''); setAngle(''); update({ detail: undefined, asset_id: asset.asset_id }); await invalidateTopics(client) }, onError: () => toast.error('添加选题失败，请重试') })
  const archive = useMutation({ mutationFn: () => api.contentAssets.archive(assetId), onSuccess: async () => { close(); await invalidateTopics(client) }, onError: () => toast.error('归档失败，请重试') })
  useEffect(() => {
    if (archive.isPending || !assets.isSuccess || assets.isPlaceholderData || assets.isFetching) return
    const lastPage = Math.max(1, Math.ceil(assets.data.total / topicPageSize))
    if (filters.page > lastPage) update({ page: lastPage === 1 ? undefined : String(lastPage) }, true)
  }, [assets.data, assets.isSuccess, assets.isPlaceholderData, assets.isFetching, filters.page, archive.isPending])
  const filtered = !!(filters.q || filters.project_id || filters.usage)
  const items = assets.data?.items ?? []
  return <div className="mx-auto w-full max-w-5xl space-y-5 pb-10">
    <PageHeader title="选题" description="找到值得写的选题，用合适的人设开始创作。"><Button onClick={() => update({ detail: 'discover' })}>找新选题</Button><Button variant="outline" onClick={() => update({ detail: 'add' })}>添加选题</Button><Button variant="ghost" onClick={() => update({ settings: 'discovery' })}>自动化设置</Button></PageHeader>
    <div className="flex flex-wrap gap-3"><Input className="min-w-52 flex-1" aria-label="搜索选题" placeholder="搜索标题、切入角度" value={filters.q ?? ''} onChange={event => update({ q: event.target.value, page: undefined }, true)} /><select aria-label="所属人设筛选" className={selectClass} value={filters.project_id ?? ''} onChange={event => update({ project_id: event.target.value, target_project_id: undefined, page: undefined })}><option value="">全部人设</option>{projects.map(project => <option key={project.id} value={project.id}>{project.name}</option>)}</select><select aria-label="人设使用状态" className={selectClass} value={filters.usage ?? ''} onChange={event => update({ usage: event.target.value, page: undefined })}><option value="">全部使用状态</option><option value="unused">未使用</option><option value="used">已使用</option></select></div>
    {assets.isPending && <p role="status">正在加载选题…</p>}{assets.isError && <div role="alert">选题加载失败。<Button variant="outline" onClick={() => void assets.refetch()}>重试</Button></div>}
    {!assets.isPending && !assets.isError && assets.data?.total === 0 && <section className="rounded-lg border border-dashed py-16 text-center"><h2 className="font-medium">{filtered ? '没有符合筛选条件的选题' : '你的第一批选题，从这里开始'}</h2><p className="my-3 text-sm text-muted-foreground">{filtered ? '试试其他关键词或人设。' : '按人设发现值得创作的内容，也可以添加自己的灵感。'}</p><Button onClick={() => filtered ? update({ q: undefined, project_id: undefined, usage: undefined, page: undefined }) : update({ detail: 'discover' })}>{filtered ? '清除筛选' : '找第一批选题'}</Button></section>}
    <section ref={listRef} aria-label="选题列表" className="divide-y rounded-lg border">{items.map(asset => <article key={asset.asset_id} className="p-4 sm:p-5"><div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-start sm:justify-between sm:gap-4"><button className="min-w-0 break-words text-left font-semibold hover:text-primary sm:min-w-64 sm:flex-1" onClick={() => update({ asset_id: asset.asset_id })}>{asset.title}</button><span className="min-w-0 break-words text-xs text-muted-foreground sm:max-w-[50%]">{filters.project_id ? (topicUsage(asset, filters.project_id) === 'used' ? '已使用' : '未使用') : asset.project_uses?.length ? asset.project_uses.map(use => `${use.project_name}：${use.status === 'used' ? '已使用' : '未使用'}`).join(' · ') : '尚未用于创作'}</span></div><p className="mt-2 line-clamp-2 text-sm text-muted-foreground">{asset.core_question || '打开选题，补充你的创作角度。'}</p><div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">{filters.project_id && <span>{asset.project_uses?.find(use => use.project_id === filters.project_id)?.project_name || '当前人设'}</span>}{topicOutputSummaries(asset).map(output => <Link key={output.job_id || output.task_id} to={topicOutputHref(asset.asset_id, output, params)!} className="underline">{isTopicScript(output) ? '脚本' : '作品'}：{output.status === 'completed' || output.status === 'succeeded' ? '已完成' : output.status === 'failed' ? '失败' : '生成中'}</Link>)}<Button className="ml-auto" size="sm" variant="ghost" onClick={() => update({ asset_id: asset.asset_id })}>查看选题</Button></div></article>)}</section>
    {(assets.data?.total ?? 0) > topicPageSize && <nav aria-label="选题分页" className="flex items-center justify-between"><span className="text-sm text-muted-foreground">共 {assets.data?.total} 个 · 第 {filters.page} 页</span><div className="flex gap-2"><Button variant="outline" disabled={filters.page === 1} onClick={() => update({ page: String(filters.page - 1) })}>上一页</Button><Button variant="outline" disabled={filters.page * topicPageSize >= (assets.data?.total ?? 0)} onClick={() => update({ page: String(filters.page + 1) })}>下一页</Button></div></nav>}
    <Sheet open={!!(assetId || settings || panel || runId)} onOpenChange={open => { if (!open) close() }}><SheetContent className="!w-full !max-w-2xl overflow-y-auto"><SheetHeader><SheetTitle>{settings ? '自动化设置' : panel === 'add' ? '添加选题' : panel === 'discover' || runId ? '找新选题' : detail.data?.asset.title || '选题详情'}</SheetTitle><SheetDescription>选题、创作与结果都保留在同一处。</SheetDescription></SheetHeader><div className="space-y-5 px-5 pb-8">
      <label className="grid gap-2 text-sm">{assetId ? '创作人设' : '人设'}<select aria-label="操作人设" className={selectClass} value={panel === 'add' && params.get('target_project_id') === allTopicPersonas ? allTopicPersonas : projectId} onChange={event => update({ target_project_id: panel === 'add' && event.target.value === allTopicPersonas ? allTopicPersonas : event.target.value, subscription_id: undefined, run: undefined })}><option value={panel === 'add' ? allTopicPersonas : ''}>{panel === 'add' ? '全部人设可用' : '请选择人设'}</option>{projects.map(project => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
      {projectsQuery.isError && <p role="alert">人设加载失败。<Button onClick={() => void projectsQuery.refetch()}>重试</Button></p>}{projectsQuery.isSuccess && projects.length === 0 && <Link to="/projects" className="underline">先创建人设</Link>}
      {panel === 'add' && <form className="space-y-4" onSubmit={event => { event.preventDefault(); create.mutate() }}><label className="grid gap-2">选题标题<Input required maxLength={200} value={title} onChange={event => setTitle(event.target.value)} /></label><label className="grid gap-2">切入角度<Input value={angle} onChange={event => setAngle(event.target.value)} /></label><Button type="submit" disabled={!title.trim() || create.isPending}>添加选题</Button></form>}
      {(settings === 'discovery' || panel === 'discover' || runId) && <TopicDiscovery projectId={projectId} settings={settings === 'discovery'} params={params} update={update} />}
      {settings && <><details open={settings === 'production'}><summary className="cursor-pointer font-medium">内容复用与自动生产</summary><ContentAutopilotSettingsPage /></details><Link className="block text-sm underline" to="/settings#publishing-settings">通知设置</Link></>}
      {assetId && !settings && panel !== 'discover' && panel !== 'add' && <>{detail.isPending && <p role="status">正在加载选题详情…</p>}{detail.isError && <div role="alert">详情加载失败。<Button onClick={() => void detail.refetch()}>重试</Button></div>}{detail.data && <TopicDetail asset={detail.data.asset} projectId={projectId} project={projects.find(project => project.id === projectId)} params={params} update={update} />}<Button variant="ghost" disabled={archive.isPending} onClick={() => archive.mutate()}>归档选题</Button></>}
    </div></SheetContent></Sheet>
  </div>
}
