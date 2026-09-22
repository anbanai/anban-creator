import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  BarChart3,
  CheckCircle2,
  CircleDashed,
  FileSpreadsheet,
  Link2,
  Loader2,
  RefreshCw,
  Upload,
  X,
} from 'lucide-react'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { uploadToOSS } from '@/lib/direct-upload'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import PageHeader from '@/components/layout/PageHeader'
import RevokeImportDialog from '@/components/common/RevokeImportDialog'
import ContentSourceLink from '@/components/common/ContentSourceLink'
import type {
  WechatAnalyticsArticleView,
  WechatAnalyticsImportBatch,
  WechatAnalyticsImportPreview,
  WechatAnalyticsImportRow,
} from '@/types/wechat-analytics-import'

type StatusFilter = 'all' | 'pending' | 'bind' | 'published' | 'abnormal'

const statusFilters: Array<{ value: StatusFilter; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'pending', label: '待发布' },
  { value: 'bind', label: '待绑定' },
  { value: 'published', label: '已发布' },
  { value: 'abnormal', label: '数据异常' },
]

function needsManualBind(article: WechatAnalyticsArticleView) {
  return article.publication.manual_publish_required || article.publication.status === 'awaiting_manual_publish'
    || (article.publication.status === 'unsupported' && Boolean(article.publication.draft_media_id))
}

function publicationLabel(article: WechatAnalyticsArticleView) {
  const publication = article.publication
  if (publication.status === 'published' && publication.source === 'wechat_console') return ['已人工发布', 'secondary'] as const
  if (publication.status === 'published') return ['API 已发布', 'default'] as const
  if (needsManualBind(article)) return ['待人工发布', 'outline'] as const
  if (publication.status === 'ambiguous') return ['结果待确认', 'destructive'] as const
  if (publication.status === 'publish_failed') return ['发布失败', 'destructive'] as const
  if (publication.status === 'drafted') return ['草稿已创建', 'outline'] as const
  if (publication.status === 'publishing') return ['发布处理中', 'secondary'] as const
  if (publication.status === 'drafting') return ['正在创建草稿', 'secondary'] as const
  return ['暂不可用', 'outline'] as const
}

function analyticsLabel(article: WechatAnalyticsArticleView) {
  if (article.latest) {
    return [article.latest.source === 'wechat_official_api' ? '官方接口' : article.latest.source || '表格导入', 'secondary'] as const
  }
  switch (article.publication.analytics_status) {
    case 'official_fetching': return ['官方数据同步中', 'outline'] as const
    case 'unsupported': return ['官方接口无权限', 'destructive'] as const
    case 'partial': return ['数据不完整', 'destructive'] as const
    default: return ['暂无数据', 'outline'] as const
  }
}

function formatNumber(value?: number) {
  return value == null ? '—' : new Intl.NumberFormat('zh-CN').format(value)
}

function formatRate(value?: number) {
  return value == null ? '—' : `${(value * 100).toFixed(1)}%`
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleDateString('zh-CN') : '尚未发布'
}

function contentKind(source?: string) {
  return /贴图|图文|图片消息|图集/.test(source?.trim() ?? '') ? '贴图' : '文章'
}

function normalizedMatchValue(value?: string) {
  const trimmed = value?.trim() ?? ''
  if (!trimmed) return ''
  try {
    const parsed = new URL(trimmed)
    parsed.protocol = parsed.protocol.toLowerCase()
    parsed.hostname = parsed.hostname.toLowerCase()
    parsed.hash = ''
    return parsed.toString()
  } catch {
    return trimmed.toLowerCase()
  }
}

function normalizedTitle(value?: string) {
  return value?.normalize('NFKC').trim().replace(/\s+/gu, '') ?? ''
}

function samePublishedDay(rowDate?: string, publishedAt?: string) {
  if (!rowDate || !publishedAt) return false
  const formatDay = (value: string) => new Date(value).toLocaleDateString('zh-CN', { timeZone: 'Asia/Shanghai' })
  const rowDay = formatDay(rowDate)
  const publicationDay = formatDay(publishedAt)
  return rowDay !== 'Invalid Date' && rowDay === publicationDay
}

type PreviewMatchLabel = '自动匹配' | '待确认' | '未匹配'

function previewMatch(row: WechatAnalyticsImportPreview['rows'][number], articles: WechatAnalyticsArticleView[]): { label: PreviewMatchLabel; title: string } {
  const rowURL = normalizedMatchValue(row.article_url)
  if (rowURL) {
    const urlMatch = articles.find((article) => normalizedMatchValue(article.publication.article_url) === rowURL)
    if (urlMatch) return { label: '自动匹配', title: urlMatch.publication.draft_title || '未命名文章' }
  }
  const titleMatches = articles.filter((article) => normalizedTitle(article.publication.draft_title) === normalizedTitle(row.title)
    && samePublishedDay(row.published_date, article.publication.published_at))
  if (titleMatches.length === 1) return { label: '自动匹配', title: titleMatches[0].publication.draft_title || '未命名文章' }
  return { label: titleMatches.length > 1 ? '待确认' : '未匹配', title: '' }
}

function matchesFilter(article: WechatAnalyticsArticleView, filter: StatusFilter) {
  const publication = article.publication
  if (filter === 'all') return true
  if (filter === 'bind') return needsManualBind(article)
  if (filter === 'published') return publication.status === 'published'
  if (filter === 'pending') {
    return ['drafting', 'drafted', 'publishing'].includes(publication.status) || needsManualBind(article)
  }
  return publication.status === 'ambiguous' || publication.status === 'publish_failed'
    || publication.analytics_status === 'unsupported' || publication.analytics_status === 'partial'
}

function ImportPreview({ preview, articles }: { preview: WechatAnalyticsImportPreview; articles: WechatAnalyticsArticleView[] }) {
  const typeCounts = preview.rows.reduce((counts, row) => {
    counts[contentKind(row.source)] += 1
    return counts
  }, { 文章: 0, 贴图: 0 })
  const matchCounts = preview.rows.reduce((counts, row) => {
    const match = previewMatch(row, articles)
    counts[match.label] += 1
    return counts
  }, { 自动匹配: 0, 待确认: 0, 未匹配: 0 } as Record<PreviewMatchLabel, number>)
  return <div className="space-y-4">
    <div className="grid grid-cols-2 gap-3 rounded-lg border bg-muted/20 p-3 text-sm sm:grid-cols-5">
      <div><p className="text-xs text-muted-foreground">文件</p><p className="mt-1 truncate font-medium">{preview.file_name}</p></div>
      <div><p className="text-xs text-muted-foreground">数据行</p><p className="mt-1 font-medium">{preview.total_rows}</p></div>
      <div><p className="text-xs text-muted-foreground">预览类型（前 10 行）</p><p className="mt-1 font-medium">{typeCounts.文章} / {typeCounts.贴图}</p></div>
      <div><p className="text-xs text-muted-foreground">预览自动匹配</p><p className="mt-1 font-medium text-primary">{matchCounts.自动匹配} 条</p></div>
      <div><p className="text-xs text-muted-foreground">预览待处理</p><p className="mt-1 font-medium">{matchCounts.待确认 + matchCounts.未匹配} 条</p></div>
    </div>
    <div>
      <p className="mb-2 text-sm font-medium">字段映射</p>
      <div className="grid gap-px overflow-hidden rounded-md border bg-border text-xs sm:grid-cols-2">
        {preview.field_mapping.map((field) => <div key={field.internal_field} className="flex items-center justify-between gap-3 bg-background px-3 py-2"><span>{field.excel_field}</span><span className="font-mono text-muted-foreground">{field.internal_field}</span></div>)}
      </div>
    </div>
    <div>
      <div className="mb-2 flex items-center justify-between gap-3"><p className="text-sm font-medium">前 {Math.min(10, preview.rows.length)} 行预览</p><p className="text-xs text-muted-foreground">系统会优先按链接和标题自动匹配</p></div>
      <div className="max-h-64 overflow-auto rounded-md border">
        <Table>
          <TableHeader><TableRow><TableHead>类型</TableHead><TableHead>标题</TableHead><TableHead>匹配</TableHead><TableHead>日期</TableHead><TableHead>阅读</TableHead><TableHead>分享</TableHead><TableHead>完成率</TableHead></TableRow></TableHeader>
          <TableBody>{preview.rows.map((row) => { const match = previewMatch(row, articles); return <TableRow key={row.source_row}><TableCell><Badge variant={contentKind(row.source) === '贴图' ? 'secondary' : 'outline'}>{contentKind(row.source)}</Badge></TableCell><TableCell className="min-w-56 max-w-72"><p className="truncate">{row.title}</p><ContentSourceLink url={row.article_url} className="mt-1" /></TableCell><TableCell><div className="min-w-24"><Badge variant={match.label === '自动匹配' ? 'default' : 'outline'}>{match.label}</Badge>{match.title && <p className="mt-1 max-w-36 truncate text-xs text-muted-foreground" title={match.title}>{match.title}</p>}</div></TableCell><TableCell className="whitespace-nowrap">{row.published_date ? formatDate(row.published_date) : '—'}</TableCell><TableCell>{formatNumber(row.read_users)}</TableCell><TableCell>{formatNumber(row.share_users)}</TableCell><TableCell>{formatRate(row.read_completion_rate)}</TableCell></TableRow> })}</TableBody>
        </Table>
      </div>
    </div>
  </div>
}

export default function WechatDataPage() {
  const queryClient = useQueryClient()
  const [projectId, setProjectId] = useState('')
  const [revokeTarget, setRevokeTarget] = useState<{ projectId: string; batch: WechatAnalyticsImportBatch } | null>(null)
  const [filter, setFilter] = useState<StatusFilter>('all')
  const [importOpen, setImportOpen] = useState(false)
  const [stagedFile, setStagedFile] = useState<File | null>(null)
  const [uploadId, setUploadId] = useState('')
  const [preview, setPreview] = useState<WechatAnalyticsImportPreview | null>(null)
  const [fileError, setFileError] = useState('')
  const [selectedArticle, setSelectedArticle] = useState<WechatAnalyticsArticleView | null>(null)
  const [url, setUrl] = useState('')
  const [bindingMode, setBindingMode] = useState(false)
  const [selectedBatch, setSelectedBatch] = useState('')
  const [resolveSelections, setResolveSelections] = useState<Record<string, string>>({})

  const projectsQuery = useQuery({ queryKey: queryKeys.projects.list({ platform: 'article' }), queryFn: () => api.projects.list({ platform: 'article' }) })
  const projects = projectsQuery.data ?? []
  const activeProjectId = projectId || projects[0]?.id || ''
  const articlesQuery = useQuery({ queryKey: queryKeys.wechatAnalyticsImport.articles(activeProjectId), queryFn: () => api.wechatAnalyticsImport.articles(activeProjectId), enabled: Boolean(activeProjectId) })
  const overviewQuery = useQuery({ queryKey: queryKeys.wechatAnalyticsImport.overview(activeProjectId), queryFn: () => api.wechatAnalyticsImport.overview(activeProjectId), enabled: Boolean(activeProjectId) })
  const batchesQuery = useQuery({ queryKey: queryKeys.wechatAnalyticsImport.batches(activeProjectId), queryFn: () => api.wechatAnalyticsImport.listBatches(activeProjectId), enabled: Boolean(activeProjectId) })
  const batchQuery = useQuery({ queryKey: queryKeys.wechatAnalyticsImport.batch(activeProjectId, selectedBatch), queryFn: () => api.wechatAnalyticsImport.getBatch(activeProjectId, selectedBatch), enabled: Boolean(activeProjectId && selectedBatch) })

  const refreshWorkbench = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.wechatAnalyticsImport.batches(activeProjectId) })
    queryClient.invalidateQueries({ queryKey: queryKeys.wechatAnalyticsImport.articles(activeProjectId) })
    queryClient.invalidateQueries({ queryKey: queryKeys.wechatAnalyticsImport.overview(activeProjectId) })
  }
  const previewMutation = useMutation({
    mutationFn: async (file: File) => {
      const upload = await uploadToOSS({ purpose: 'wechat_analytics_import', file })
      const result = await api.wechatAnalyticsImport.preview(activeProjectId, { upload_id: upload.uploadId, timezone: 'Asia/Shanghai' })
      return { result, uploadId: upload.uploadId }
    },
    onSuccess: ({ result, uploadId: nextUploadId }) => {
      setPreview(result)
      setUploadId(nextUploadId)
    },
  })
  const importMutation = useMutation({
    mutationFn: () => api.wechatAnalyticsImport.import(activeProjectId, {
      upload_id: uploadId,
      timezone: 'Asia/Shanghai',
      client_file_modified_at: stagedFile ? new Date(stagedFile.lastModified).toISOString() : undefined,
    }),
    onSuccess: (result) => {
      setSelectedBatch(result.batch_id)
      closeImport()
      refreshWorkbench()
    },
  })
  const resolveMutation = useMutation({
    mutationFn: (actions: Array<{ row_id: string; action: string; publication_id?: string }>) => api.wechatAnalyticsImport.resolve(activeProjectId, selectedBatch, actions),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.wechatAnalyticsImport.batch(activeProjectId, selectedBatch) })
      queryClient.invalidateQueries({ queryKey: queryKeys.wechatAnalyticsImport.articles(activeProjectId) })
      queryClient.invalidateQueries({ queryKey: queryKeys.wechatAnalyticsImport.overview(activeProjectId) })
    },
  })
  const revokeMutation = useMutation({
    mutationFn: ({ projectId, batch }: { projectId: string; batch: WechatAnalyticsImportBatch }) => api.wechatAnalyticsImport.revoke(projectId, batch.id),
    onSuccess: async (summary, { projectId }) => {
      queryClient.setQueryData(queryKeys.wechatAnalyticsImport.batch(projectId, summary.batch.id), summary)
      setSelectedArticle(null)
      setBindingMode(false)
      setUrl('')
      setResolveSelections({})
      setRevokeTarget(null)
      await queryClient.invalidateQueries({ predicate: ({ queryKey }) =>
        typeof queryKey[0] === 'string' && queryKey[0].startsWith('wechat-import-') && queryKey[1] === projectId,
      })
      void queryClient.invalidateQueries({ predicate: ({ queryKey }) =>
        ['task', 'tasks', 'wechat-publication', 'wechat-analytics'].includes(String(queryKey[0])),
      })
    },
  })
  const bindMutation = useMutation({
    mutationFn: () => selectedArticle ? api.tasks.bindManualWechatPublication(selectedArticle.publication.task_id, url.trim()) : Promise.reject(new Error('请选择文章')),
    onSuccess: (publication) => {
      setBindingMode(false)
      setUrl('')
      setSelectedArticle((current) => current ? { ...current, publication } : current)
      refreshWorkbench()
    },
  })
  const reconcileMutation = useMutation({
    mutationFn: (taskId: string) => api.tasks.reconcileWechat(taskId),
    onSuccess: refreshWorkbench,
  })

  const articles = articlesQuery.data?.items ?? []
  const visibleArticles = useMemo(() => articles.filter((article) => matchesFilter(article, filter)), [articles, filter])
  const reviewRows = useMemo(() => (batchQuery.data?.rows ?? []).filter((row: WechatAnalyticsImportRow) => row.match_status !== 'matched'), [batchQuery.data?.rows])
  const dataAsOf = useMemo(() => {
    const dates = articles.flatMap((article) => article.latest?.data_as_of_at ? [new Date(article.latest.data_as_of_at).getTime()] : [])
    return dates.length ? new Date(Math.max(...dates)).toLocaleString('zh-CN') : '暂无数据'
  }, [articles])

  function closeImport() {
    setImportOpen(false)
    setStagedFile(null)
    setUploadId('')
    setPreview(null)
    setFileError('')
    previewMutation.reset()
    importMutation.reset()
  }
  function stageFile(file?: File) {
    setFileError('')
    setUploadId('')
    setPreview(null)
    previewMutation.reset()
    if (!file) return
    if (!/\.(xls|xlsx)$/i.test(file.name)) {
      setStagedFile(null)
      setFileError('请选择 .xls 或 .xlsx 文件')
      return
    }
    if (file.size > 20 * 1024 * 1024) {
      setStagedFile(null)
      setFileError('文件不能超过 20 MB')
      return
    }
    setStagedFile(file)
    previewMutation.mutate(file)
  }
  function openBind(article: WechatAnalyticsArticleView) {
    setSelectedArticle(article)
    setUrl(article.publication.article_url ?? '')
    setBindingMode(true)
  }

  return <div className="space-y-5">
    <PageHeader title="公众号数据" description="文章、发布与数据在同一条内容流中管理。未认证账号会自动转为人工发布。">
      {activeProjectId && <Button onClick={() => setImportOpen(true)}><Upload />导入数据</Button>}
    </PageHeader>

    <section className="border-y bg-card/40 py-4">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <label className="grid gap-2 text-sm" htmlFor="wechat-account"><span className="font-medium">公众号项目</span><select id="wechat-account" className="h-10 min-w-64 rounded-md border border-input bg-background px-3" value={activeProjectId} disabled={revokeMutation.isPending} onChange={(event) => { setProjectId(event.target.value); setSelectedBatch(''); setSelectedArticle(null); setBindingMode(false); setRevokeTarget(null); revokeMutation.reset() }}><option value="">请选择项目</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-muted-foreground">
          {overviewQuery.data && <><span>文章 <strong className="font-medium text-foreground">{overviewQuery.data.articles}</strong></span><span>已发布 <strong className="font-medium text-foreground">{overviewQuery.data.published}</strong></span><span>有数据 <strong className="font-medium text-foreground">{overviewQuery.data.with_data}</strong></span><span>阅读 <strong className="font-medium text-foreground">{formatNumber(overviewQuery.data.read_users)}</strong></span></>}
          <span>数据截至 <strong className="font-medium text-foreground">{dataAsOf}</strong></span>
        </div>
      </div>
    </section>

    <div className="overflow-x-auto pb-1">
      <ToggleGroup value={[filter]} onValueChange={(value) => { const next = value[0] as StatusFilter | undefined; if (next) setFilter(next) }} variant="outline" spacing={0} aria-label="文章状态筛选">
        {statusFilters.map((item) => <ToggleGroupItem key={item.value} value={item.value}>{item.label}</ToggleGroupItem>)}
      </ToggleGroup>
    </div>

    {articlesQuery.isLoading && <div className="flex items-center gap-2 py-12 text-sm text-muted-foreground"><Loader2 className="animate-spin" />正在加载文章</div>}
    {articlesQuery.error && <Alert variant="destructive"><AlertCircle /><AlertTitle>读取失败</AlertTitle><AlertDescription>{(articlesQuery.error as Error).message}</AlertDescription></Alert>}
    {activeProjectId && !articlesQuery.isLoading && <section className="overflow-hidden rounded-md border bg-card">
      <div className="flex items-end justify-between border-b px-4 py-3"><div><h2 className="font-medium">内容流</h2><p className="mt-1 text-xs text-muted-foreground">仅呈现当前状态和下一步，缺失指标保持为空。</p></div><span className="text-xs text-muted-foreground">{visibleArticles.length} 篇</span></div>
      <div className="overflow-x-auto max-md:overflow-visible"><Table className="max-md:block"><TableHeader className="max-md:hidden"><TableRow><TableHead>文章</TableHead><TableHead>发布渠道</TableHead><TableHead>当前状态</TableHead><TableHead>数据来源</TableHead><TableHead>阅读</TableHead><TableHead>分享</TableHead><TableHead>阅读完成率</TableHead><TableHead className="text-right">下一步</TableHead></TableRow></TableHeader><TableBody className="max-md:block">{visibleArticles.map((article) => {
        const [label, variant] = publicationLabel(article)
        const [dataLabel, dataVariant] = analyticsLabel(article)
        const publication = article.publication
        return <TableRow key={publication.id} className="max-md:grid max-md:grid-cols-6 max-md:gap-x-3 max-md:gap-y-3 max-md:p-3">
          <TableCell className="min-w-72 max-w-[380px] max-md:col-span-6 max-md:min-w-0 max-md:max-w-none max-md:p-0"><button type="button" className="max-w-full truncate text-left font-medium hover:text-primary" onClick={() => setSelectedArticle(article)}>{publication.draft_title || '未命名文章'}</button><p className="mt-1 text-xs text-muted-foreground">{formatDate(publication.published_at)}<ContentSourceLink url={publication.article_url} className="ml-2 align-bottom" /></p></TableCell>
          <TableCell className="whitespace-nowrap max-md:col-span-3 max-md:p-0"><span className="mb-1 block text-xs text-muted-foreground md:hidden">发布渠道</span>{publication.source === 'wechat_console' ? '人工发布' : 'API 发布'}</TableCell>
          <TableCell className="max-md:col-span-3 max-md:p-0"><span className="mb-1 block text-xs text-muted-foreground md:hidden">当前状态</span><Badge variant={variant}>{label}</Badge></TableCell>
          <TableCell className="max-md:col-span-6 max-md:flex max-md:items-center max-md:justify-between max-md:border-y max-md:py-2"><span className="text-xs text-muted-foreground md:hidden">数据来源</span><Badge variant={dataVariant}>{dataLabel}</Badge></TableCell>
          <TableCell className="max-md:col-span-2 max-md:p-0"><span className="mb-1 block text-xs text-muted-foreground md:hidden">阅读</span>{formatNumber(article.latest?.read_users)}</TableCell>
          <TableCell className="max-md:col-span-2 max-md:p-0"><span className="mb-1 block text-xs text-muted-foreground md:hidden">分享</span>{formatNumber(article.latest?.share_users)}</TableCell>
          <TableCell className="max-md:col-span-2 max-md:p-0"><span className="mb-1 block text-xs text-muted-foreground md:hidden">完成率</span>{formatRate(article.latest?.read_completion_rate)}</TableCell>
          <TableCell className="max-md:col-span-6 max-md:p-0"><span className="sr-only md:hidden">下一步</span><div className="flex justify-end max-md:[&>button]:w-full">{needsManualBind(article) ? <Button size="sm" variant="outline" onClick={() => openBind(article)}><Link2 />确认已在公众号发布</Button> : publication.status === 'ambiguous' ? <Button size="sm" variant="outline" disabled={reconcileMutation.isPending} onClick={() => reconcileMutation.mutate(publication.task_id)}><RefreshCw />重试对账</Button> : publication.status === 'published' ? <Button size="sm" variant="ghost" onClick={() => setSelectedArticle(article)}><BarChart3 />查看数据</Button> : <Button size="sm" variant="ghost" onClick={() => setSelectedArticle(article)}>查看详情</Button>}</div></TableCell>
        </TableRow>
      })}</TableBody></Table></div>
      {!visibleArticles.length && <p className="py-14 text-center text-sm text-muted-foreground">当前筛选下没有文章。</p>}
    </section>}

    {selectedBatch && batchQuery.data && (batchQuery.data.batch.status === 'revoked' ? <Alert><AlertTitle>已撤销</AlertTitle><AlertDescription>此批次仅保留导入历史，不再计入统计。请修改文件后重新导入。</AlertDescription></Alert> : <Card><CardHeader><CardTitle>批次异常 <span className="text-sm font-normal text-muted-foreground">{reviewRows.length}</span></CardTitle><CardDescription>正常行已自动入库，只需处理歧义、未匹配和格式错误。</CardDescription></CardHeader><CardContent>{reviewRows.length ? <div className="overflow-x-auto"><Table><TableHeader><TableRow><TableHead>源行</TableHead><TableHead>类型</TableHead><TableHead>标题</TableHead><TableHead>状态</TableHead><TableHead>匹配文章</TableHead><TableHead className="text-right">处理</TableHead></TableRow></TableHeader><TableBody>{reviewRows.map((row) => <TableRow key={row.id}><TableCell>{row.source_row}</TableCell><TableCell><Badge variant={contentKind(row.source) === '贴图' ? 'secondary' : 'outline'}>{contentKind(row.source)}</Badge></TableCell><TableCell className="min-w-64 max-w-[360px]"><p className="truncate">{row.title}</p><ContentSourceLink url={row.article_url} className="mt-1" />{row.parse_error && <p className="mt-1 text-xs text-destructive">{row.parse_error}</p>}</TableCell><TableCell><Badge variant={row.match_status === 'invalid' ? 'destructive' : 'outline'}>{row.match_status === 'needs_review' ? '需复核' : row.match_status === 'invalid' ? '格式错误' : '未匹配'}</Badge></TableCell><TableCell>{row.match_status !== 'invalid' ? <select aria-label={`匹配文章 ${row.source_row}`} className="h-9 max-w-72 rounded-md border bg-background px-2 text-sm" value={resolveSelections[row.id] ?? ''} onChange={(event) => setResolveSelections((current) => ({ ...current, [row.id]: event.target.value }))}><option value="">选择现有文章</option>{articles.map((article) => <option key={article.publication.id} value={article.publication.id}>{article.publication.draft_title || '未命名文章'}</option>)}</select> : '—'}</TableCell><TableCell>{row.match_status === 'invalid' ? <p className="text-right text-xs text-muted-foreground">修正源文件后重新导入</p> : <div className="flex justify-end gap-1"><Button size="sm" disabled={!resolveSelections[row.id] || resolveMutation.isPending || revokeMutation.isPending} onClick={() => resolveMutation.mutate([{ row_id: row.id, action: 'link_existing', publication_id: resolveSelections[row.id] }])}>确认匹配</Button><Button size="sm" variant="ghost" disabled={resolveMutation.isPending || revokeMutation.isPending} onClick={() => resolveMutation.mutate([{ row_id: row.id, action: 'skip' }])}>跳过</Button></div>}</TableCell></TableRow>)}</TableBody></Table></div> : <div className="flex items-center gap-2 text-sm text-muted-foreground"><CheckCircle2 className="text-primary" />此批次没有待处理行</div>}</CardContent></Card>)}

    {batchesQuery.data?.items?.length ? <details className="rounded-md border bg-card px-4 py-3"><summary className="cursor-pointer text-sm font-medium">历史导入 {batchesQuery.data.total}</summary><div className="mt-3 grid gap-2 sm:grid-cols-2">{batchesQuery.data.items.map((batch) => <div key={batch.id} className={`rounded-md border px-3 py-2 text-sm ${selectedBatch === batch.id ? 'border-primary bg-primary/5' : ''}`}>
      <button type="button" className="flex w-full items-center justify-between gap-3 text-left" onClick={() => setSelectedBatch(batch.id)}><span className="truncate"><FileSpreadsheet className="mr-2 inline h-4 w-4" />{batch.file_name}</span><span className="shrink-0 text-xs text-muted-foreground">{batch.status === 'revoked' ? '已撤销' : `${batch.matched_rows}/${batch.total_rows} 已匹配`}</span></button>
      {batch.revoked_at && <time className="mt-1 block text-xs text-muted-foreground" dateTime={batch.revoked_at}>{new Date(batch.revoked_at).toLocaleString('zh-CN')}</time>}
      {batch.status !== 'revoked' && <Button variant="ghost" size="sm" className="mt-1 text-destructive" disabled={revokeMutation.isPending || resolveMutation.isPending || importMutation.isPending} onClick={() => { revokeMutation.reset(); setRevokeTarget({ projectId: activeProjectId, batch }) }}>撤销本次导入</Button>}
    </div>)}</div></details> : null}

    <RevokeImportDialog fileName={revokeTarget?.batch.file_name} pending={revokeMutation.isPending} error={revokeMutation.error} onCancel={() => { setRevokeTarget(null); revokeMutation.reset() }} onConfirm={() => revokeTarget && revokeMutation.mutate(revokeTarget)} />

    <Sheet open={Boolean(selectedArticle)} onOpenChange={(open) => { if (!open) { setSelectedArticle(null); setBindingMode(false) } }}><SheetContent side="right" className="w-full overflow-y-auto sm:max-w-xl"><SheetHeader className="border-b pr-14"><SheetTitle>{selectedArticle?.publication.draft_title || '文章详情'}</SheetTitle><SheetDescription>{bindingMode ? '确认发布状态并可选绑定文章 URL' : '发布与数据时间线'}</SheetDescription></SheetHeader>{selectedArticle && <div className="space-y-5 px-5 pb-6">
      {bindingMode ? <div className="space-y-4 rounded-lg border bg-muted/20 p-4"><div><p className="font-medium">确认已在公众号发布</p><p className="mt-1 text-sm text-muted-foreground">这一步只会更新系统状态，不会再次发布文章。填写 URL 后，后续导入数据会优先匹配它。</p></div><Input value={url} onChange={(event) => setUrl(event.target.value)} placeholder="可选：https://mp.weixin.qq.com/s/..." aria-label="文章 URL（可选）" /><ContentSourceLink url={url} />{bindMutation.error && <p className="text-sm text-destructive">{(bindMutation.error as Error).message}</p>}<div className="flex justify-end gap-2"><Button variant="outline" disabled={bindMutation.isPending} onClick={() => setBindingMode(false)}>返回详情</Button><Button disabled={bindMutation.isPending} onClick={() => bindMutation.mutate()}>{bindMutation.isPending ? <Loader2 className="animate-spin" /> : <Link2 />}保存发布状态</Button></div></div> : <>
      <ContentSourceLink url={selectedArticle.publication.article_url} />
      <div className="space-y-0">{[
        ['内容生成', '已完成', true],
        ['草稿创建', selectedArticle.publication.draft_media_id ? '已创建' : '等待中', Boolean(selectedArticle.publication.draft_media_id)],
        ['正式发布', publicationLabel(selectedArticle)[0], selectedArticle.publication.status === 'published'],
        ['URL 绑定', selectedArticle.publication.article_url ? '已绑定' : '未绑定（可选）', Boolean(selectedArticle.publication.article_url)],
        ['数据获取', selectedArticle.latest ? `截至 ${new Date(selectedArticle.latest.data_as_of_at).toLocaleString('zh-CN')}` : analyticsLabel(selectedArticle)[0], Boolean(selectedArticle.latest)],
      ].map(([title, description, complete], index, entries) => <div key={String(title)} className="grid grid-cols-[24px_1fr] gap-3"><div className="flex flex-col items-center">{complete ? <CheckCircle2 className="h-5 w-5 text-primary" /> : <CircleDashed className="h-5 w-5 text-muted-foreground" />}{index < entries.length - 1 && <span className="h-10 w-px bg-border" />}</div><div><p className="font-medium">{title}</p><p className="mt-0.5 text-sm text-muted-foreground">{description}</p></div></div>)}</div>
      {selectedArticle.publication.last_error && <Alert variant={selectedArticle.publication.wechat_status_code === 48001 ? 'default' : 'destructive'}><AlertCircle /><AlertTitle>{selectedArticle.publication.wechat_status_code === 48001 ? '草稿已创建，需要人工发布' : '需要处理'}</AlertTitle><AlertDescription className="space-y-2"><p>{selectedArticle.publication.wechat_status_code === 48001 ? '当前公众号没有正式发布接口权限。请前往公众号后台发布此草稿；发布后返回确认，文章 URL 可选。' : selectedArticle.publication.last_error}</p><dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs"><dt>微信错误码</dt><dd>{selectedArticle.publication.wechat_status_code || '—'}</dd><dt>能否重试</dt><dd>{selectedArticle.publication.status === 'ambiguous' ? '先对账' : '按当前提示操作'}</dd><dt>是否新建草稿</dt><dd>不会</dd><dt>草稿 media_id</dt><dd className="truncate font-mono" title={selectedArticle.publication.draft_media_id}>{selectedArticle.publication.draft_media_id || '—'}</dd><dt>publish_id</dt><dd className="truncate font-mono" title={selectedArticle.publication.publish_id}>{selectedArticle.publication.publish_id || '—'}</dd></dl></AlertDescription></Alert>}
      {needsManualBind(selectedArticle) && <Button className="w-full" onClick={() => openBind(selectedArticle)}><Link2 />确认已在公众号发布</Button>}
      </>}
    </div>}</SheetContent></Sheet>

    <Dialog open={importOpen} onOpenChange={(open) => { if (!open && !previewMutation.isPending && !importMutation.isPending) closeImport() }}><DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-4xl"><DialogHeader><DialogTitle>导入公众号数据</DialogTitle><p className="text-sm text-muted-foreground">上传后自动解析并展示预览，系统会先按链接和标题匹配已有文章。</p></DialogHeader><div className="space-y-4"><label htmlFor="wechat-import-file" className="flex min-h-32 cursor-pointer flex-col items-center justify-center rounded-md border border-dashed bg-muted/20 px-5 text-center hover:border-primary/50"><FileSpreadsheet className="mb-3 h-8 w-8 text-primary" /><span className="text-sm font-medium">选择公众号导出文件</span><span className="mt-1 text-xs text-muted-foreground">支持 .xls 和 .xlsx，最大 20 MB</span><input id="wechat-import-file" className="sr-only" type="file" accept=".xls,.xlsx,application/vnd.ms-excel,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" onChange={(event) => { stageFile(event.target.files?.[0]); event.currentTarget.value = '' }} /></label>{stagedFile && <div className="flex items-center justify-between rounded-md border p-3 text-sm"><span className="truncate">{stagedFile.name}</span><Button size="icon-sm" variant="ghost" aria-label="移除文件" onClick={() => { previewMutation.reset(); setPreview(null); setUploadId(''); setStagedFile(null) }}><X /></Button></div>}{previewMutation.isPending && <div className="flex items-center gap-2 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-sm text-primary"><Loader2 className="h-4 w-4 animate-spin" />正在解析并自动匹配数据…</div>}{preview && <ImportPreview preview={preview} articles={articles} />}{fileError && <p className="text-sm text-destructive">{fileError}</p>}{previewMutation.error && <p className="text-sm text-destructive">{(previewMutation.error as Error).message}</p>}{importMutation.error && <p className="text-sm text-destructive">{(importMutation.error as Error).message}</p>}</div><DialogFooter><Button variant="outline" disabled={previewMutation.isPending || importMutation.isPending} onClick={closeImport}>取消</Button>{preview && <><Button variant="outline" disabled={previewMutation.isPending || importMutation.isPending} onClick={() => { previewMutation.reset(); setPreview(null); setUploadId(''); setStagedFile(null) }}>重新选择</Button><Button disabled={importMutation.isPending || previewMutation.isPending} onClick={() => importMutation.mutate()}>{importMutation.isPending ? <><Loader2 className="animate-spin" />正在导入</> : '确认导入'}</Button></>}</DialogFooter></DialogContent></Dialog>
  </div>
}
