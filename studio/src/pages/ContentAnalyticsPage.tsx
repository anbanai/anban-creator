import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { Activity, AlertCircle, ArrowLeft, ArrowUpDown, Check, ChevronDown, ChevronLeft, ChevronRight, History, Layers3, Loader2, Search, Upload, X } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { defaultAnalyticsPeriod, periodError, shanghaiDay, type AnalyticsPeriod } from '@/lib/analytics-period'
import { contentAnalyticsApi, contentTypeLabel, contentTypesFor, descriptionFor, formatMetric, metricLabel, isAnalyticsRevisionConflict, metricsFor, parseTargetKey, seednoteAnalyticsViews, seednoteMetricsForView, type AnalyticsOverview, type AnalyticsParams, type AnalyticsPlatform, type SeednoteAnalyticsViewKey } from '@/lib/content-analytics'
import { useDebouncedValue } from '@/hooks/use-debounced-value'
import AnalyticsTrend from '@/components/analytics/AnalyticsTrend'
import AnalyticsPeriodControls from '@/components/analytics/AnalyticsPeriodControls'
import ContentImportDialog, { type InitialContentImportUpload } from '@/components/analytics/ContentImportDialog'
import ContentImportHistory from '@/components/analytics/ContentImportHistory'
import WechatAccountImportDialog from '@/components/analytics/WechatAccountImportDialog'
import WechatAccountImportHistory from '@/components/analytics/WechatAccountImportHistory'
import ContentSourceLink from '@/components/common/ContentSourceLink'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import PageHeader from '@/components/layout/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { agentDisplayName } from '@/components/agent-prompt/AgentIconStack'
import { platformIcon, platformIconColor } from '@/lib/PlatformIcon'
import SeednoteOfficialAnalytics from '@/components/analytics/SeednoteOfficialAnalytics'
import WechatAccountOverviewPanel from '@/components/analytics/WechatAccountOverview'
import WechatTrafficSources from '@/components/analytics/WechatTrafficSources'
import { wechatAccountAnalyticsApi, wechatAccountMetrics, wechatTrafficMetrics, wechatTrafficSources, type WechatAccountMetric, type WechatTrafficSource } from '@/lib/wechat-account-analytics'
import type { Project } from '@/types'
import ContentLineagePanel from '@/components/tasks/ContentLineagePanel'

const tagGroupLabels: Record<string, string> = { content_fact: '内容事实', editorial_structure: '编辑结构', strategy_signal: '策略信号', source_risk: '来源与风险', media_performance: '媒体与表现' }
function ContentTags({ tags }: { tags?: import('@/lib/content-analytics').AnalyticsContentTag[] }) {
  if (!tags?.length) return null
  const groups = tags.reduce<Record<string, typeof tags>>((all, tag) => { (all[tag.group || 'content_fact'] ||= []).push(tag); return all }, {})
  return <div aria-label="内容标签" className="space-y-2">{Object.entries(groups).map(([group, items]) => <div key={group}><p className="mb-1 text-[11px] font-medium text-muted-foreground">{tagGroupLabels[group] || group}</p><div className="flex flex-wrap gap-1.5">{items.map(tag => <span key={`${tag.dimension}:${tag.value}`} data-content-tag className="rounded-md bg-muted px-2 py-1 text-xs text-muted-foreground" title={`置信度 ${(tag.confidence ?? 0).toFixed(2)} · ${tag.evidence_available ? '有证据' : '证据缺失'}`}>{tag.display_name}</span>)}</div></div>)}</div>
}

function rememberKey(userId?: string) { return `content-analytics:account:${userId || 'anonymous'}` }
function readRemembered(userId?: string) { try { return localStorage.getItem(rememberKey(userId)) || '' } catch { return '' } }
function readPeriod(params: URLSearchParams): AnalyticsPeriod {
  const defaults = defaultAnalyticsPeriod()
  const from = params.get('from') || defaults.from
  const to = params.get('to') || defaults.to
  const granularity = params.get('granularity')
  const preset = params.get('preset')
  return { from, to, preset: preset && ['yesterday', '7d', '30d', 'week', 'month', 'custom'].includes(preset) ? preset as AnalyticsPeriod['preset'] : params.has('from') ? 'custom' : defaults.preset, granularity: granularity === 'week' || granularity === 'month' ? granularity : 'day' }
}
const supportedAnalyticsAgents = new Set(['wechat', 'wechat-article', 'wechat-picture', 'seednote'])

function analyticsPlatformForAgent(agentID: string): AnalyticsPlatform {
  return agentID === 'seednote' ? 'seednote' : 'wechat'
}

function analyticsContentTypeForAgent(agentID: string) {
  return agentID === 'wechat-article' || agentID === 'wechat-picture' ? agentID : undefined
}

function analyticsAgentForContentType(contentType: string, agentIDs: string[]) {
  if (agentIDs.includes(contentType)) return contentType
  const contentPlatform = contentType === 'image_text' || contentType === 'video'
    ? 'seednote'
    : contentType.startsWith('wechat-') ? 'wechat' : undefined
  if (!contentPlatform) return undefined
  return agentIDs.find(agentID => analyticsPlatformForAgent(agentID) === contentPlatform)
}

function analyticsAgentsForProject(project?: Project, platformHint?: string) {
  const configuredAgents = project?.agent_ids ?? []
  const ids = [...new Set(configuredAgents.filter(agentID => supportedAnalyticsAgents.has(agentID)))]
  if (configuredAgents.length > 0) return ids
  if (project?.platform === 'wechat') return ['wechat']
  if (project?.platform === 'seednote') return ['seednote']
  if (!project?.platform && (platformHint === 'seednote' || platformHint === 'wechat')) {
    return [platformHint === 'seednote' ? 'seednote' : 'wechat']
  }
  return []
}

function AnalyticsAgentSwitcher({ agentIDs, selectedAgent, onSelect }: { agentIDs: string[]; selectedAgent: string; onSelect: (agentID: string) => void }) {
  return (
    <div role="group" aria-label="选择分析 Agent" className="ml-auto flex items-center gap-1.5">
      {agentIDs.map(agentID => {
        const Icon = platformIcon[agentID] ?? Layers3
        const label = agentDisplayName(agentID)
        const selected = agentID === selectedAgent
        return (
          <Tooltip key={agentID}>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  aria-label={`选择${label}`}
                  aria-pressed={selected}
                  onClick={() => onSelect(agentID)}
                  className={`flex size-11 items-center justify-center rounded-lg outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring ${selected ? 'text-primary' : `${platformIconColor[agentID] ?? 'text-muted-foreground'} hover:text-primary`}`}
                />
              }
            >
              <Icon aria-hidden="true" className="size-5" />
            </TooltipTrigger>
            <TooltipContent>{label}</TooltipContent>
          </Tooltip>
        )
      })}
    </div>
  )
}

function ContentSwitcher({ projectId, request, current, onSelect }: { projectId: string; request: AnalyticsParams; current: string; onSelect: (id: string) => void }) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const debounced = useDebouncedValue(search)
  const query = useQuery({ queryKey: ['content-analytics', projectId, 'picker', request, debounced, page], queryFn: ({ signal }) => contentAnalyticsApi.contents(projectId, { ...request, search: debounced, offset: page * 25, limit: 25 }, signal), enabled: open, placeholderData: keepPreviousData })
  return <Popover open={open} onOpenChange={setOpen}><PopoverTrigger render={<Button size="sm" variant="ghost" className="h-8 px-2 hover:bg-muted/70" />}>切换内容<ChevronDown /></PopoverTrigger><PopoverContent align="end" className="w-[min(440px,calc(100vw-32px))]"><Input autoFocus aria-label="搜索要切换的内容" placeholder="搜索内容标题" value={search} onChange={e => { setSearch(e.target.value); setPage(0) }} /><div className="max-h-80 overflow-y-auto">{query.data?.items.map(item => <button key={item.id} className="flex w-full items-start justify-between gap-3 rounded-md p-2.5 text-left text-sm hover:bg-muted" onClick={() => { onSelect(item.id); setOpen(false) }}><span>{item.title}</span>{current === item.id && <Check className="size-4 shrink-0 text-primary" />}</button>)}{query.isError && <p role="alert">{query.error.message}</p>}</div><div className="flex justify-between border-t pt-2"><Button size="sm" variant="ghost" disabled={page === 0 || query.isFetching} onClick={() => setPage(page - 1)}>上一页</Button><span className="text-xs">共 {query.data?.total ?? 0} 篇</span><Button size="sm" variant="ghost" disabled={!query.data || (page + 1) * 25 >= query.data.total || query.isFetching} onClick={() => setPage(page + 1)}>下一页</Button></div></PopoverContent></Popover>
}
function Observations({ projectId, contentId, request }: { projectId: string; contentId: string; request: AnalyticsParams }) {
  const [open, setOpen] = useState(false)
  const [page, setPage] = useState(0)
  const query = useQuery({ queryKey: ['content-analytics', projectId, 'observations', contentId, request, page], queryFn: ({ signal }) => contentAnalyticsApi.observations(projectId, contentId, { ...request, offset: page * 25, limit: 25 }, signal), enabled: open, placeholderData: keepPreviousData })
  return <section className="rounded-lg border p-4"><Button variant="outline" onClick={() => setOpen(!open)}>{open ? '收起观测记录' : '查看观测记录'}</Button>{open && <div className="mt-3 space-y-3">{query.isError && <p role="alert">{query.error.message}</p>}{query.isPending && <p role="status">正在读取记录…</p>}{query.data?.items.map(item => <div key={item.id} className="border-b py-2 text-xs"><p>{item.stat_date} · {item.source} · 累计{item.revoked_at ? ' · 已撤销' : ''}</p><p className="mt-1 text-muted-foreground">{Object.entries(item.metrics).map(([key, value]) => `${metricLabel(key)}：${formatMetric(key, value)}`).join(' · ')}</p></div>)}<div className="flex items-center justify-between"><Button size="sm" variant="ghost" disabled={page === 0 || query.isFetching} onClick={() => setPage(page - 1)}>上一页</Button><span className="text-xs">共 {query.data?.total ?? 0} 条</span><Button size="sm" variant="ghost" disabled={!query.data || (page + 1) * 25 >= query.data.total || query.isFetching} onClick={() => setPage(page + 1)}>下一页</Button></div></div>}</section>
}

function WechatContentCumulativeOverview({ overview, loading, error, metrics, selectedMetric, onMetricChange }: { overview?: AnalyticsOverview; loading?: boolean; error?: Error; metrics: ReturnType<typeof metricsFor>; selectedMetric: string; onMetricChange: (key: string) => void }) {
  const summaryMetrics = metrics.filter(item => item.summary)
  const metric = metrics.find(item => item.key === selectedMetric) ?? summaryMetrics[0] ?? metrics[0]
  return <section aria-label="内容累计总览" className="space-y-4">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div><h2 className="text-base font-semibold">内容累计总览</h2><p className="mt-1 text-xs text-muted-foreground">总览逐篇相加当前范围内每篇的最新累计值；各篇阅读人数可能重叠，不代表账号去重人数。</p></div>
      <span className="text-xs text-muted-foreground">统计口径：累计 · {overview ? `${overview.coverage.contents} 篇内容` : '等待数据'}</span>
    </div>
    {error && <Alert variant="destructive"><AlertCircle /><AlertDescription>内容累计数据暂时无法读取：{error.message}</AlertDescription></Alert>}
    <div className="grid grid-cols-2 overflow-hidden rounded-xl border bg-card sm:grid-cols-3 xl:flex" aria-label="内容累计关键指标">
      {summaryMetrics.map(item => <button key={item.key} type="button" aria-pressed={selectedMetric === item.key} onClick={() => onMetricChange(item.key)} className={`relative min-w-0 flex-1 border-b border-r px-4 py-4 text-left transition-colors hover:bg-muted/50 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-primary xl:border-b-0 ${selectedMetric === item.key ? 'bg-primary/[0.045]' : ''}`}>
        {selectedMetric === item.key && <span className="absolute inset-x-4 bottom-0 h-0.5 rounded-full bg-primary" />}<span className="text-xs text-muted-foreground">{item.label}</span><span className="mt-2 block text-2xl font-semibold tracking-tight tabular-nums">{loading || error || overview?.totals[item.key] == null ? '—' : item.format(overview.totals[item.key]!)}</span>
      </button>)}
    </div>
    <AnalyticsTrend title="内容累计趋势" metrics={metrics} data={error ? [] : overview?.series ?? []} selectedMetric={metric.key} onMetricChange={onMetricChange} loading={loading} description="按每篇内容的累计记录汇总；缺失记录保留为空，不补零。" emptyMessage={error ? '内容累计数据读取失败，请重试。' : '当前范围暂无内容累计数据，请导入公众号内容累计数据。'} />
  </section>
}

export default function ContentAnalyticsPage() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const queryClient = useQueryClient()
  const period = useMemo(() => readPeriod(params), [params])
  const [importOpen, setImportOpen] = useState(false)
  const [accountImportOpen, setAccountImportOpen] = useState(false)
  const [initialContentImportUpload, setInitialContentImportUpload] = useState<InitialContentImportUpload>()
  const [historyOpen, setHistoryOpen] = useState(false)
  const [accountHistoryOpen, setAccountHistoryOpen] = useState(false)
  const [trafficSourcesOpen, setTrafficSourcesOpen] = useState(false)
  const [notice, setNotice] = useState('')
  const [importedDate, setImportedDate] = useState('')
  const heading = useRef<HTMLHeadingElement>(null)
  const tableScroll = useRef(0)
  const restoreScroll = useRef(false)
  const previousContent = useRef<string | undefined>(undefined)
  const projectsQuery = useQuery({ queryKey: queryKeys.projects.list(), queryFn: () => api.projects.list() })
  const projects = projectsQuery.data ?? []
  const requestedAccount = params.get('account')
  const project = projects.find(p => p.id === requestedAccount) ?? projects.find(p => p.id === readRemembered(user?.id)) ?? projects[0]
  const requestedAgent = params.get('agent')
  const platformParam = params.get('platform')
  const agentIDs = useMemo(() => analyticsAgentsForProject(project, platformParam ?? undefined), [project, platformParam])
  const routeAgent = requestedAgent && agentIDs.includes(requestedAgent) ? requestedAgent : undefined
  const fallbackAgent = agentIDs.find(agentID => analyticsPlatformForAgent(agentID) === platformParam) ?? agentIDs[0] ?? ''
  const contentId = params.get('content') || undefined
  const requestAgent = routeAgent ?? fallbackAgent
  const analyticsPlatform: AnalyticsPlatform = requestAgent
    ? analyticsPlatformForAgent(requestAgent)
    : platformParam === 'seednote' ? 'seednote' : 'wechat'
  const accountAnalyticsEnabled = analyticsPlatform === 'wechat' && project?.platform === 'wechat'
  const wechatConfigQuery = useQuery({ queryKey: ['wechat-platform-config', project?.id], queryFn: () => api.projects.getWechatPlatformConfig(project!.id), enabled: Boolean(project?.id && accountAnalyticsEnabled && !contentId), staleTime: 60_000, retry: false })
  const accountConnection = wechatConfigQuery.data?.connection
  const canSyncAccountAnalytics = Boolean(accountConnection?.connected && accountConnection.can_read_account_analytics)
  // A detail ID is canonical. Resolve its actual type before applying any
  // Agent filter so stale or conflicting deep-link parameters cannot blank it.
  const agentContentType = contentId ? undefined : analyticsContentTypeForAgent(requestAgent)
  const metrics = metricsFor(analyticsPlatform)
  const requestedSeednoteView = params.get('view') as SeednoteAnalyticsViewKey | null
  const seednoteView: SeednoteAnalyticsViewKey = analyticsPlatform === 'seednote' && seednoteAnalyticsViews.some(item => item.key === requestedSeednoteView) ? requestedSeednoteView! : 'watch'
  const viewMetrics = analyticsPlatform === 'seednote' ? seednoteMetricsForView(seednoteView) : metrics
  const metric = viewMetrics.find(m => m.key === params.get('metric'))?.key || viewMetrics.find(m => m.key === (analyticsPlatform === 'seednote' ? 'view_count' : 'read_users'))?.key || viewMetrics.find(m => m.summary)?.key || viewMetrics[0].key
  const accountMetric = wechatAccountMetrics.some(item => item.key === params.get('account_metric')) ? params.get('account_metric') as WechatAccountMetric : canSyncAccountAnalytics ? 'follower_net_gain_count' : 'read_users'
  const accountSourceMetric = wechatTrafficMetrics.some(item => item.key === params.get('account_source_metric')) ? params.get('account_source_metric') as WechatAccountMetric : canSyncAccountAnalytics ? 'follower_gain_count' : 'read_users'
  const accountSource = wechatTrafficSources.some(item => item.value === params.get('account_source')) ? params.get('account_source') as WechatTrafficSource : 'all'
  const rangeError = periodError(period.from, period.to)
  const request = useMemo(() => ({ platform: analyticsPlatform, content_type: agentContentType, from: period.from, to: period.to, granularity: period.granularity, metric_basis: 'cumulative' as const }), [analyticsPlatform, agentContentType, period.from, period.to, period.granularity])
  const search = params.get('q') || ''
  const debouncedSearch = useDebouncedValue(search)
  const contentType = params.get('type') || 'all'
  const serverSortableMetrics = new Set(['date', 'title', 'exposure_count', 'view_count', 'like_count', 'comment_count', 'collect_count', 'follower_gain_count', 'share_count', 'barrage_count', 'read_users', 'share_users', 'read_to_follow_users', 'delivered_users'])
  const defaultSort = serverSortableMetrics.has(metric) ? metric : (metrics.find(item => serverSortableMetrics.has(item.key))?.key ?? metric)
  const sort = params.get('sort') && serverSortableMetrics.has(params.get('sort')!) ? params.get('sort')! : defaultSort
  const direction = params.get('direction') === 'asc' ? 'asc' : 'desc'
  const page = Math.max(1, Number.parseInt(params.get('page') || '1', 10) || 1)
  const enabled = Boolean(project && agentIDs.length > 0 && !rangeError)
  const overviewQuery = useQuery({ queryKey: ['content-analytics', project?.id, 'overview', request], queryFn: ({ signal }) => contentAnalyticsApi.overview(project!.id, request, signal), enabled: enabled && !contentId, staleTime: 60_000 })
  // The account overview stays account-wide. Source-specific trends and shares
  // belong to the traffic-source drilldown below, so changing its filter must
  // not replace the totals or comparisons shown here.
  const accountOverviewQuery = useQuery({ queryKey: ['wechat-account-analytics', project?.id, 'overview', period.from, period.to], queryFn: ({ signal }) => wechatAccountAnalyticsApi.overview(project!.id, { from: period.from, to: period.to }, signal), enabled: enabled && accountAnalyticsEnabled && !contentId, staleTime: 60_000, retry: false })
  const accountSyncMutation = useMutation({
    mutationFn: (range: { from: string; to: string }) => wechatAccountAnalyticsApi.sync(project!.id, range),
    onSuccess: async result => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['wechat-account-analytics', project?.id] }),
        queryClient.invalidateQueries({ queryKey: ['content-analytics', project?.id] }),
      ])
      setNotice(`已从微信同步 ${result.account_days} 天账号统计（${result.from} 至 ${result.to}）`)
    },
  })
  const detailQuery = useQuery({ queryKey: ['content-analytics', project?.id, 'detail', contentId, request], queryFn: ({ signal }) => contentAnalyticsApi.detail(project!.id, contentId!, request, signal), enabled: enabled && Boolean(contentId), staleTime: 60_000 })
  const contentTarget = parseTargetKey(contentId)
  const contentLineageQuery = useQuery({ queryKey: ['content-asset-lineage-by-content', project?.id, contentTarget?.kind, contentTarget?.id], queryFn: () => api.contentAssets.forContent({ project_id: project!.id, content_kind: contentTarget!.kind, content_id: contentTarget!.id }), enabled: enabled && Boolean(contentTarget), staleTime: 60_000 })
  const listQuery = useQuery({ queryKey: ['content-analytics', project?.id, 'contents', request, overviewQuery.data?.revision, debouncedSearch, contentType, sort, direction, page], queryFn: ({ signal }) => contentAnalyticsApi.contents(project!.id, { ...request, expected_revision: overviewQuery.data?.revision, search: debouncedSearch, content_type: agentContentType ?? (contentType === 'all' ? undefined : contentType), sort, direction, offset: (page - 1) * 25, limit: 25 }, signal), enabled: enabled && !contentId && Boolean(overviewQuery.data) && debouncedSearch === search, placeholderData: (previous, query) => query && query.queryKey[1] === project?.id && query.queryKey[4] === overviewQuery.data?.revision && JSON.stringify(query.queryKey[3]) === JSON.stringify(request) ? previous : undefined, retry: false })
  const conflictHandled = useRef(0)
  useEffect(() => {
    if (isAnalyticsRevisionConflict(listQuery.error) && conflictHandled.current !== listQuery.errorUpdatedAt) {
      conflictHandled.current = listQuery.errorUpdatedAt
      void queryClient.invalidateQueries({ queryKey: ['content-analytics', project?.id] })
    }
  }, [listQuery.error, listQuery.errorUpdatedAt, project?.id, queryClient])
  const dataQuery = contentId ? detailQuery : overviewQuery
  const selected = contentId ? detailQuery.data?.content : undefined
  const inferredAgent = selected ? analyticsAgentForContentType(selected.content_type, agentIDs) : undefined
  const selectedAgent = inferredAgent ?? (!selected ? routeAgent ?? fallbackAgent : undefined) ?? ''
  const resolvingLegacyDetail = Boolean(contentId && !routeAgent && !selected)
  const missingSelection = Boolean(contentId && !selected && !dataQuery.isPending)
  const error = projectsQuery.error || dataQuery.error || (!contentId && !isAnalyticsRevisionConflict(listQuery.error) ? listQuery.error : null)
  const contents = listQuery.data?.items ?? []
  const total = listQuery.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / 25))
  const syncRangeDays = Math.floor((Date.parse(`${period.to}T00:00:00Z`) - Date.parse(`${period.from}T00:00:00Z`)) / 86_400_000) + 1
  const syncRangeTooLong = !rangeError && (!Number.isFinite(syncRangeDays) || syncRangeDays > 31)
  const syncUnavailableMessage = accountConnection?.authorization_record_exists
    ? !accountConnection.connected ? '公众号授权已失效，请在项目设置中重新绑定。'
      : !accountConnection.can_read_account_analytics ? '当前授权未包含公众号用户统计权限。'
        : syncRangeTooLong ? '单次同步最多覆盖 31 天，请缩短所选时间范围。' : undefined
    : undefined
  const tableMetrics = metrics.filter(m => m.summary && serverSortableMetrics.has(m.key))
  const rows = contents
  function update(values: Record<string, string | undefined>, replace = true) { setParams(previous => { const next = new URLSearchParams(previous); if (project) next.set('account', project.id); for (const [key, value] of Object.entries(period)) { if (!next.get(key)) next.set(key, value) } for (const [key, value] of Object.entries(values)) { if (value) next.set(key, value); else next.delete(key) } return next }, { replace }) }
  useEffect(() => {
    if (!project) return
    try { localStorage.setItem(rememberKey(user?.id), project.id) } catch { /* Storage is optional. */ }
    if (!agentIDs.length) {
      if (requestedAccount !== project.id || params.has('agent') || params.has('platform') || Object.keys(period).some(key => !params.get(key))) {
        setParams(previous => {
          const next = new URLSearchParams(previous)
          next.set('account', project.id)
          next.delete('agent')
          next.delete('platform')
          if (requestedAccount && requestedAccount !== project.id) next.delete('content')
          for (const [key, value] of Object.entries(period)) { if (!next.get(key)) next.set(key, value) }
          return next
        }, { replace: true })
      }
      return
    }
    const agentMismatch = !resolvingLegacyDetail && (params.get('agent') || undefined) !== (selectedAgent || undefined)
    const platformMismatch = Boolean(selectedAgent) && platformParam !== analyticsPlatformForAgent(selectedAgent)
    if (requestedAccount !== project.id || agentMismatch || platformMismatch || Object.keys(period).some(key => !params.get(key))) {
      setParams(previous => {
        const next = new URLSearchParams(previous)
        next.set('account', project.id)
        if (!resolvingLegacyDetail && selectedAgent) next.set('agent', selectedAgent)
        else if (!resolvingLegacyDetail) next.delete('agent')
        if (selectedAgent) next.set('platform', analyticsPlatformForAgent(selectedAgent))
        if (requestedAccount && requestedAccount !== project.id) next.delete('content')
        for (const [key, value] of Object.entries(period)) { if (!next.get(key)) next.set(key, value) }
        return next
      }, { replace: true })
    }
  }, [project?.id, user?.id, requestedAccount, requestedAgent, platformParam, selectedAgent, resolvingLegacyDetail, agentIDs.length, params, period, setParams])
  useEffect(() => {
    if (contentId && !dataQuery.isPending) { heading.current?.focus({ preventScroll: true }); heading.current?.scrollIntoView?.({ block: 'start' }) }
    if (!contentId && (restoreScroll.current || previousContent.current) && !dataQuery.isPending) { document.getElementById('main-content')?.scrollTo?.({ top: tableScroll.current }); restoreScroll.current = false }
    previousContent.current = contentId
  }, [contentId, selected?.id, dataQuery.isPending])
  function setPeriod(next: AnalyticsPeriod) { update({ from: next.from, to: next.to, preset: next.preset, granularity: next.granularity, page: undefined }, false) }
  function selectContent(id: string) { if (!contentId) tableScroll.current = document.getElementById('main-content')?.scrollTop || window.scrollY; update({ content: id }, false) }
  function backToTable() { restoreScroll.current = true; update({ content: undefined }, false) }
  function changeAccount(id: string) { update({ account: id, agent: undefined, platform: undefined, content: undefined, metric: undefined, account_metric: undefined, account_source_metric: undefined, account_source: undefined, basis: undefined, q: undefined, type: undefined, sort: undefined, direction: undefined, page: undefined }, false); setNotice(''); setImportedDate('') }
  function changeAgent(agentID: string) {
    if (agentID === selectedAgent) return
    update({ agent: agentID, platform: analyticsPlatformForAgent(agentID), content: undefined, metric: undefined, view: undefined, type: undefined, sort: undefined, direction: undefined, page: undefined }, false)
  }
  function changeSeednoteView(next: SeednoteAnalyticsViewKey) { update({ view: next, metric: undefined, page: undefined }) }
  async function refresh() {
    if (!project) return
    // Imports advance the shared analytics revision. Stop the old list
    // request before refreshing the overview so it cannot refetch with a
    // stale expected_revision while React switches to the new revision key.
    await queryClient.cancelQueries({ queryKey: ['content-analytics', project.id] })
    await queryClient.refetchQueries({ queryKey: ['content-analytics', project.id, contentId ? 'detail' : 'overview'] })
    return queryClient.invalidateQueries({ queryKey: ['wechat-account-analytics', project.id] })
  }
  function sortBy(key: string) { update({ sort: key, direction: sort === key && direction === 'desc' ? 'asc' : 'desc', page: undefined }) }
  function openContentImport(initialUpload?: InitialContentImportUpload) {
    setAccountImportOpen(false)
    setInitialContentImportUpload(initialUpload)
    setImportOpen(true)
  }

  return <div className="space-y-5">
    <PageHeader title="内容分析" description="结合可用的内容表现数据复盘效果，为后续选题和创作提供依据。"><div className="flex gap-1"><Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-md" disabled={!project || !agentIDs.length} aria-label={accountAnalyticsEnabled && !contentId ? '查看账号导入记录' : '查看导入记录'} onClick={() => accountAnalyticsEnabled && !contentId ? setAccountHistoryOpen(true) : setHistoryOpen(true)} />}><History /></TooltipTrigger><TooltipContent>{accountAnalyticsEnabled && !contentId ? '查看账号导入记录' : '查看导入记录'}</TooltipContent></Tooltip>{accountAnalyticsEnabled && !contentId ? <><Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-md" disabled={!project || !agentIDs.length} aria-label="导入公众号内容累计数据" onClick={() => openContentImport()} />}><Upload /></TooltipTrigger><TooltipContent>导入公众号内容累计数据</TooltipContent></Tooltip><Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-md" disabled={!project || !agentIDs.length} aria-label="导入公众号账号统计" onClick={() => setAccountImportOpen(true)} />}><Activity /></TooltipTrigger><TooltipContent>导入公众号账号统计</TooltipContent></Tooltip></> : <Tooltip><TooltipTrigger render={<Button variant="ghost" size="icon-md" disabled={!project || !agentIDs.length} aria-label="导入分析数据" onClick={() => setImportOpen(true)} />}><Upload /></TooltipTrigger><TooltipContent>导入分析数据</TooltipContent></Tooltip>}</div></PageHeader>
    <div className="flex flex-wrap items-center justify-between gap-3"><ProjectContextControl mode="select" projects={projects} value={project?.id ?? null} onValueChange={id => { if (id) changeAccount(id) }} loading={projectsQuery.isPending} disabled={!projectsQuery.isPending && !projects.length} compact ariaLabel="筛选人设" placeholder="选择人设" />{project && agentIDs.length > 0 && <AnalyticsAgentSwitcher agentIDs={agentIDs} selectedAgent={selectedAgent} onSelect={changeAgent} />}</div>
    {projectsQuery.isPending && <p role="status" className="flex items-center gap-2 py-16 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在读取账号…</p>}
    {error && <Alert variant="destructive"><AlertCircle /><AlertTitle>数据加载失败</AlertTitle><AlertDescription>{error.message}<Button variant="outline" size="sm" onClick={() => { void projectsQuery.refetch(); void refresh() }}>重试</Button></AlertDescription></Alert>}
    {!projectsQuery.isPending && !projects.length && !error && <div className="rounded-xl border bg-card py-20 text-center text-sm text-muted-foreground"><p>还没有人设，请先新建一个，再选择分析平台。</p><Link to="/projects" className="mt-4 inline-block font-medium text-primary underline underline-offset-4">前往新建</Link></div>}
    {project && !agentIDs.length && <div role="status" className="rounded-lg border bg-card px-4 py-6 text-sm text-muted-foreground">该人设没有可用的内容分析 Agent。</div>}
    {project && agentIDs.length > 0 && <>
      {contentId && <section aria-label="单篇数据" className="space-y-3 border-b pb-5"><div className="flex items-center justify-between gap-3"><Button variant="ghost" size="sm" className="-ml-2" onClick={backToTable}><ArrowLeft />返回全部内容</Button><ContentSwitcher projectId={project.id} request={request} current={selected?.id ?? contentId} onSelect={selectContent} /></div><p className="text-xs text-muted-foreground">单篇数据分析 · {selected ? contentTypeLabel(selected.content_type) : '内容'}</p><h2 ref={heading} tabIndex={-1} className="scroll-mt-8 break-words text-xl font-semibold leading-relaxed outline-none sm:text-2xl">{selected?.title || (dataQuery.isPending ? '正在读取内容…' : '该内容暂不可用')}</h2>{selected && <ContentTags tags={selected.tags} />}{selected && <ContentSourceLink url={selected.url} />}</section>}
      <div className="border-b pb-4"><AnalyticsPeriodControls className="min-w-0" value={period} onChange={setPeriod} /></div>
      {notice && <div role="status" className="flex items-center justify-between gap-3 rounded-lg border border-primary/20 bg-primary/5 px-4 py-3 text-sm"><span>{notice}</span>{importedDate && (importedDate < period.from || importedDate > period.to) && <Button size="sm" variant="outline" onClick={() => setPeriod({ ...period, preset: 'custom', from: importedDate, to: importedDate })}>查看本次数据</Button>}<Tooltip><TooltipTrigger render={<Button size="icon-md" variant="ghost" aria-label="关闭提示" onClick={() => setNotice('')} />}><X /></TooltipTrigger><TooltipContent>关闭提示</TooltipContent></Tooltip></div>}
      {accountAnalyticsEnabled && !contentId && <div className="space-y-4">
        <WechatAccountOverviewPanel overview={accountOverviewQuery.data} loading={accountOverviewQuery.isPending} error={accountOverviewQuery.error instanceof Error ? accountOverviewQuery.error : undefined} selectedMetric={accountMetric} granularity={period.granularity} onMetricChange={value => update({ account_metric: value })} onSync={canSyncAccountAnalytics ? () => accountSyncMutation.mutate({ from: period.from, to: period.to }) : undefined} syncing={accountSyncMutation.isPending} syncDisabled={Boolean(rangeError || syncRangeTooLong)} syncDisabledMessage={syncUnavailableMessage} syncError={accountSyncMutation.error instanceof Error ? accountSyncMutation.error : undefined} />
        <WechatContentCumulativeOverview overview={overviewQuery.data} loading={overviewQuery.isPending} error={overviewQuery.error instanceof Error ? overviewQuery.error : undefined} metrics={metrics} selectedMetric={metric} onMetricChange={value => update({ metric: value })} />
        <div>
          <button type="button" aria-expanded={trafficSourcesOpen} aria-controls="wechat-traffic-source-panel" onClick={() => setTrafficSourcesOpen(open => !open)} className="flex min-h-10 w-full items-center justify-between border-y py-2 text-left text-sm font-medium hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">
            <span>流量来源</span><ChevronDown aria-hidden="true" className={`size-4 transition-transform ${trafficSourcesOpen ? 'rotate-180' : ''}`} />
          </button>
          {trafficSourcesOpen && <div id="wechat-traffic-source-panel" className="pt-3"><WechatTrafficSources projectId={project.id} from={period.from} to={period.to} metric={accountSourceMetric} source={accountSource} onMetricChange={value => update({ account_source_metric: value })} onSourceChange={value => update({ account_source: value })} /></div>}
        </div>
      </div>}
      {analyticsPlatform === 'seednote' && !contentId ? <SeednoteOfficialAnalytics view={seednoteView} views={seednoteAnalyticsViews} overview={error || rangeError || missingSelection ? undefined : dataQuery.data as AnalyticsOverview | undefined} data={error || rangeError || missingSelection ? [] : dataQuery.data?.series ?? []} metrics={viewMetrics} selectedMetric={metric} onViewChange={changeSeednoteView} onMetricChange={value => update({ metric: value })} loading={dataQuery.isPending && !rangeError} emptyMessage={rangeError || (error ? '数据读取失败，请重试。' : missingSelection ? '未找到这篇内容，请切换内容或返回全部内容。' : undefined)} /> : (contentId || !accountAnalyticsEnabled) ? <><div className="grid grid-cols-2 overflow-hidden rounded-xl border bg-card sm:grid-cols-3 xl:flex" aria-label="关键指标">{metrics.filter(item => item.summary).map(item => <button key={item.key} className={`relative min-w-0 flex-1 border-b border-r px-4 py-4 text-left transition-colors hover:bg-muted/50 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-primary xl:border-b-0 last:border-r-0 ${metric === item.key ? 'bg-primary/[0.045]' : ''}`} aria-pressed={metric === item.key} onClick={() => update({ metric: item.key })}>{metric === item.key && <span className="absolute inset-x-4 bottom-0 h-0.5 rounded-full bg-primary" />}<span className="text-xs text-muted-foreground">{item.label}</span><span className="mt-2 block text-2xl font-semibold tracking-tight tabular-nums">{dataQuery.isPending || error || rangeError || missingSelection || dataQuery.data?.totals[item.key] == null ? '—' : item.format(dataQuery.data.totals[item.key]!)}</span></button>)}</div><AnalyticsTrend title={contentId ? '内容趋势' : '整体趋势'} metrics={metrics} data={error || rangeError || missingSelection ? [] : dataQuery.data?.series ?? []} selectedMetric={metric} onMetricChange={value => update({ metric: value })} loading={dataQuery.isPending && !rangeError} description={descriptionFor()} emptyMessage={rangeError || (error ? '数据读取失败，请重试。' : missingSelection ? '未找到这篇内容，请切换内容或返回全部内容。' : undefined)} /></> : null}
      {contentId && selected && <Observations key={`${project.id}:${selected.id}:${period.from}:${period.to}`} projectId={project.id} contentId={selected.id} request={request} />}
      {contentTarget && contentLineageQuery.data?.items.map(lineage => <ContentLineagePanel key={lineage.asset.id} assetID={lineage.asset.id} />)}
      {Object.entries(dataQuery.data?.unavailable_metrics ?? {}).length > 0 && <p className="text-xs text-muted-foreground">{Object.entries(dataQuery.data!.unavailable_metrics).map(([key, reason]) => `${metricLabel(key)}：${reason}`).join('；')}</p>}
      {!contentId && <section aria-label="内容表现" className="overflow-hidden rounded-xl border bg-card"><div className="flex flex-wrap items-center justify-between gap-3 border-b p-4"><div><h2 className="font-semibold">内容表现 <span className="ml-2 text-sm font-normal text-muted-foreground">{total} 篇</span></h2><p className="mt-1 text-xs text-muted-foreground">点击标题查看单篇趋势 · 每篇内容按累计值展示 · 缺失数据以 — 显示</p></div><div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">{analyticsPlatform === 'wechat' && <><Button variant="outline" size="sm" onClick={() => setImportOpen(true)}><Upload />导入内容累计数据</Button><Button variant="outline" size="sm" onClick={() => setHistoryOpen(true)}><History />导入历史</Button></>}<div className="relative min-w-40 flex-1 sm:w-56"><Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" /><Input className="pl-9" aria-label="搜索内容" placeholder="搜索内容标题" value={search} onChange={e => update({ q: e.target.value, page: undefined })} /></div>{!agentContentType && <select aria-label="内容类型" className="h-9 rounded-md border border-transparent bg-transparent px-2 text-sm outline-none transition-colors hover:bg-muted/70 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50" value={contentType} onChange={e => update({ type: e.target.value, page: undefined })}><option value="all">全部类型</option>{contentTypesFor(analyticsPlatform).map(type => <option value={type} key={type}>{contentTypeLabel(type)}</option>)}</select>}<select aria-label="排序指标" className="h-9 max-w-36 rounded-md border border-transparent bg-transparent px-2 text-sm outline-none transition-colors hover:bg-muted/70 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50" value={sort} onChange={e => update({ sort: e.target.value, page: undefined })}><option value="date">数据日期</option><option value="title">内容标题</option>{tableMetrics.map(item => <option key={item.key} value={item.key}>{item.label}</option>)}</select><Tooltip><TooltipTrigger render={<Button size="icon-md" variant="ghost" aria-label={direction === 'desc' ? '切换升序' : '切换降序'} onClick={() => update({ direction: direction === 'desc' ? 'asc' : 'desc', page: undefined })} />}><ArrowUpDown /></TooltipTrigger><TooltipContent>{direction === 'desc' ? '切换升序' : '切换降序'}</TooltipContent></Tooltip></div></div>
      <div className="overflow-x-auto"><Table className="min-w-[820px]"><TableHeader><TableRow><TableHead className="w-[36%] pl-5"><button className="py-2 hover:text-foreground" onClick={() => sortBy('title')}>内容标题</button></TableHead><TableHead>类型</TableHead>{tableMetrics.map(item => <TableHead key={item.key} className="text-right" aria-sort={sort === item.key ? direction === 'asc' ? 'ascending' : 'descending' : 'none'}><button className="inline-flex items-center gap-1 whitespace-nowrap py-2 hover:text-foreground" onClick={() => sortBy(item.key)}>{item.label}{sort === item.key && <span>{direction === 'desc' ? '↓' : '↑'}</span>}</button></TableHead>)}<TableHead className="pr-5 text-right"><button className="whitespace-nowrap py-2" onClick={() => sortBy('date')}>数据截至</button></TableHead></TableRow></TableHeader><TableBody>{!error && !rangeError && rows.map(item => <Fragment key={item.id}><TableRow className="border-b-0"><TableCell className="max-w-96 py-4 pl-5"><button className="line-clamp-2 text-left font-medium leading-relaxed hover:text-primary focus-visible:rounded focus-visible:outline-2 focus-visible:outline-primary" title={item.title} onClick={() => selectContent(item.id)}>{item.title}</button></TableCell><TableCell><span className="whitespace-nowrap rounded bg-muted px-2 py-1 text-xs text-muted-foreground">{contentTypeLabel(item.content_type)}</span></TableCell>{tableMetrics.map(m => <TableCell key={m.key} className="text-right tabular-nums">{item.metrics[m.key] == null ? '—' : m.format(item.metrics[m.key]!)}</TableCell>)}<TableCell className="whitespace-nowrap pr-5 text-right text-xs text-muted-foreground">{item.last_stat_date ? shanghaiDay(item.last_stat_date) : '—'}</TableCell></TableRow>{item.tags?.length ? <TableRow data-testid={`content-tags-row-${item.id}`} aria-label={`${item.title}的内容标签`}><TableCell colSpan={tableMetrics.length + 3} className="px-5 pb-4 pt-0"><ContentTags tags={item.tags} /></TableCell></TableRow> : null}</Fragment>)}</TableBody></Table></div>
      {listQuery.isPending ? <p role="status" className="py-14 text-center text-sm text-muted-foreground">正在读取内容…</p> : !rows.length && !error && <p className="py-14 text-center text-sm text-muted-foreground">{search || contentType !== 'all' ? '没有找到符合条件的内容' : '暂无分析数据，导入平台数据后即可查看。'}</p>}
      <div className="flex items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground"><span>每页 25 篇 · 共 {total} 篇</span><div className="flex items-center gap-2"><Button variant="outline" size="icon-md" aria-label="上一页" disabled={page <= 1 || listQuery.isFetching} onClick={() => update({ page: String(page - 1) })}><ChevronLeft /></Button><span className="min-w-12 text-center tabular-nums">{page} / {pageCount}</span><Button variant="outline" size="icon-md" aria-label="下一页" disabled={page >= pageCount || listQuery.isFetching} onClick={() => update({ page: String(page + 1) })}><ChevronRight /></Button></div></div></section>}
    </>}
    {project && importOpen && <ContentImportDialog key={`${project.id}:${analyticsPlatform}:${initialContentImportUpload?.uploadId ?? 'new'}`} project={project} platform={analyticsPlatform} initialUpload={initialContentImportUpload} onClose={() => { setImportOpen(false); setInitialContentImportUpload(undefined) }} onImported={({ count, date }) => { setImportOpen(false); setInitialContentImportUpload(undefined); setNotice(`已导入 ${count} 条分析数据。`); setImportedDate(date); void refresh() }} />}
    {project && historyOpen && <ContentImportHistory key={`${project.id}:${analyticsPlatform}`} project={project} platform={analyticsPlatform} onClose={() => setHistoryOpen(false)} onRevoked={() => { setNotice('已撤销本次导入，数据已重新计算。'); setImportedDate(''); void refresh() }} />}
    {project && accountImportOpen && <WechatAccountImportDialog project={project} onClose={() => setAccountImportOpen(false)} onContentImportRequested={openContentImport} onImported={({ count, date, dateFrom, dateTo }) => { setAccountImportOpen(false); setNotice(`已导入 ${count} 天账号统计。`); setImportedDate(date); const from = dateFrom || date; const to = dateTo || date; if (from && to && (to < period.from || from > period.to)) setPeriod({ ...period, preset: 'custom', from, to }); void refresh() }} />}
    {project && accountHistoryOpen && <WechatAccountImportHistory project={project} onClose={() => setAccountHistoryOpen(false)} onRevoked={() => { setNotice('已撤销账号统计导入，数据已重新计算。'); setImportedDate(''); void refresh() }} />}
  </div>
}
