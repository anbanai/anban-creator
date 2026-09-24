import { api } from '@/lib/api'
import { http, unwrap } from '@/lib/http-client'
import { addDays, bucketDailySeries, periodDates, periodError, periodKey, shanghaiDay, type AnalyticsPeriod, type AnalyticsPoint } from '@/lib/analytics-period'
import type { Project } from '@/types'
import type { AnalyticsCandidate, AnalyticsImportPayload, AnalyticsPreview, AnalyticsTarget } from '@/types/content-analytics'
import type { SeednoteMetricVersion } from '@/types/seednote-import'
import type { WechatAnalyticsSnapshot } from '@/types/wechat-analytics-import'

export type AnalyticsPlatform = 'article' | 'seednote'
export interface AnalyticsMetric { key: string; label: string; color: string; kind: 'count' | 'rate' | 'duration'; summary: boolean; format: (value: number) => string }
export interface AnalyticsContent { id: string; title: string; contentType: string; status?: string; url?: string; date?: string; metrics: Record<string, number | null> }
export interface ContentAnalyticsData { contents: AnalyticsContent[]; selected?: AnalyticsContent; series: AnalyticsPoint[]; totals: Record<string, number | null>; availableDates: string[] }
const metric = (key: string, label: string, kind: AnalyticsMetric['kind'] = 'count', summary = true): AnalyticsMetric => ({ key, label, kind, summary, color: 'var(--primary)', format: (value) => kind === 'rate' ? `${(value * 100).toFixed(2)}%` : kind === 'duration' ? `${value.toFixed(1)} 秒` : value.toLocaleString('zh-CN') })
const wechatMetrics = [metric('read_users', '阅读人数'), metric('share_users', '分享人数'), metric('read_to_follow_users', '阅读后关注'), metric('delivered_users', '送达人数'), metric('read_completion_rate', '阅读完成率', 'rate', false)]
const seednoteMetrics = [metric('exposure_count', '曝光'), metric('view_count', '观看量'), metric('like_count', '点赞'), metric('comment_count', '评论'), metric('collect_count', '收藏'), metric('follower_gain_count', '涨粉'), metric('cover_click_rate', '封面点击率', 'rate', false), metric('share_count', '分享', 'count', false), metric('avg_watch_duration', '人均观看时长', 'duration', false), metric('barrage_count', '弹幕', 'count', false)]
export const metricsFor = (platform: string) => platform === 'article' ? wechatMetrics : seednoteMetrics
export const targetKey = (target: AnalyticsTarget) => `${target.kind}:${target.id}`
export const contentTypeLabel = (type: string) => ({ article: '文章', image: '贴图', image_text: '图文', '图文': '图文', '视频': '视频', video: '视频', unknown: '类型未知' }[type] ?? '类型未知')
export function descriptionFor(platform: string, selected: boolean) { return platform === 'article' ? `${selected ? '每个周期取该内容' : '每篇内容在每个周期取'}最新累计记录，不代表当日新增；缺失记录不补零。` : '计数按日求和；比率与时长按有效日期平均，缺失数据不计作零。' }
const nativePath = (project: Project) => `/projects/${project.id}/${project.platform === 'article' ? 'wechat' : 'seednote'}-analytics`
export const contentAnalyticsApi = {
  candidates: (projectId: string, params: { search?: string; offset?: number; limit?: number }) => unwrap<{ items: AnalyticsCandidate[]; total: number }>(http.get(`/projects/${projectId}/content-analytics/candidates`, { params })),
  preview: (project: Project, uploadId: string) => unwrap<AnalyticsPreview>(http.post(`${nativePath(project)}/imports/preview`, { upload_id: uploadId, timezone: 'Asia/Shanghai' })),
  import: async (project: Project, payload: AnalyticsImportPayload) => {
    const result = await unwrap<{ matched_rows?: number; batch?: { resolved_rows: number } }>(http.post(`${nativePath(project)}/imports`, payload))
    return { count: result.batch?.resolved_rows ?? result.matched_rows ?? 0, date: shanghaiDay(payload.data_as_of_at) }
  },
}
type Numeric = Record<string, unknown>
function values(item: Numeric | undefined, metrics: AnalyticsMetric[]) { return Object.fromEntries(metrics.map(({ key }) => [key, typeof item?.[key] === 'number' ? item[key] as number : null])) }
function aggregate(items: Numeric[], metrics: AnalyticsMetric[]) {
  return Object.fromEntries(metrics.map(({ key, kind }) => {
    const numbers = items.map((item) => item[key]).filter((n): n is number => typeof n === 'number' && Number.isFinite(n))
    return [key, numbers.length ? numbers.reduce((a, b) => a + b, 0) / (kind === 'count' ? 1 : numbers.length) : null]
  }))
}
function seednoteType(genre?: string) { return genre === 'video' || genre === '视频' ? 'video' : ['image', 'image_text', '图文', '图文笔记'].includes(genre ?? '') ? 'image_text' : 'unknown' }
function dailyVersions(versions: SeednoteMetricVersion[]) {
  const daily = new Map<string, SeednoteMetricVersion>()
  for (const version of versions) { const date = shanghaiDay(version.data_as_of_at); const old = daily.get(date); if (!old || Date.parse(version.imported_at) > Date.parse(old.imported_at) || (Date.parse(version.imported_at) === Date.parse(old.imported_at) && version.id > old.id)) daily.set(date, version) }
  return [...daily].map(([date, version]) => ({ ...version, date }))
}
async function allSeednotePosts(projectId: string) {
  const items = [] as Awaited<ReturnType<typeof api.seednoteImport.posts>>['items']
  for (let offset = 0; ; ) {
    const page = await api.seednoteImport.posts(projectId, undefined, { offset, limit: 200 })
    items.push(...page.items); offset += page.items.length
    if (!page.items.length || offset >= page.total) return items
  }
}
async function allSeednoteBatches(projectId: string) {
  const items = [] as Awaited<ReturnType<typeof api.seednoteImport.listBatches>>['items']
  for (let offset = 0; ; ) {
    const page = await api.seednoteImport.listBatches(projectId, { offset, limit: 100 })
    items.push(...page.items); offset += page.items.length
    if (!page.items.length || offset >= page.total) return items
  }
}
export async function loadContentAnalytics(project: Project, period: AnalyticsPeriod, contentId?: string): Promise<ContentAnalyticsData> {
  const metrics = metricsFor(project.platform)
  if (periodError(period.from, period.to)) return { contents: [], series: [], totals: aggregate([], metrics), availableDates: [] }
  if (project.platform === 'article') {
    const result = await api.wechatAnalyticsImport.articles(project.id)
    const all = result.items.map((article) => {
      const snapshots = [...(article.snapshots ?? [])]
      if (article.latest && !snapshots.some((snapshot) => snapshot.id === article.latest!.id)) snapshots.push(article.latest)
      snapshots.sort((a, b) => Date.parse(b.data_as_of_at) - Date.parse(a.data_as_of_at) || (a.source === b.source ? Date.parse(b.imported_at) - Date.parse(a.imported_at) : a.source === 'wechat_official_api' ? -1 : 1))
      const id = targetKey(article.target ?? (article.publication?.task_id ? { kind: 'task', id: article.publication.task_id } : { kind: 'wechat_publication', id: article.publication?.id ?? article.task!.id }))
      const within = snapshots.filter((point) => shanghaiDay(point.data_as_of_at) >= period.from && shanghaiDay(point.data_as_of_at) <= period.to)
      const content: AnalyticsContent = { id, title: article.task?.title || article.publication?.draft_title || '未命名内容', contentType: article.content_type ?? 'unknown', status: article.task?.status ?? article.publication?.status, url: article.url || article.publication?.article_url, date: within[0] ? shanghaiDay(within[0].data_as_of_at) : undefined, metrics: values(within[0] as unknown as Numeric, metrics) }
      return { content, within, snapshots }
    })
    const scoped = contentId ? all.filter((item) => item.content.id === contentId) : all
    const buckets = new Map<string, WechatAnalyticsSnapshot[]>()
    for (const item of scoped) {
      const seen = new Set<string>()
      for (const snapshot of item.within) { const date = periodKey(shanghaiDay(snapshot.data_as_of_at), period.granularity); if (!seen.has(date)) { seen.add(date); buckets.set(date, [...(buckets.get(date) ?? []), snapshot]) } }
    }
    return { contents: all.map((item) => item.content), selected: contentId ? scoped[0]?.content : undefined, totals: aggregate(scoped.flatMap((item) => item.within.slice(0, 1)) as unknown as Numeric[], metrics), series: periodDates(period.from, period.to, period.granularity).map((date) => ({ date, ...aggregate((buckets.get(date) ?? []) as unknown as Numeric[], metrics) })), availableDates: [...new Set(all.flatMap((item) => item.snapshots.map((snapshot) => shanghaiDay(snapshot.data_as_of_at))))].sort() }
  }
  const params = { from: period.from, to: addDays(period.to, 1) }
  const selectedId = contentId?.startsWith('seednote_post:') ? contentId.slice('seednote_post:'.length) : undefined
  const [overview, posts, detail, batches] = await Promise.all([
    api.seednoteImport.overview(project.id, params),
    allSeednotePosts(project.id),
    selectedId ? api.seednoteImport.post(project.id, selectedId, params) : Promise.resolve(undefined),
    allSeednoteBatches(project.id),
  ])
  const summaries = new Map((overview.post_summaries ?? []).map((item) => [item.id, item]))
  const identities = new Map([...posts, ...(overview.posts ?? []), ...(overview.post_summaries ?? []), ...(detail ? [detail.post] : [])].map((post) => [post.id, post]))
  const contents: AnalyticsContent[] = [...identities.values()].map((post) => ({ id: `seednote_post:${post.id}`, title: post.title, contentType: seednoteType(post.genre), date: summaries.get(post.id)?.data_as_of_at, url: post.note_url, metrics: values(summaries.get(post.id) as unknown as Numeric, metrics) }))
  const series = contentId ? detail ? dailyVersions(detail.versions).filter((point) => point.date >= period.from && point.date <= period.to) : [] : overview.series
  const totals = aggregate(series as unknown as Numeric[], metrics)
  const selected = contents.find((item) => item.id === contentId)
  if (selected && detail) selected.metrics = totals
  const availableDates = [...new Set([...(overview.dates ?? []), ...batches.filter((batch) => !batch.revoked_at && batch.status !== 'revoked' && batch.resolved_rows > 0).map((batch) => shanghaiDay(batch.data_as_of_at))])].sort()
  return { contents, selected, totals, series: bucketDailySeries(series.map((point) => ({ ...point })), period.granularity, metrics.map((item) => ({ key: item.key, aggregation: item.kind === 'count' ? 'sum' : 'mean' })), period.from, period.to), availableDates }
}
