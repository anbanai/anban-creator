import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { AlertCircle, ArrowLeft, ArrowUpDown, Check, ChevronDown, ChevronLeft, ChevronRight, History, Loader2, Search, Upload, X } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { defaultAnalyticsPeriod, periodError, shanghaiDay, type AnalyticsPeriod } from '@/lib/analytics-period'
import { descriptionFor, loadContentAnalytics, metricsFor, type AnalyticsContent, type AnalyticsPlatform } from '@/lib/content-analytics'
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
  return <Popover open={open} onOpenChange={setOpen}><PopoverTrigger render={<Button variant="outline" className="h-auto min-w-60 max-w-full justify-start gap-3 bg-card px-3 py-2" aria-label={`选择账号 ${selected?.name || ''}`} />}>
    {selected && <AccountAvatar project={selected} />}<span className="min-w-0 flex-1 text-left"><span className="block truncate font-medium">{selected?.name || '选择账号'}</span><span className="block text-xs font-normal text-muted-foreground">{selected ? platformName(selected.platform) : '公众号 / 种草笔记'}</span></span><ChevronDown className="size-4 text-muted-foreground" />
  </PopoverTrigger><PopoverContent align="start" className="w-[min(360px,calc(100vw-32px))] p-2"><Input autoFocus aria-label="搜索账号" placeholder="搜索账号名称或平台" value={search} onChange={e => setSearch(e.target.value)} /><div className="max-h-80 overflow-y-auto">
    {(['article', 'seednote'] as const).map(platform => { const group = matches.filter(p => p.platform === platform); return group.length > 0 && <div key={platform} role="group" aria-label={platformName(platform)}><p className="px-2 pb-1 pt-3 text-xs text-muted-foreground">{platformName(platform)}</p>{group.map(project => <button key={project.id} className="flex w-full items-center gap-3 rounded-md p-2 text-left hover:bg-muted focus-visible:outline-2 focus-visible:outline-primary" aria-label={`${project.name} ${platformName(platform)}`} onClick={() => { onSelect(project.id); setOpen(false); setSearch('') }}><AccountAvatar project={project} /><span className="min-w-0 flex-1 truncate">{project.name}</span>{selected?.id === project.id && <Check className="size-4 text-primary" />}</button>)}</div> })}
    {!matches.length && <p className="py-8 text-center text-sm text-muted-foreground">没有找到匹配的账号</p>}
  </div></PopoverContent></Popover>
}
function ContentSwitcher({ contents, current, onSelect }: { contents: AnalyticsContent[]; current: string; onSelect: (id: string) => void }) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const matching = contents.filter(item => item.title.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
  return <Popover open={open} onOpenChange={setOpen}><PopoverTrigger render={<Button size="sm" variant="outline" />} >切换内容<ChevronDown /></PopoverTrigger><PopoverContent align="end" className="w-[min(440px,calc(100vw-32px))]"><Input autoFocus aria-label="搜索要切换的内容" placeholder="搜索内容标题" value={search} onChange={e => setSearch(e.target.value)} /><div className="max-h-80 overflow-y-auto">{matching.map(item => <button key={item.id} className="flex w-full items-start justify-between gap-3 rounded-md p-2.5 text-left text-sm hover:bg-muted focus-visible:outline-2 focus-visible:outline-primary" onClick={() => { onSelect(item.id); setOpen(false); setSearch('') }}><span className="whitespace-normal break-words">{item.title}</span>{current === item.id && <Check className="size-4 shrink-0 text-primary" />}</button>)}{!matching.length && <p className="py-6 text-center text-muted-foreground">没有找到匹配的内容</p>}</div></PopoverContent></Popover>
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
  const dataQuery = useQuery({ queryKey: ['content-analytics', project?.id, period, contentId], queryFn: () => loadContentAnalytics(project!, period, contentId), enabled: Boolean(project && !rangeError) })
  const contents = dataQuery.data?.contents ?? []
  const selected = dataQuery.data?.selected
  const missingSelection = Boolean(contentId && !selected && !dataQuery.isPending)
  const error = projectsQuery.error || dataQuery.error
  const search = params.get('q') || ''
  const contentType = params.get('type') || 'all'
  const sort = params.get('sort') || metric
  const direction = params.get('direction') === 'asc' ? 'asc' : 'desc'
  const requestedPage = Math.max(1, Number.parseInt(params.get('page') || '1', 10) || 1)
  const tableMetrics = metrics.filter(m => m.summary)
  const visibleContents = useMemo(() => contents.filter(item => item.title.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()) && (contentType === 'all' || item.contentType === contentType)).sort((a, b) => {
    let comparison: number
    if (sort === 'title' || sort === 'date') comparison = (a[sort] || '').localeCompare(b[sort] || '', 'zh-CN')
    else { const av = a.metrics[sort]; const bv = b.metrics[sort]; if (av == null) return bv == null ? 0 : 1; if (bv == null) return -1; comparison = av - bv }
    return direction === 'asc' ? comparison : -comparison
  }), [contents, search, contentType, sort, direction])
  const pageCount = Math.max(1, Math.ceil(visibleContents.length / 25))
  const page = Math.min(requestedPage, pageCount)
  const rows = visibleContents.slice((page - 1) * 25, page * 25)
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
  function changeAccount(id: string) { update({ account: id, content: undefined, metric: undefined, q: undefined, type: undefined, sort: undefined, direction: undefined, page: undefined }, false); setNotice(''); setImportedDate('') }
  function refresh() { return queryClient.invalidateQueries({ queryKey: ['content-analytics', project?.id] }) }
  function sortBy(key: string) { update({ sort: key, direction: sort === key && direction === 'desc' ? 'asc' : 'desc', page: undefined }) }
  const dates = dataQuery.data?.availableDates ?? []
  const emptyRange = !Object.values(dataQuery.data?.totals ?? {}).some(value => value != null)

  return <div className="space-y-5">
    <PageHeader title="内容数据" description="把每一篇内容的表现，变成下一次创作的线索。"><div className="flex gap-2"><Button variant="outline" disabled={!project} onClick={() => setHistoryOpen(true)}><History />导入记录</Button><Button disabled={!project} onClick={() => setImportOpen(true)}><Upload />导入数据</Button></div></PageHeader>
    <div className="flex flex-wrap items-center justify-between gap-3"><AccountPicker projects={projects} selected={project} onSelect={changeAccount} /><p className="text-xs text-muted-foreground">{dates.length ? `最近数据 ${dates[dates.length - 1]} · ${contents.length} 篇内容` : '导入平台后台数据，持续观察内容表现'}</p></div>
    {projectsQuery.isPending && <p role="status" className="flex items-center gap-2 py-16 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在读取账号…</p>}
    {error && <Alert variant="destructive"><AlertCircle /><AlertTitle>数据加载失败</AlertTitle><AlertDescription>{error.message}<Button variant="outline" size="sm" onClick={() => { void projectsQuery.refetch(); void dataQuery.refetch() }}>重试</Button></AlertDescription></Alert>}
    {!projectsQuery.isPending && !projects.length && !error && <div className="rounded-xl border bg-card py-20 text-center text-sm text-muted-foreground"><p>还没有可查看数据的账号，请先创建公众号或种草笔记项目。</p><Link to="/projects" className="mt-4 inline-block font-medium text-primary underline underline-offset-4">前往创建项目</Link></div>}
    {project && <>
      {contentId && <section aria-label="单篇数据" className="space-y-3 border-b pb-5"><div className="flex items-center justify-between gap-3"><Button variant="ghost" size="sm" className="-ml-2" onClick={backToTable}><ArrowLeft />返回全部内容</Button><ContentSwitcher contents={contents} current={contentId} onSelect={selectContent} /></div><p className="text-xs text-muted-foreground">单篇数据分析 · {selected ? typeName(selected.contentType) : '内容'}</p><h2 ref={heading} tabIndex={-1} className="scroll-mt-8 break-words text-xl font-semibold leading-relaxed outline-none sm:text-2xl">{selected?.title || (dataQuery.isPending ? '正在读取内容…' : '该内容暂不可用')}</h2>{selected && <ContentSourceLink url={selected.url} />}</section>}
      <AnalyticsPeriodControls value={period} onChange={setPeriod} />
      {notice && <div role="status" className="flex items-center justify-between gap-3 rounded-lg border border-primary/20 bg-primary/5 px-4 py-3 text-sm"><span>{notice}</span>{importedDate && (importedDate < period.from || importedDate > period.to) && <Button size="sm" variant="outline" onClick={() => setPeriod({ ...period, preset: 'custom', from: importedDate, to: importedDate })}>查看本次数据</Button>}<Button size="icon-sm" variant="ghost" aria-label="关闭提示" onClick={() => setNotice('')}><X /></Button></div>}
      <div className="grid grid-cols-2 overflow-hidden rounded-xl border bg-card sm:grid-cols-3 xl:flex" aria-label="关键指标">{metrics.filter(item => item.summary).map(item => <button key={item.key} className={`relative min-w-0 flex-1 border-b border-r px-4 py-4 text-left transition-colors hover:bg-muted/50 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-primary xl:border-b-0 last:border-r-0 ${metric === item.key ? 'bg-primary/[0.045]' : ''}`} aria-pressed={metric === item.key} onClick={() => update({ metric: item.key })}>{metric === item.key && <span className="absolute inset-x-4 bottom-0 h-0.5 rounded-full bg-primary" />}<span className="text-xs text-muted-foreground">{item.label}</span><span className="mt-2 block text-2xl font-semibold tracking-tight tabular-nums">{dataQuery.isPending || error || rangeError || missingSelection || dataQuery.data?.totals[item.key] == null ? '—' : item.format(dataQuery.data.totals[item.key]!)}</span></button>)}</div>
      <AnalyticsTrend title={contentId ? '内容趋势' : '整体趋势'} metrics={metrics} data={error || rangeError || missingSelection ? [] : dataQuery.data?.series ?? []} selectedMetric={metric} onMetricChange={value => update({ metric: value })} loading={dataQuery.isPending && !rangeError} description={descriptionFor(project.platform as AnalyticsPlatform, Boolean(contentId))} emptyMessage={rangeError || (error ? '数据读取失败，请重试。' : missingSelection ? '未找到这篇内容，请切换内容或返回全部内容。' : undefined)} />
      {!dataQuery.isPending && !error && emptyRange && dates.length > 0 && <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3 text-sm"><span className="text-muted-foreground">当前日期范围没有内容数据</span><Button size="sm" variant="outline" onClick={() => setPeriod({ ...period, preset: 'custom', from: dates[0], to: dates[dates.length - 1] })}>查看全部数据日期</Button></div>}
      {!contentId && <section aria-label="内容表现" className="overflow-hidden rounded-xl border bg-card"><div className="flex flex-wrap items-center justify-between gap-3 border-b p-4"><div><h2 className="font-semibold">内容表现 <span className="ml-2 text-sm font-normal text-muted-foreground">{visibleContents.length} 篇</span></h2><p className="mt-1 text-xs text-muted-foreground">点击标题查看单篇趋势 · 缺失数据以 — 显示</p></div><div className="flex w-full flex-wrap items-center gap-2 sm:w-auto"><div className="relative min-w-40 flex-1 sm:w-56"><Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" /><Input className="pl-9" aria-label="搜索内容" placeholder="搜索内容标题" value={search} onChange={e => update({ q: e.target.value, page: undefined })} /></div><select aria-label="内容类型" className="h-9 rounded-md border bg-background px-2 text-sm" value={contentType} onChange={e => update({ type: e.target.value, page: undefined })}><option value="all">全部类型</option>{[...new Set(contents.map(item => item.contentType))].sort().map(type => <option value={type} key={type}>{typeName(type)}</option>)}</select><select aria-label="排序指标" className="h-9 max-w-36 rounded-md border bg-background px-2 text-sm" value={sort} onChange={e => update({ sort: e.target.value, page: undefined })}><option value="date">数据日期</option><option value="title">内容标题</option>{metrics.map(item => <option key={item.key} value={item.key}>{item.label}</option>)}</select><Button size="icon" variant="outline" aria-label={direction === 'desc' ? '切换升序' : '切换降序'} onClick={() => update({ direction: direction === 'desc' ? 'asc' : 'desc', page: undefined })}><ArrowUpDown /></Button></div></div>
      <div className="overflow-x-auto"><Table className="min-w-[820px]"><TableHeader><TableRow><TableHead className="w-[36%] pl-5"><button className="py-2 hover:text-foreground" onClick={() => sortBy('title')}>内容标题</button></TableHead><TableHead>类型</TableHead>{tableMetrics.map(item => <TableHead key={item.key} className="text-right" aria-sort={sort === item.key ? direction === 'asc' ? 'ascending' : 'descending' : 'none'}><button className="inline-flex items-center gap-1 whitespace-nowrap py-2 hover:text-foreground" onClick={() => sortBy(item.key)}>{item.label}{sort === item.key && <span>{direction === 'desc' ? '↓' : '↑'}</span>}</button></TableHead>)}<TableHead className="pr-5 text-right"><button className="whitespace-nowrap py-2" onClick={() => sortBy('date')}>数据截至</button></TableHead></TableRow></TableHeader><TableBody>{!error && !rangeError && rows.map(item => <TableRow key={item.id}><TableCell className="max-w-96 py-4 pl-5"><button className="line-clamp-2 text-left font-medium leading-relaxed hover:text-primary focus-visible:rounded focus-visible:outline-2 focus-visible:outline-primary" title={item.title} onClick={() => selectContent(item.id)}>{item.title}</button></TableCell><TableCell><span className="whitespace-nowrap rounded bg-muted px-2 py-1 text-xs text-muted-foreground">{typeName(item.contentType)}</span></TableCell>{tableMetrics.map(m => <TableCell key={m.key} className="text-right tabular-nums">{item.metrics[m.key] == null ? '—' : m.format(item.metrics[m.key]!)}</TableCell>)}<TableCell className="whitespace-nowrap pr-5 text-right text-xs text-muted-foreground">{item.date ? shanghaiDay(item.date) : '—'}</TableCell></TableRow>)}</TableBody></Table></div>
      {dataQuery.isPending ? <p role="status" className="py-14 text-center text-sm text-muted-foreground">正在读取内容…</p> : !rows.length && !error && <p className="py-14 text-center text-sm text-muted-foreground">{search || contentType !== 'all' ? '没有找到符合条件的内容' : '暂无内容数据，导入平台数据后即可查看。'}</p>}
      <div className="flex items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground"><span>每页 25 篇 · 共 {visibleContents.length} 篇</span><div className="flex items-center gap-2"><Button variant="outline" size="icon-sm" aria-label="上一页" disabled={page <= 1} onClick={() => update({ page: String(page - 1) })}><ChevronLeft /></Button><span className="min-w-12 text-center tabular-nums">{page} / {pageCount}</span><Button variant="outline" size="icon-sm" aria-label="下一页" disabled={page >= pageCount} onClick={() => update({ page: String(page + 1) })}><ChevronRight /></Button></div></div></section>}
    </>}
    {project && importOpen && <ContentImportDialog key={project.id} project={project} onClose={() => setImportOpen(false)} onImported={({ count, date }) => { setImportOpen(false); setNotice(`已导入 ${count} 条内容数据。`); setImportedDate(date); void refresh() }} />}
    {project && historyOpen && <ContentImportHistory key={project.id} project={project} onClose={() => setHistoryOpen(false)} onRevoked={() => { setNotice('已撤销本次导入，数据已重新计算。'); setImportedDate(''); void refresh() }} />}
  </div>
}
