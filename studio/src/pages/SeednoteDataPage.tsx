import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertCircle, Check, Download, RefreshCw, Upload } from 'lucide-react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import PageHeader from '@/components/layout/PageHeader'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { uploadToOSS } from '@/lib/direct-upload'
import type { Project } from '@/types'
import type { SeednoteImportRow, SeednoteMetricVersion } from '@/types/seednote-import'

const metrics = [
  ['exposure_count', '曝光', 'count'], ['view_count', '观看量', 'count'], ['cover_click_rate', '封面点击率', 'rate'],
  ['like_count', '点赞', 'count'], ['comment_count', '评论', 'count'], ['collect_count', '收藏', 'count'],
  ['follower_gain_count', '涨粉', 'count'], ['share_count', '分享', 'count'], ['avg_watch_duration', '人均观看时长', 'duration'],
  ['barrage_count', '弹幕', 'count'],
] as const
type MetricKey = typeof metrics[number][0]

function localDate(value: Date) { const offset = value.getTimezoneOffset() * 60_000; return new Date(value.getTime() - offset).toISOString().slice(0, 10) }
function nextDate(value: string) { const date = new Date(`${value}T00:00:00`); date.setDate(date.getDate() + 1); return localDate(date) }
function normalizeTitle(value: string) { return value.normalize('NFKC').trim().replace(/\s+/g, ' ') }
function samePublishTime(a?: string | null, b?: string | null) { return Boolean(a && b && new Date(a).getTime() === new Date(b).getTime()) }
function metricLabel(key: MetricKey) { return metrics.find(([value]) => value === key)?.[1] ?? key }
function metricNote(key: MetricKey) {
  const kind = metrics.find(([value]) => value === key)?.[2]
  return kind === 'rate' || kind === 'duration' ? '按当日有值帖子平均' : '按当日已确认帖子求和'
}
function formatMetric(value: number | null | undefined, key: MetricKey) {
  if (value == null) return '-'
  const kind = metrics.find(([valueKey]) => valueKey === key)?.[2]
  if (kind === 'rate') return `${(value * 100).toFixed(2)}%`
  if (kind === 'duration') return `${value.toFixed(1)} 秒`
  return value.toLocaleString('zh-CN')
}
function statusLabel(status: string) { return ({ processing: '解析中', needs_review: '待确认', completed: '已完成', failed: '失败' } as Record<string, string>)[status] ?? status }
function latestDailyVersions(versions: SeednoteMetricVersion[]) {
  const latest = new Map<string, SeednoteMetricVersion>()
  versions.forEach((version) => { const date = version.data_as_of_at.slice(0, 10); const current = latest.get(date); if (!current || new Date(version.imported_at).getTime() > new Date(current.imported_at).getTime()) latest.set(date, version) })
  return [...latest.values()].sort((a, b) => a.data_as_of_at.localeCompare(b.data_as_of_at))
}

export default function SeednoteDataPage() {
  const queryClient = useQueryClient()
  const [projectId, setProjectId] = useState('')
  const [selectedBatch, setSelectedBatch] = useState<string | null>(null)
  const [selectedPost, setSelectedPost] = useState<string | null>(null)
  const [metric, setMetric] = useState<MetricKey>('exposure_count')
  const [asOf, setAsOf] = useState(() => `${localDate(new Date())}T23:59`)
  const [from, setFrom] = useState(() => localDate(new Date(Date.now() - 29 * 86400000)))
  const [to, setTo] = useState(() => localDate(new Date()))
  const projectsQuery = useQuery({ queryKey: queryKeys.projects.list({ platform: 'seednote' }), queryFn: () => api.projects.list({ platform: 'seednote' }) })
  const projects = (projectsQuery.data ?? []) as Project[]
  const activeProjectId = projectId || projects[0]?.id || ''
  const overviewParams = { from: from || undefined, to: to ? nextDate(to) : undefined }
  const batchesQuery = useQuery({ queryKey: queryKeys.seednoteImport.batches(activeProjectId), queryFn: () => api.seednoteImport.listBatches(activeProjectId), enabled: Boolean(activeProjectId) })
  const overviewQuery = useQuery({ queryKey: queryKeys.seednoteImport.overview(activeProjectId, overviewParams), queryFn: () => api.seednoteImport.overview(activeProjectId, overviewParams), enabled: Boolean(activeProjectId) })
  const batchQuery = useQuery({ queryKey: queryKeys.seednoteImport.batch(activeProjectId, selectedBatch || ''), queryFn: () => api.seednoteImport.getBatch(activeProjectId, selectedBatch!), enabled: Boolean(activeProjectId && selectedBatch) })
  const postsQuery = useQuery({ queryKey: queryKeys.seednoteImport.posts(activeProjectId), queryFn: () => api.seednoteImport.posts(activeProjectId), enabled: Boolean(activeProjectId) })
  const postQuery = useQuery({ queryKey: queryKeys.seednoteImport.post(activeProjectId, selectedPost || '', overviewParams), queryFn: () => api.seednoteImport.post(activeProjectId, selectedPost!, overviewParams), enabled: Boolean(activeProjectId && selectedPost) })
  const importMutation = useMutation({
    mutationFn: async (file: File) => { if (!file.name.toLowerCase().endsWith('.xlsx')) throw new Error('请选择 .xlsx 文件'); const uploaded = await uploadToOSS({ purpose: 'seednote_analytics_import', file }); return api.seednoteImport.import(activeProjectId, { upload_id: uploaded.uploadSessionId, data_as_of_at: new Date(asOf).toISOString(), timezone: 'Asia/Shanghai', client_file_modified_at: new Date(file.lastModified).toISOString() }) },
    onSuccess: (result) => { setSelectedBatch(result.batch.id); queryClient.invalidateQueries({ queryKey: queryKeys.seednoteImport.batches(activeProjectId) }); queryClient.invalidateQueries({ queryKey: queryKeys.seednoteImport.overview(activeProjectId) }) },
  })
  const resolveMutation = useMutation({ mutationFn: (actions: Array<{ row_id: string; action: string; post_id?: string }>) => api.seednoteImport.resolve(activeProjectId, selectedBatch!, actions), onSuccess: () => { queryClient.invalidateQueries({ queryKey: queryKeys.seednoteImport.batch(activeProjectId, selectedBatch || '') }); queryClient.invalidateQueries({ queryKey: queryKeys.seednoteImport.overview(activeProjectId) }); queryClient.invalidateQueries({ queryKey: queryKeys.seednoteImport.posts(activeProjectId) }) } })
  const series = overviewQuery.data?.series ?? []
  const posts = postsQuery.data?.items ?? []
  const pendingRows = (batchQuery.data?.rows ?? []).filter((row) => row.match_status === 'needs_review')
  const chartData = useMemo(() => series.map((item) => ({ ...item, label: item.date.slice(5) })), [series])
  const postVersions = useMemo(() => latestDailyVersions(postQuery.data?.versions ?? []), [postQuery.data?.versions])
  async function downloadBatch(batchId: string) { const result = await api.seednoteImport.file(activeProjectId, batchId); window.open(result.url, '_blank', 'noopener,noreferrer') }
  function candidatesFor(row: SeednoteImportRow) { return posts.filter((post) => normalizeTitle(post.title) === normalizeTitle(row.title) && samePublishTime(post.first_published_at, row.first_published_at)) }

  return <div className="space-y-6">
    <PageHeader title="小红书数据" description="导入官方 XLSX，确认帖子后按天查看账号和单帖表现。" />
    <Card><CardContent className="flex flex-col gap-4 pt-6 lg:flex-row lg:items-end lg:justify-between"><label className="grid gap-2 text-sm"><span className="text-muted-foreground">项目</span><select className="h-9 min-w-64 rounded-md border border-input bg-background px-3" value={activeProjectId} onChange={(e) => { setProjectId(e.target.value); setSelectedBatch(null); setSelectedPost(null) }}><option value="">请选择小红书项目</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label><label className="grid gap-2 text-sm"><span className="text-muted-foreground">数据截至时间</span><Input type="datetime-local" value={asOf} onChange={(e) => setAsOf(e.target.value)} /></label><label className="inline-flex cursor-pointer items-center gap-2 rounded-md border border-input px-3 py-2 text-sm hover:bg-accent"><Upload className="h-4 w-4" />{importMutation.isPending ? '导入中…' : '导入 XLSX'}<input className="sr-only" type="file" accept=".xlsx" disabled={!activeProjectId || importMutation.isPending} onChange={(e) => { const file = e.target.files?.[0]; if (file) importMutation.mutate(file); e.currentTarget.value = '' }} /></label></CardContent><CardContent className="flex flex-wrap items-center gap-3 border-t pt-4 text-sm"><span className="text-muted-foreground">趋势范围</span><Input className="w-36" type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} /><span>至</span><Input className="w-36" type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} /><select className="h-9 rounded-md border border-input bg-background px-3" value={metric} onChange={(e) => setMetric(e.target.value as MetricKey)}>{metrics.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></CardContent>{importMutation.error && <CardContent className="pt-0 text-sm text-destructive"><AlertCircle className="mr-1 inline h-4 w-4" />{(importMutation.error as Error).message}</CardContent>}</Card>
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_360px]"><Card><CardHeader><CardTitle>账号日趋势 · {metricLabel(metric)}</CardTitle><p className="text-xs text-muted-foreground">{metricNote(metric)}</p></CardHeader><CardContent><div className="h-72">{chartData.length ? <ResponsiveContainer width="100%" height="100%"><LineChart data={chartData}><CartesianGrid strokeDasharray="3 3" className="stroke-border" /><XAxis dataKey="label" /><YAxis allowDecimals={metric !== 'exposure_count' && metric !== 'view_count'} /><Tooltip formatter={(value) => formatMetric(Number(value), metric)} /><Line type="monotone" dataKey={metric} stroke="#0f766e" strokeWidth={2} dot={false} connectNulls /></LineChart></ResponsiveContainer> : <p className="py-20 text-center text-sm text-muted-foreground">暂无已确认数据，请先导入并完成匹配。</p>}</div></CardContent></Card><Card><CardHeader><CardTitle>导入批次</CardTitle></CardHeader><CardContent className="space-y-2">{(batchesQuery.data?.items ?? []).map((batch) => <div key={batch.id} className={`rounded-md border p-3 text-sm ${selectedBatch === batch.id ? 'border-primary bg-primary/5' : 'border-border'}`}><button type="button" className="w-full text-left" onClick={() => setSelectedBatch(batch.id)}><div className="flex items-center justify-between gap-2"><span className="truncate font-medium">{batch.file_name}</span><span className="text-xs text-muted-foreground">{statusLabel(batch.status)}</span></div><div className="mt-1 text-xs text-muted-foreground">数据截至 {new Date(batch.data_as_of_at).toLocaleString('zh-CN')} · {batch.resolved_rows}/{batch.total_rows} 行已确认</div></button><div className="mt-2 flex items-center justify-between text-xs text-muted-foreground"><span>接收于 {new Date(batch.received_at).toLocaleString('zh-CN')}</span><Button variant="ghost" size="sm" className="h-7 px-2" title="下载原始文件" onClick={() => void downloadBatch(batch.id)}><Download className="mr-1 h-3.5 w-3.5" />下载</Button></div></div>)}{!batchesQuery.data?.items?.length && <p className="text-sm text-muted-foreground">还没有导入批次</p>}</CardContent></Card></div>
    {selectedBatch && batchQuery.data && <Card><CardHeader><CardTitle>待确认帖子 <span className="text-sm font-normal text-muted-foreground">{pendingRows.length} 条</span></CardTitle></CardHeader><CardContent><Table><TableHeader><TableRow><TableHead>源行</TableHead><TableHead>标题</TableHead><TableHead>首次发布时间</TableHead><TableHead>候选/处理</TableHead></TableRow></TableHeader><TableBody>{pendingRows.map((row) => { const candidates = candidatesFor(row); return <TableRow key={row.id}><TableCell>{row.source_row}</TableCell><TableCell className="max-w-[300px] truncate" title={row.title}>{row.title}</TableCell><TableCell>{row.first_published_at ? new Date(row.first_published_at).toLocaleString('zh-CN') : '未提供'}</TableCell><TableCell><div className="flex flex-wrap gap-2"><select className="h-8 max-w-[240px] rounded-md border border-input bg-background px-2 text-xs" defaultValue="" disabled={resolveMutation.isPending} aria-label={`为 ${row.title} 选择已有帖子`} onChange={(e) => { if (e.target.value) resolveMutation.mutate([{ row_id: row.id, action: 'link_existing', post_id: e.target.value }]) }}><option value="">选择已有帖子…</option>{posts.map((post) => <option key={post.id} value={post.id}>{post.title}</option>)}</select>{candidates.map((post) => <Button key={post.id} size="sm" variant="outline" disabled={resolveMutation.isPending} onClick={() => resolveMutation.mutate([{ row_id: row.id, action: 'link_existing', post_id: post.id }])}><Check className="h-4 w-4" />精确关联</Button>)}<Button size="sm" variant="outline" disabled={resolveMutation.isPending} onClick={() => resolveMutation.mutate([{ row_id: row.id, action: 'create_new' }])}>新建帖子</Button><Button size="sm" variant="ghost" disabled={resolveMutation.isPending} onClick={() => resolveMutation.mutate([{ row_id: row.id, action: 'skip' }])}>跳过</Button></div>{row.parse_error && <p className="mt-1 text-xs text-destructive">{row.parse_error}</p>}{!candidates.length && <p className="mt-1 text-xs text-muted-foreground">未找到精确候选，请确认后再新建</p>}</TableCell></TableRow> })}</TableBody></Table>{!pendingRows.length && <p className="py-4 text-sm text-muted-foreground">当前批次没有待处理行。</p>}</CardContent></Card>}
    <Card><CardHeader><CardTitle>帖子排行</CardTitle></CardHeader><CardContent className="space-y-4"><div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{(overviewQuery.data?.posts ?? posts).map((post, index) => <button key={post.id} type="button" className={`rounded-md border p-3 text-left text-sm ${selectedPost === post.id ? 'border-primary bg-primary/5' : 'border-border'}`} onClick={() => setSelectedPost(post.id)}><span className="mr-2 text-muted-foreground">#{index + 1}</span>{post.title}</button>)}</div>{selectedPost && postQuery.data && <div className="border-t pt-4"><p className="mb-3 text-sm font-medium">{postQuery.data.post.title} · {metricLabel(metric)}</p><div className="h-64"><ResponsiveContainer width="100%" height="100%"><LineChart data={postVersions.map((version) => ({ ...version, label: version.data_as_of_at.slice(0, 10) }))}><CartesianGrid strokeDasharray="3 3" className="stroke-border" /><XAxis dataKey="label" /><YAxis allowDecimals={metric !== 'exposure_count' && metric !== 'view_count'} /><Tooltip formatter={(value) => formatMetric(Number(value), metric)} /><Line type="monotone" dataKey={metric} stroke="#0f766e" strokeWidth={2} dot={false} connectNulls /></LineChart></ResponsiveContainer></div><Table className="mt-4"><TableHeader><TableRow><TableHead>数据日期</TableHead><TableHead>{metricLabel(metric)}</TableHead><TableHead>导入时间</TableHead></TableRow></TableHeader><TableBody>{postVersions.map((version) => <TableRow key={version.id}><TableCell>{version.data_as_of_at.slice(0, 10)}</TableCell><TableCell>{formatMetric(version[metric], metric)}</TableCell><TableCell>{new Date(version.imported_at).toLocaleString('zh-CN')}</TableCell></TableRow>)}</TableBody></Table></div>}{!posts.length && <p className="text-sm text-muted-foreground">确认匹配后，帖子会显示在这里。</p>}</CardContent></Card>
    {overviewQuery.isFetching && <div className="text-xs text-muted-foreground"><RefreshCw className="mr-1 inline h-3 w-3 animate-spin" />数据刷新中</div>}
  </div>
}
