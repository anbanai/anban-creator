import { useEffect, useMemo, useRef, useState } from 'react'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { AlertCircle, ArrowLeft, ArrowUpDown, Check, ChevronDown, ChevronLeft, ChevronRight, History, Loader2, Search, Upload, X } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { defaultAnalyticsPeriod, periodError, shanghaiDay, type AnalyticsPeriod } from '@/lib/analytics-period'
import { contentAnalyticsApi, descriptionFor, isAnalyticsRevisionConflict, metricsFor, type AnalyticsParams, type AnalyticsPlatform, type MetricBasis } from '@/lib/content-analytics'
import { useDebouncedValue } from '@/hooks/use-debounced-value'
import type { Project } from '@/types'
import AnalyticsTrend from '@/components/analytics/AnalyticsTrend'
import AnalyticsPeriodControls from '@/components/analytics/AnalyticsPeriodControls'
import ContentImportDialog from '@/components/analytics/ContentImportDialog'
import ContentImportHistory from '@/components/analytics/ContentImportHistory'
import ContentSourceLink from '@/components/common/ContentSourceLink'
import PageHeader from '@/components/layout/PageHeader'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const platformName = (platform: string) => platform === 'article' ? '公众号' : '种草笔记'
function typeName(type: string) { return ({ article: '文章', image: '贴图', image_text: '图文', image_note: '图文', video: '视频', video_note: '视频', unknown: '类型未知', note: '笔记' } as Record<string, string>)[type] || type || '类型未知' }
function rememberKey(userId?: string) { return `content-data:account:${userId || 'anonymous'}` }
function readRemembered(userId?: string) { try { return localStorage.getItem(rememberKey(userId)) || '' } catch { return '' } }
function readPeriod(params: URLSearchParams): AnalyticsPeriod {
  const defaults = defaultAnalyticsPeriod()
  const from = params.get('from') || defaults.from
  const to = params.get('to') || defaults.to
  const granularity = params.get('granularity')
  const preset = params.get('preset')
  return { from, to, preset: preset && ['7d', '30d', 'week', 'month', 'custom'].includes(preset) ? preset as AnalyticsPeriod['preset'] : params.has('from') ? 'custom' : defaults.preset, granularity: granularity === 'week' || granularity === 'month' ? granularity : 'day' }
}
function AccountAvatar({ project }: { project: Project }) {
  return <span className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-lg border bg-muted text-sm font-semibold text-muted-foreground">{project.avatar_url ? <img src={project.avatar_url} alt="" className="size-full object-cover" /> : project.name.slice(0, 1)}</span>
}
function AccountPicker({ projects, selected, onSelect }: { projects: Project[]; selected?: Project; onSelect: (id: string) => void }) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const matches = projects.filter(p => `${p.name} ${platformName(p.platform)}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  return <Popover open={open} onOpenChange={setOpen}><PopoverTrigger render={<Button variant="outline" className="h-auto min-w-60 max-w-full justify-start gap-3 px-2.5 py-2" aria-label={`选择账号 ${selected?.name || ''}`} />}>
    {selected && <AccountAvatar project={selected} />}<span className="min-w-0 flex-1 text-left"><span className="block truncate font-medium">{selected?.name || '选择账号'}</span><span className="block text-xs font-normal text-muted-foreground">{selected ? platformName(selected.platform) : '公众号 / 种草笔记'}</span></span><ChevronDown className="size-4 text-muted-foreground" />
  </PopoverTrigger><PopoverContent align="start" className="w-[min(360px,calc(100vw-32px))] p-2"><Input autoFocus aria-label="搜索账号" placeholder="搜索账号名称或平台" value={search} onChange={e => setSearch(e.target.value)} /><div className="max-h-80 overflow-y-auto">
    {(['article', 'seednote'] as const).map(platform => { const group = matches.filter(p => p.platform === platform); return group.length > 0 && <div key={platform} role="group" aria-label={platformName(platform)}><p className="px-2 pb-1 pt-3 text-xs text-muted-foreground">{platformName(platform)}</p>{group.map(project => <button key={project.id} className="flex w-full items-center gap-3 rounded-md p-2 text-left hover:bg-muted focus-visible:outline-2 focus-visible:outline-primary" aria-label={`${project.name} ${platformName(platform)}`} onClick={() => { onSelect(project.id); setOpen(false); setSearch('') }}><AccountAvatar project={project} /><span className="min-w-0 flex-1 truncate">{project.name}</span>{selected?.id === project.id && <Check className="size-4 text-primary" />}</button>)}</div> })}
    {!matches.length && <p className="py-8 text-center text-sm text-muted-foreground">没有找到匹配的账号</p>}
  </div></PopoverContent></Popover>
}
function ContentSwitcher({ projectId, request, current, onSelect }: { projectId: string; request: AnalyticsParams; current: string; onSelect: (id: string) => void }) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const debounced = useDebouncedValue(search)
  const query = useQuery({ queryKey: ['content-analytics', projectId, 'picker', request, debounced, page], queryFn: ({ signal }) => contentAnalyticsApi.contents(projectId, { ...request, search: debounced, offset: page * 25, limit: 25 }, signal), enabled: open, placeholderData: keepPreviousData })
  return <Popover open={open} onOpenChange={setOpen}><PopoverTrigger render={<Button size="sm" variant="outline" />}>切换内容<ChevronDown /></PopoverTrigger><PopoverContent align="end" className="w-[min(440px,calc(100vw-32px))]"><Input autoFocus aria-label="搜索要切换的内容" placeholder="搜索内容标题" value={search} onChange={e => { setSearch(e.target.value); setPage(0) }} /><div className="max-h-80 overflow-y-auto">{query.data?.items.map(item => <button key={item.id} className="flex w-full items-start justify-between gap-3 rounded-md p-2.5 text-left text-sm hover:bg-muted" onClick={() => { onSelect(item.id); setOpen(false) }}><span>{item.title}</span>{current === item.id && <Check className="size-4 shrink-0 text-primary" />}</button>)}{query.isError && <p role="alert">{query.error.message}</p>}</div><div className="flex justify-between border-t pt-2"><Button size="sm" variant="ghost" disabled={page === 0 || query.isFetching} onClick={() => setPage(page - 1)}>上一页</Button><span className="text-xs">共 {query.data?.total ?? 0} 篇</span><Button size="sm" variant="ghost" disabled={!query.data || (page + 1) * 25 >= query.data.total || query.isFetching} onClick={() => setPage(page + 1)}>下一页</Button></div></PopoverContent></Popover>
}
function Observations({ projectId, contentId, request }: { projectId: string; contentId: string; request: AnalyticsParams }) {
  const [open, setOpen] = useState(false)
  const [page, setPage] = useState(0)
  const query = useQuery({ queryKey: ['content-analytics', projectId, 'observations', contentId, request, page], queryFn: ({ signal }) => contentAnalyticsApi.observations(projectId, contentId, { ...request, offset: page * 25, limit: 25 }, signal), enabled: open, placeholderData: keepPreviousData })
  return <section className="rounded-lg border p-4"><Button variant="outline" onClick={() => setOpen(!open)}>{open ? '收起观测记录' : '查看观测记录'}</Button>{open && <div className="mt-3 space-y-3">{query.isError && <p role="alert">{query.error.message}</p>}{query.isPending && <p role="status">正在读取记录…</p>}{query.data?.items.map(item => <div key={item.id} className="border-b py-2 text-xs"><p>{item.stat_date} · {item.source} · {item.metric_basis === 'daily' ? '当日新增' : '累计'}{item.revoked_at ? ' · 已撤销' : ''}</p><p className="mt-1 text-muted-foreground">{Object.entries(item.metrics).map(([key, value]) => `${key}: ${value ?? '—'}`).join(' · ')}</p></div>)}<div className="flex items-center justify-between"><Button size="sm" variant="ghost" disabled={page === 0 || query.isFetching} onClick={() => setPage(page - 1)}>上一页</Button><span className="text-xs">共 {query.data?.total ?? 0} 条</span><Button size="sm" variant="ghost" disabled={!query.data || (page + 1) * 25 >= query.data.total || query.isFetching} onClick={() => setPage(page + 1)}>下一页</Button></div></div>}</section>
}

export default function ContentDataPage() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const queryClient = useQueryClient()
  const period = useMemo(() => readPeriod(params), [params])
  const [importOpen, setImportOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [notice, setNotice] = useState('')
  const [importedDate, setImportedDate] = useState('')
  const heading = useRef<HTMLHeadingElement>(null)
  const tableScroll = useRef(0)
  const restoreScroll = useRef(false)
  const previousContent = useRef<string | undefined>(undefined)
  const projectsQuery = useQuery({ queryKey: queryKeys.projects.list(), queryFn: () => api.projects.list() })
  const projects = (projectsQuery.data ?? []).filter(p => p.platform === 'article' || p.platform === 'seednote')
  const requestedAccount = params.get('account')
  const project = projects.find(p => p.id === requestedAccount) ?? projects.find(p => p.id === readRemembered(user?.id)) ?? projects[0]
  const contentId = params.get('content') || undefined
  const metrics = metricsFor((project?.platform || 'article') as AnalyticsPlatform)
  const metric = metrics.find(m => m.key === params.get('metric'))?.key || metrics.find(m => m.key === (project?.platform === 'seednote' ? 'view_count' : 'read_users'))?.key || metrics.find(m => m.summary)?.key || metrics[0].key
  const rangeError = periodError(period.from, period.to)
  const basis: MetricBasis = project?.platform === 'seednote' && params.get('basis') === 'daily' ? 'daily' : 'cumulative'
  const request = useMemo(() => ({ from: period.from, to: period.to, granularity: period.granularity, metric_basis: basis }), [period.from, period.to, period.granularity, basis])
  const search = params.get('q') || ''
  const debouncedSearch = useDebouncedValue(search)
  const contentType = params.get('type') || 'all'
  const sort = params.get('sort') || metric
  const direction = params.get('direction') === 'asc' ? 'asc' : 'desc'
  const page = Math.max(1, Number.parseInt(params.get('page') || '1', 10) || 1)
  const enabled = Boolean(project && !rangeError)
  const overviewQuery = useQuery({ queryKey: ['content-analytics', project?.id, 'overview', request], queryFn: ({ signal }) => contentAnalyticsApi.overview(project!.id, request, signal), enabled: enabled && !contentId, staleTime: 60_000 })
  const detailQuery = useQuery({ queryKey: ['content-analytics', project?.id, 'detail', contentId, request], queryFn: ({ signal }) => contentAnalyticsApi.detail(project!.id, contentId!, request, signal), enabled: enabled && Boolean(contentId), staleTime: 60_000 })
  const listQuery = useQuery({ queryKey: ['content-analytics', project?.id, 'contents', request, overviewQuery.data?.revision, debouncedSearch, contentType, sort, direction, page], queryFn: ({ signal }) => contentAnalyticsApi.contents(project!.id, { ...request, expected_revision: overviewQuery.data?.revision, search: debouncedSearch, content_type: contentType === 'all' ? undefined : contentType, sort, direction, offset: (page - 1) * 25, limit: 25 }, signal), enabled: enabled && !contentId && Boolean(overviewQuery.data) && debouncedSearch === search, placeholderData: (previous, query) => query && query.queryKey[1] === project?.id && query.queryKey[4] === overviewQuery.data?.revision && JSON.stringify(query.queryKey[3]) === JSON.stringify(request) ? previous : undefined, retry: false })
  const [datesYear, setDatesYear] = useState(Number(period.to.slice(0, 4)))
  const datesQuery = useQuery({ queryKey: ['content-analytics', project?.id, 'dates', basis, datesYear], queryFn: ({ signal }) => contentAnalyticsApi.dates(project!.id, { year: datesYear, metric_basis: basis }, signal), enabled, staleTime: 60_000 })
  useEffect(() => { setDatesYear(Number(period.to.slice(0, 4))) }, [period.to])
  const conflictHandled = useRef(0)
  useEffect(() => {
    if (isAnalyticsRevisionConflict(listQuery.error) && conflictHandled.current !== listQuery.errorUpdatedAt) {
      conflictHandled.current = listQuery.errorUpdatedAt
      void queryClient.invalidateQueries({ queryKey: ['content-analytics', project?.id] })
    }
  }, [listQuery.error, listQuery.errorUpdatedAt, project?.id, queryClient])
  const dataQuery = contentId ? detailQuery : overviewQuery
  const selected = contentId ? detailQuery.data?.content : undefined
  const missingSelection = Boolean(contentId && !selected && !dataQuery.isPending)
  const error = projectsQuery.error || dataQuery.error || (!contentId && !isAnalyticsRevisionConflict(listQuery.error) ? listQuery.error : null)
  const contents = listQuery.data?.items ?? []
  const total = listQuery.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / 25))
  const tableMetrics = metrics.filter(m => m.summary)
  const rows = contents
  function update(values: Record<string, string | undefined>, replace = true) { setParams(previous => { const next = new URLSearchParams(previous); if (project) next.set('account', project.id); for (const [key, value] of Object.entries(period)) { if (!next.get(key)) next.set(key, value) } for (const [key, value] of Object.entries(values)) { if (value) next.set(key, value); else next.delete(key) } return next }, { replace }) }
  useEffect(() => {
    if (!project) return
    try { localStorage.setItem(rememberKey(user?.id), project.id) } catch { /* Storage is optional. */ }
    if (requestedAccount !== project.id || Object.keys(period).some(key => !params.get(key))) {
      setParams(previous => {
        const next = new URLSearchParams(previous)
        next.set('account', project.id)
        if (requestedAccount && requestedAccount !== project.id) next.delete('content')
        for (const [key, value] of Object.entries(period)) { if (!next.get(key)) next.set(key, value) }
        return next
      }, { replace: true })
    }
  }, [project?.id, user?.id, requestedAccount, params, period, setParams])
  useEffect(() => {
    if (contentId && !dataQuery.isPending) { heading.current?.focus({ preventScroll: true }); heading.current?.scrollIntoView?.({ block: 'start' }) }
    if (!contentId && (restoreScroll.current || previousContent.current) && !dataQuery.isPending) { document.getElementById('main-content')?.scrollTo?.({ top: tableScroll.current }); restoreScroll.current = false }
    previousContent.current = contentId
  }, [contentId, selected?.id, dataQuery.isPending])
  function setPeriod(next: AnalyticsPeriod) { update({ from: next.from, to: next.to, preset: next.preset, granularity: next.granularity, page: undefined }, false) }
  function selectContent(id: string) { if (!contentId) tableScroll.current = document.getElementById('main-content')?.scrollTop || window.scrollY; update({ content: id }, false) }
  function backToTable() { restoreScroll.current = true; update({ content: undefined }, false) }
  function changeAccount(id: string) { update({ account: id, content: undefined, metric: undefined, basis: undefined, q: undefined, type: undefined, sort: undefined, direction: undefined, page: undefined }, false); setNotice(''); setImportedDate('') }
  function refresh() { return queryClient.invalidateQueries({ queryKey: ['content-analytics', project?.id] }) }
  function sortBy(key: string) { update({ sort: key, direction: sort === key && direction === 'desc' ? 'asc' : 'desc', page: undefined }) }
  const dates = datesQuery.data?.dates ?? []
  const emptyRange = !Object.values(dataQuery.data?.totals ?? {}).some(value => value != null)

  return <div className="space-y-5">
    <PageHeader title="内容数据" description="把每一篇内容的表现，变成下一次创作的线索。"><div className="flex gap-2"><Button variant="outline" disabled={!project} onClick={() => setHistoryOpen(true)}><History />导入记录</Button><Button disabled={!project} onClick={() => setImportOpen(true)}><Upload />导入数据</Button></div></PageHeader>
    <div className="flex flex-wrap items-center justify-between gap-3"><AccountPicker projects={projects} selected={project} onSelect={changeAccount} /><p className="text-xs text-muted-foreground">{dates.length ? `最近数据 ${dates[dates.length - 1]} · ${overviewQuery.data?.coverage.contents ?? total} 篇内容` : '导入平台后台数据，持续观察内容表现'}</p></div>
    {projectsQuery.isPending && <p role="status" className="flex items-center gap-2 py-16 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在读取账号…</p>}
    {error && <Alert variant="destructive"><AlertCircle /><AlertTitle>数据加载失败</AlertTitle><AlertDescription>{error.message}<Button variant="outline" size="sm" onClick={() => { void projectsQuery.refetch(); void refresh() }}>重试</Button></AlertDescription></Alert>}
    {!projectsQuery.isPending && !projects.length && !error && <div className="rounded-xl border bg-card py-20 text-center text-sm text-muted-foreground"><p>还没有可查看数据的账号，请先创建公众号或种草笔记项目。</p><Link to="/projects" className="mt-4 inline-block font-medium text-primary underline underline-offset-4">前往创建项目</Link></div>}
    {project && <>
      {contentId && <section aria-label="单篇数据" className="space-y-3 border-b pb-5"><div className="flex items-center justify-between gap-3"><Button variant="ghost" size="sm" className="-ml-2" onClick={backToTable}><ArrowLeft />返回全部内容</Button><ContentSwitcher projectId={project.id} request={request} current={selected?.id ?? contentId} onSelect={selectContent} /></div><p className="text-xs text-muted-foreground">单篇数据分析 · {selected ? typeName(selected.content_type) : '内容'}</p><h2 ref={heading} tabIndex={-1} className="scroll-mt-8 break-words text-xl font-semibold leading-relaxed outline-none sm:text-2xl">{selected?.title || (dataQuery.isPending ? '正在读取内容…' : '该内容暂不可用')}</h2>{selected && <ContentSourceLink url={selected.url} />}</section>}
      <div className="flex flex-wrap items-end justify-between gap-4 border-b pb-4"><AnalyticsPeriodControls className="min-w-0 flex-1" value={period} onChange={setPeriod} /><div className="flex flex-wrap items-end gap-3 text-sm"><label className="grid gap-1 text-xs text-muted-foreground">统计口径<Select value={basis} onValueChange={(value) => update({ basis: value ?? undefined, page: undefined })} disabled={project.platform === 'article'}><SelectTrigger aria-label="统计口径" className="w-32"><SelectValue>{basis === 'daily' ? '当日新增' : '累计'}</SelectValue></SelectTrigger><SelectContent><SelectItem value="cumulative">累计</SelectItem>{project.platform === 'seednote' && <SelectItem value="daily">当日新增</SelectItem>}</SelectContent></Select></label><label className="grid gap-1 text-xs text-muted-foreground">数据年份<Select value={String(datesYear)} onValueChange={(value) => setDatesYear(Number(value))}><SelectTrigger aria-label="数据年份" className="w-28"><SelectValue /></SelectTrigger><SelectContent>{Array.from({ length: 11 }, (_, i) => Number(shanghaiDay(new Date()).slice(0, 4)) - i).map(year => <SelectItem key={year} value={String(year)}>{year}</SelectItem>)}</SelectContent></Select></label>{datesQuery.isError && <span role="alert">日期读取失败</span>}</div></div>
      {notice && <div role="status" className="flex items-center justify-between gap-3 rounded-lg border border-primary/20 bg-primary/5 px-4 py-3 text-sm"><span>{notice}</span>{importedDate && (importedDate < period.from || importedDate > period.to) && <Button size="sm" variant="outline" onClick={() => setPeriod({ ...period, preset: 'custom', from: importedDate, to: importedDate })}>查看本次数据</Button>}<Button size="icon-sm" variant="ghost" aria-label="关闭提示" onClick={() => setNotice('')}><X /></Button></div>}
      <div className="grid grid-cols-2 overflow-hidden rounded-xl border bg-card sm:grid-cols-3 xl:flex" aria-label="关键指标">{metrics.filter(item => item.summary).map(item => <button key={item.key} className={`relative min-w-0 flex-1 border-b border-r px-4 py-4 text-left transition-colors hover:bg-muted/50 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-primary xl:border-b-0 last:border-r-0 ${metric === item.key ? 'bg-primary/[0.045]' : ''}`} aria-pressed={metric === item.key} onClick={() => update({ metric: item.key })}>{metric === item.key && <span className="absolute inset-x-4 bottom-0 h-0.5 rounded-full bg-primary" />}<span className="text-xs text-muted-foreground">{item.label}</span><span className="mt-2 block text-2xl font-semibold tracking-tight tabular-nums">{dataQuery.isPending || error || rangeError || missingSelection || dataQuery.data?.totals[item.key] == null ? '—' : item.format(dataQuery.data.totals[item.key]!)}</span></button>)}</div>
      <AnalyticsTrend title={contentId ? '内容趋势' : '整体趋势'} metrics={metrics} data={error || rangeError || missingSelection ? [] : dataQuery.data?.series ?? []} selectedMetric={metric} onMetricChange={value => update({ metric: value })} loading={dataQuery.isPending && !rangeError} description={descriptionFor(project.platform as AnalyticsPlatform, Boolean(contentId), basis)} emptyMessage={rangeError || (error ? '数据读取失败，请重试。' : missingSelection ? '未找到这篇内容，请切换内容或返回全部内容。' : undefined)} />
      {!dataQuery.isPending && !error && emptyRange && dates.length > 0 && <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3 text-sm"><span className="text-muted-foreground">当前日期范围没有内容数据</span><Button size="sm" variant="outline" onClick={() => setPeriod({ ...period, preset: 'custom', from: dates[0], to: dates[dates.length - 1] })}>查看该年数据日期</Button></div>}
      {contentId && selected && <Observations key={`${project.id}:${selected.id}:${basis}:${period.from}:${period.to}`} projectId={project.id} contentId={selected.id} request={request} />}
      {Object.entries(dataQuery.data?.unavailable_metrics ?? {}).length > 0 && <p className="text-xs text-muted-foreground">{Object.entries(dataQuery.data!.unavailable_metrics).map(([key, reason]) => `${metrics.find(m => m.key === key)?.label ?? key}：${reason}`).join('；')}</p>}
      {!contentId && <section aria-label="内容表现" className="overflow-hidden rounded-xl border bg-card"><div className="flex flex-wrap items-center justify-between gap-3 border-b p-4"><div><h2 className="font-semibold">内容表现 <span className="ml-2 text-sm font-normal text-muted-foreground">{total} 篇</span></h2><p className="mt-1 text-xs text-muted-foreground">点击标题查看单篇趋势 · 缺失数据以 — 显示</p></div><div className="flex w-full flex-wrap items-center gap-2 sm:w-auto"><div className="relative min-w-40 flex-1 sm:w-56"><Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" /><Input className="pl-9" aria-label="搜索内容" placeholder="搜索内容标题" value={search} onChange={e => update({ q: e.target.value, page: undefined })} /></div><select aria-label="内容类型" className="h-9 rounded-md border bg-background px-2 text-sm" value={contentType} onChange={e => update({ type: e.target.value, page: undefined })}><option value="all">全部类型</option>{(project.platform === 'article' ? ['article', 'image'] : ['image_text', 'video', 'unknown']).map(type => <option value={type} key={type}>{typeName(type)}</option>)}</select><select aria-label="排序指标" className="h-9 max-w-36 rounded-md border bg-background px-2 text-sm" value={sort} onChange={e => update({ sort: e.target.value, page: undefined })}><option value="date">数据日期</option><option value="title">内容标题</option>{metrics.map(item => <option key={item.key} value={item.key}>{item.label}</option>)}</select><Button size="icon" variant="outline" aria-label={direction === 'desc' ? '切换升序' : '切换降序'} onClick={() => update({ direction: direction === 'desc' ? 'asc' : 'desc', page: undefined })}><ArrowUpDown /></Button></div></div>
      <div className="overflow-x-auto"><Table className="min-w-[820px]"><TableHeader><TableRow><TableHead className="w-[36%] pl-5"><button className="py-2 hover:text-foreground" onClick={() => sortBy('title')}>内容标题</button></TableHead><TableHead>类型</TableHead>{tableMetrics.map(item => <TableHead key={item.key} className="text-right" aria-sort={sort === item.key ? direction === 'asc' ? 'ascending' : 'descending' : 'none'}><button className="inline-flex items-center gap-1 whitespace-nowrap py-2 hover:text-foreground" onClick={() => sortBy(item.key)}>{item.label}{sort === item.key && <span>{direction === 'desc' ? '↓' : '↑'}</span>}</button></TableHead>)}<TableHead className="pr-5 text-right"><button className="whitespace-nowrap py-2" onClick={() => sortBy('date')}>数据截至</button></TableHead></TableRow></TableHeader><TableBody>{!error && !rangeError && rows.map(item => <TableRow key={item.id}><TableCell className="max-w-96 py-4 pl-5"><button className="line-clamp-2 text-left font-medium leading-relaxed hover:text-primary focus-visible:rounded focus-visible:outline-2 focus-visible:outline-primary" title={item.title} onClick={() => selectContent(item.id)}>{item.title}</button></TableCell><TableCell><span className="whitespace-nowrap rounded bg-muted px-2 py-1 text-xs text-muted-foreground">{typeName(item.content_type)}</span></TableCell>{tableMetrics.map(m => <TableCell key={m.key} className="text-right tabular-nums">{item.metrics[m.key] == null ? '—' : m.format(item.metrics[m.key]!)}</TableCell>)}<TableCell className="whitespace-nowrap pr-5 text-right text-xs text-muted-foreground">{(item.last_stat_date || item.date) ? shanghaiDay(item.last_stat_date || item.date!) : '—'}</TableCell></TableRow>)}</TableBody></Table></div>
      {listQuery.isPending ? <p role="status" className="py-14 text-center text-sm text-muted-foreground">正在读取内容…</p> : !rows.length && !error && <p className="py-14 text-center text-sm text-muted-foreground">{search || contentType !== 'all' ? '没有找到符合条件的内容' : '暂无内容数据，导入平台数据后即可查看。'}</p>}
      <div className="flex items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground"><span>每页 25 篇 · 共 {total} 篇</span><div className="flex items-center gap-2"><Button variant="outline" size="icon-sm" aria-label="上一页" disabled={page <= 1 || listQuery.isFetching} onClick={() => update({ page: String(page - 1) })}><ChevronLeft /></Button><span className="min-w-12 text-center tabular-nums">{page} / {pageCount}</span><Button variant="outline" size="icon-sm" aria-label="下一页" disabled={page >= pageCount || listQuery.isFetching} onClick={() => update({ page: String(page + 1) })}><ChevronRight /></Button></div></div></section>}
    </>}
    {project && importOpen && <ContentImportDialog key={project.id} project={project} onClose={() => setImportOpen(false)} onImported={({ count, date }) => { setImportOpen(false); setNotice(`已导入 ${count} 条内容数据。`); setImportedDate(date); void refresh() }} />}
    {project && historyOpen && <ContentImportHistory key={project.id} project={project} onClose={() => setHistoryOpen(false)} onRevoked={() => { setNotice('已撤销本次导入，数据已重新计算。'); setImportedDate(''); void refresh() }} />}
  </div>
}
