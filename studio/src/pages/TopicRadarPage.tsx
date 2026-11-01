import { useMemo, useState } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { SubscriptionSettings } from '@/components/topic-radar/SubscriptionSettings'
import { DigestEffects } from '@/components/topic-radar/DigestEffects'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Play, RefreshCw, Video } from 'lucide-react'
import { toast } from 'sonner'
import PageHeader from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import type { TopicDigest, TopicSuggestion } from '@/types/topic-radar'

const scoreNames: Record<string, string> = {
  traffic_potential: '流量潜力', account_fit: '账号匹配', differentiation: '差异化', timeliness: '时效性',
  monetization: '变现潜力', production_cost: '易制作程度', compliance_risk: '合规安全度', score_total: '综合评分',
}
const activeStatuses = new Set(['queued', 'preparing', 'dispatched', 'running', 'finalizing'])
const statusNames: Record<string, string> = {
  queued: '排队中', preparing: '准备中', dispatched: '已派发', running: '生成中', finalizing: '整理结果',
  succeeded: '已完成', failed: '失败', pending: '待入库', processing: '入库中', adopted: '已入库', duplicate: '重复选题',
}
export default function TopicRadarPage() {
  const [params] = useSearchParams()
  const [projectId, setProjectId] = useState(params.get('project_id') ?? '')
  const [subscriptionId, setSubscriptionId] = useState('')
  const [settingsOpen,setSettingsOpen] = useState(false)
  const [digestId, setDigestId] = useState(params.get('digest_id') ?? '')
  const [scriptTarget, setScriptTarget] = useState<{ suggestion: TopicSuggestion; platform: string; duration: string } | null>(null)
  const queryClient = useQueryClient()
  const projectsQuery = useQuery({ queryKey: queryKeys.projects.list({ status: 'active' }), queryFn: () => api.projects.list({ status: 'active' }) })
  const projects = projectsQuery.data ?? []
  const selectedProjectId = projects.some((project) => project.id === projectId) ? projectId : projects[0]?.id ?? ''
  const subscriptionQuery = useQuery({
    queryKey: queryKeys.topicRadar.subscriptions(selectedProjectId),
    queryFn: () => api.topicRadar.subscriptions(selectedProjectId), enabled: Boolean(selectedProjectId),
  })
  const subscriptions = subscriptionQuery.data?.items ?? []
  const subscription = subscriptions.find((item) => item.id === subscriptionId) ?? subscriptions[0]
  const digestsQuery = useQuery({
    queryKey: queryKeys.topicRadar.digests(subscription?.id ?? ''),
    queryFn: () => api.topicRadar.digests(subscription!.id), enabled: Boolean(subscription?.id),
    refetchInterval: (query) => query.state.data?.items?.some((digest) => activeStatuses.has(digest.status)) ? 2500 : false,
  })
  const digests = digestsQuery.data?.items ?? []
  const selectedDigestId = digestId || digests[0]?.id || ''
  const suggestionsQuery = useQuery({
    queryKey: ['topic-radar', 'detail', selectedDigestId],
    queryFn: () => api.topicRadar.digest(selectedDigestId), enabled: Boolean(selectedDigestId),
    refetchInterval: (query) => {
      const detail = query.state.data
      if (!detail) return false
      return activeStatuses.has(detail.digest.status) || detail.suggestions.some((item) => ['pending','processing'].includes(item.adoption_status)) || detail.scripts.some((item) => activeStatuses.has(item.status)) || detail.notifications.some((item) => ['pending','sending'].includes(item.status) || (item.status === 'failed' && item.attempt < 8)) ? 2500 : false
    },
  })
  const selectedDigest = suggestionsQuery.data?.digest ?? digests.find((digest) => digest.id === selectedDigestId)
  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['topic-radar', 'detail', selectedDigestId] }),
      queryClient.invalidateQueries({ queryKey: queryKeys.topicRadar.digests(subscription?.id ?? '') }),
      queryClient.invalidateQueries({ queryKey: queryKeys.topicRadar.subscriptions(selectedProjectId) }),
    ])
  }
  const createMutation = useMutation({
    mutationFn: () => api.topicRadar.createSubscription(selectedProjectId, { name: '选题雷达', enabled: false, timezone: 'Asia/Shanghai' }),
    onSuccess: async () => { await invalidate(); toast.success('选题雷达已创建') },
    onError: () => toast.error('创建选题雷达失败'),
  })
  const runMutation = useMutation({
    mutationFn: () => api.topicRadar.run(subscription!.id, { request_key: crypto.randomUUID() }),
    onSuccess: async (digest: TopicDigest) => { setDigestId(digest.id); await invalidate(); toast.success('选题分析已启动') },
    onError: () => toast.error('启动分析失败，请稍后重试'),
  })
  const scriptMutation = useMutation({
    mutationFn: () => api.topicRadar.requestScript(scriptTarget!.suggestion.id, {
      target_platform: scriptTarget!.platform, duration_seconds: Number(scriptTarget!.duration), aspect_ratio: '9:16',
    }),
    onSuccess: async () => { await invalidate(); toast.success('脚本生成任务已提交'); setScriptTarget(null) },
    onError: () => toast.error('脚本任务提交失败'),
  })
  const retryDigest = useMutation({ mutationFn: () => api.topicRadar.retryDigest(selectedDigestId), onSuccess: async (digest) => { setDigestId(digest.id); await invalidate() }, onError: () => toast.error('重新分析失败') })
  const suggestions = suggestionsQuery.data?.suggestions ?? []
  const projectLabel = useMemo(() => projects.find((project) => project.id === selectedProjectId)?.name ?? '', [projects, selectedProjectId])

  return <div className="mx-auto w-full max-w-7xl space-y-5 p-4 md:p-6">
    <PageHeader title="选题雷达" description="项目趋势、选题证据与脚本任务" />
    <section className="flex flex-wrap items-end gap-3 border-b pb-4">
      <label className="grid min-w-56 gap-1 text-xs font-medium text-muted-foreground">项目
        <select aria-label="选择项目" className="h-9 rounded-md border bg-background px-3 text-sm text-foreground" value={selectedProjectId} onChange={(event) => { setProjectId(event.target.value); setDigestId(''); setSubscriptionId('') }}>
          {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
        </select>
      </label>
      {subscriptions.length > 1 && <label className="grid gap-1 text-xs">订阅<select className="h-9 rounded-md border bg-background px-2" value={subscription?.id} onChange={(event) => {setSubscriptionId(event.target.value);setDigestId('')}}>{subscriptions.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}
      {subscription && <div className="min-w-40 text-sm"><div className="text-xs text-muted-foreground">雷达订阅</div><div className="mt-1 font-medium">{subscription.name} <span className="text-xs text-muted-foreground">{subscription.enabled ? '定时开启' : '手动运行'}</span></div></div>}
      {!subscription && selectedProjectId && <Button variant="outline" disabled={createMutation.isPending || subscriptionQuery.isPending} onClick={() => createMutation.mutate()}>{createMutation.isPending && <Loader2 className="size-4 animate-spin" />}创建选题雷达</Button>}
      {subscription && <Button disabled={runMutation.isPending || subscriptionQuery.isPending} onClick={() => runMutation.mutate()}><Play className="size-4" />手动分析</Button>}
      {subscription && <Button variant="outline" onClick={() => setSettingsOpen(true)}>订阅配置</Button>}
      {subscription?.next_run_at && <p className="text-xs text-muted-foreground">下次：{new Date(subscription.next_run_at).toLocaleString()} · {subscription.timezone}</p>}
      {subscription && <Button variant="outline" size="icon" aria-label="刷新历史" onClick={() => void invalidate()}><RefreshCw className="size-4" /></Button>}
      {selectedProjectId && <Link className="text-sm underline" to={`/topic-pool?project_id=${selectedProjectId}`}>选题库</Link>}
      <Link className="text-sm underline" to="/settings#publishing-settings">通知设置</Link>
      {projectLabel && <span className="ml-auto text-xs text-muted-foreground">{projectLabel}</span>}
    </section>
    {projectsQuery.isPending && <p className="py-8 text-sm text-muted-foreground">正在加载项目…</p>}
    {projectsQuery.isError && <p role="alert" className="text-destructive">项目加载失败，请重试。</p>}
    {!projectsQuery.isError && !projectsQuery.isPending && projects.length === 0 && <p className="py-8 text-sm text-muted-foreground">暂无可用项目</p>}
    {selectedProjectId && subscriptionQuery.isError && <p role="alert" className="text-sm text-destructive">订阅加载失败，请刷新重试。</p>}
    {selectedProjectId && !subscriptionQuery.isPending && !subscription && <p className="text-sm text-muted-foreground">该项目尚未创建雷达配置。</p>}
    {subscription && <div className="grid min-h-[420px] gap-5 lg:grid-cols-[280px_minmax(0,1fr)]">
      <aside className="border-r pr-4">
        <div className="mb-3 flex items-center justify-between"><h2 className="text-sm font-semibold">运行历史</h2><span className="text-xs text-muted-foreground">{digests.length}</span></div>
        {digestsQuery.isPending && <p className="py-4 text-xs text-muted-foreground">加载中…</p>}
        {digestsQuery.isError && <p role="alert">运行历史加载失败</p>}
        <div className="space-y-1" role="list" aria-label="运行历史">
          {digests.map((digest) => <button key={digest.id} type="button" onClick={() => setDigestId(digest.id)} aria-current={digest.id === selectedDigestId ? 'true' : undefined} className={`w-full rounded-md border-l-2 px-3 py-2.5 text-left transition-colors ${digest.id === selectedDigestId ? 'border-primary bg-muted' : 'border-transparent hover:bg-muted/60'}`}>
            <span className="flex items-center justify-between text-sm font-medium"><span>{statusNames[digest.status] ?? digest.status}</span><span className="text-xs text-muted-foreground">{digest.candidate_count} 条</span></span>
            <span className="mt-1 block text-xs text-muted-foreground">{new Date(digest.created_at || digest.planned_at).toLocaleString('zh-CN')}</span>
          </button>)}
          {!digestsQuery.isPending && digests.length === 0 && <p className="py-5 text-xs text-muted-foreground">暂无运行记录</p>}
        </div>
      </aside>
      <main className="min-w-0">
        {selectedDigest && <div className="mb-4 flex flex-wrap items-center justify-between gap-2 border-b pb-3"><div><h2 className="text-sm font-semibold">建议选题</h2><p className="mt-1 text-xs text-muted-foreground">运行于 {new Date(selectedDigest.created_at || selectedDigest.planned_at).toLocaleString('zh-CN')}</p></div><span className="text-xs text-muted-foreground">{statusNames[selectedDigest.status] ?? selectedDigest.status}{selectedDigest.error ? ` · ${selectedDigest.error}` : ''}</span></div>}
        {selectedDigest?.status === 'failed' && <Button variant="outline" disabled={retryDigest.isPending} onClick={() => retryDigest.mutate()}>重新分析</Button>}
        {suggestionsQuery.isError && <p role="alert" className="text-destructive">结果读取失败，请刷新重试。</p>}
        {selectedDigestId && suggestionsQuery.isPending && <p className="py-8 text-sm text-muted-foreground">正在载入选题…</p>}
        <div className="divide-y">
          {suggestions.map((suggestion) => <article key={suggestion.id} className="grid gap-4 py-4 xl:grid-cols-[minmax(0,1fr)_260px]">
            <div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><span className="text-xs font-semibold text-muted-foreground">#{suggestion.rank}</span><h3 className="text-base font-semibold">{suggestion.title}</h3><span className="rounded border px-1.5 py-0.5 text-[11px] text-muted-foreground">{statusNames[suggestion.adoption_status] ?? suggestion.adoption_status}</span></div>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">{suggestion.introduction}</p>
              <p className="mt-3 text-xs"><span className="font-medium">来源依据：</span>{suggestion.source_summary}</p>
              <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">{suggestion.source_refs?.map((source, index) => <a key={`${source.ref_id ?? source.url}-${index}`} href={source.url && /^https?:\/\//i.test(source.url) ? source.url : undefined} target="_blank" rel="noreferrer" className="underline-offset-2 hover:underline" aria-label={`来源：${source.title || source.label || source.platform || source.type}`}>
                {source.platform || source.label || source.type}{source.title ? ` · ${source.title}` : ''}{source.rank ? ` #${source.rank}` : ''}{source.freshness ? ` · ${source.freshness}` : ''}</a>)}</div>
              {suggestionsQuery.data?.adoptions.filter((item) => item.suggestion_id === suggestion.id).map((adoption) => <div key={adoption.id} className="mt-2 text-xs">{adoption.topic_pool_id && <Link className="underline" to={`/topic-pool?project_id=${selectedProjectId}`}>查看选题库 #{adoption.topic_pool_id}</Link>}{adoption.error && <p className="text-destructive">入库失败：{adoption.error}</p>}</div>)}
              {suggestion.video_fit && <p className="mt-2 text-xs text-muted-foreground">视频适配：{suggestion.video_fit}</p>}
            </div>
            <div className="space-y-3 xl:border-l xl:pl-4"><dl className="grid grid-cols-2 gap-x-3 gap-y-2">{Object.entries(suggestion.scores ?? {}).map(([key, score]) => <div key={key} className="flex justify-between gap-2 text-xs"><dt className="text-muted-foreground">{scoreNames[key] ?? key}</dt><dd className="font-medium tabular-nums">{Math.round(score * 100)}%</dd></div>)}</dl>
              <Button size="sm" variant="outline" onClick={() => setScriptTarget({ suggestion, platform: 'douyin', duration: '60' })}><Video className="size-4" />生成短视频脚本</Button>
            </div>
          </article>)}
          {!suggestionsQuery.isPending && selectedDigest?.status === 'succeeded' && suggestions.length === 0 && <p className="py-8 text-sm text-muted-foreground">本次运行没有建议选题</p>}
          {!selectedDigest && !digestsQuery.isPending && <p className="py-8 text-sm text-muted-foreground">启动一次手动分析以查看选题结果。</p>}
        </div>
        {suggestionsQuery.data && <DigestEffects detail={suggestionsQuery.data} />}
      </main>
    </div>}
    {settingsOpen && subscription && <SubscriptionSettings key={subscription.id} subscription={subscription} onClose={() => setSettingsOpen(false)} onSaved={invalidate} onDeleted={async () => { setSubscriptionId(''); setDigestId(''); setSettingsOpen(false); await queryClient.invalidateQueries({ queryKey: queryKeys.topicRadar.subscriptions(selectedProjectId) }) }} />}
    {scriptTarget && <Dialog open onOpenChange={(open) => {if(!open)setScriptTarget(null)}}><DialogContent className="sm:max-w-md">
      <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); scriptMutation.mutate() }}>
        <DialogHeader><DialogTitle>请求生成脚本</DialogTitle></DialogHeader><p className="text-sm text-muted-foreground">{scriptTarget.suggestion.title}</p>
        <label className="grid gap-1 text-sm">发布平台<select className="h-9 rounded-md border bg-background px-2" value={scriptTarget.platform} onChange={(event) => setScriptTarget({ ...scriptTarget, platform: event.target.value })}><option value="douyin">抖音</option><option value="channels">视频号</option><option value="bilibili">B站</option><option value="xiaohongshu">小红书</option></select></label>
        <label className="grid gap-1 text-sm">时长（秒）<input className="h-9 rounded-md border bg-background px-2" type="number" min="15" max="300" step="15" value={scriptTarget.duration} onChange={(event) => setScriptTarget({ ...scriptTarget, duration: event.target.value })} /></label>
        <div className="flex justify-end gap-2"><Button type="button" variant="outline" onClick={() => setScriptTarget(null)}>取消</Button><Button type="submit" disabled={scriptMutation.isPending || Number(scriptTarget.duration) < 15}>{scriptMutation.isPending && <Loader2 className="size-4 animate-spin" />}提交任务</Button></div>
      </form>
    </DialogContent></Dialog>}
  </div>
}
