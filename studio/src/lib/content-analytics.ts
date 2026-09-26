import { http, unwrap } from '@/lib/http-client'
import { shanghaiDay, type AnalyticsPoint } from '@/lib/analytics-period'
import type { Project } from '@/types'
import type { AnalyticsCandidate, AnalyticsImportPayload, AnalyticsPreview, AnalyticsTarget } from '@/types/content-analytics'
export type AnalyticsPlatform = 'article' | 'seednote'
export type MetricBasis = 'cumulative' | 'daily'
export interface AnalyticsMetric { key: string; label: string; color: string; kind: 'count' | 'rate' | 'duration'; summary: boolean; format: (value: number) => string }
export interface AnalyticsContent { id: string; title: string; content_type: string; status?: string; url?: string; date?: string; last_stat_date?: string; metrics: Record<string, number | null> }
export interface AnalyticsParams { from: string; to: string; granularity: 'day' | 'week' | 'month'; metric_basis: MetricBasis; expected_revision?: number }
export interface AnalyticsOverview { revision: number; updated_at: string; metric_basis: MetricBasis; totals: Record<string, number | null>; series: AnalyticsPoint[]; coverage: { contents: number }; unavailable_metrics: Record<string, string> }
export interface AnalyticsDetail extends Omit<AnalyticsOverview, 'coverage'> { content: AnalyticsContent }
export interface AnalyticsPage<T> { revision: number; items: T[]; total: number; offset: number; limit: number }
export interface AnalyticsListParams extends AnalyticsParams { search?: string; content_type?: string; sort?: string; direction?: 'asc' | 'desc'; offset?: number; limit?: number }
export interface AnalyticsObservation { id: string; stat_date: string; source: string; metric_basis: MetricBasis; effective_at: string; received_at: string; revoked_at?: string; metrics: Record<string, number | null> }
const metric = (key: string, label: string, kind: AnalyticsMetric['kind'] = 'count', summary = true): AnalyticsMetric => ({ key, label, kind, summary, color: 'var(--primary)', format: (value) => kind === 'rate' ? `${(value * 100).toFixed(2)}%` : kind === 'duration' ? `${value.toFixed(1)} 秒` : value.toLocaleString('zh-CN') })
const wechatMetrics = [metric('read_users', '阅读人数'), metric('share_users', '分享人数'), metric('read_to_follow_users', '阅读后关注'), metric('delivered_users', '送达人数'), metric('read_completion_rate', '阅读完成率', 'rate', false)]
const seednoteMetrics = [metric('exposure_count', '曝光'), metric('view_count', '观看量'), metric('like_count', '点赞'), metric('comment_count', '评论'), metric('collect_count', '收藏'), metric('follower_gain_count', '涨粉'), metric('cover_click_rate', '封面点击率', 'rate', false), metric('share_count', '分享', 'count', false), metric('avg_watch_duration', '人均观看时长', 'duration', false), metric('barrage_count', '弹幕', 'count', false)]
export const metricsFor = (platform: string) => platform === 'article' ? wechatMetrics : seednoteMetrics
export const targetKey = (target: AnalyticsTarget) => `${target.kind}:${target.id}`
const targetKinds = ['task', 'wechat_publication', 'seednote_post'] as const satisfies readonly AnalyticsTarget['kind'][]
export function parseTargetKey(value?: string): AnalyticsTarget | undefined {
  const separator = value?.indexOf(':') ?? -1
  const kind = value?.slice(0, separator)
  const id = value?.slice(separator + 1)
  if (separator < 1 || !id || !targetKinds.includes(kind as AnalyticsTarget['kind'])) return undefined
  return { kind: kind as AnalyticsTarget['kind'], id }
}
/** Single source of truth for the `/content-data` deep-link contract: `?account=<project>&content=<target>`. */
function contentDataHref(projectId: string, target: AnalyticsTarget) {
  return `/content-data?${new URLSearchParams({ account: projectId, content: targetKey(target) })}`
}
export function taskContentDataHref(projectId: string, taskId: string) { return contentDataHref(projectId, { kind: 'task', id: taskId }) }
export const contentTypeLabel = (type: string) => ({ article: '文章', image: '贴图', image_text: '图文', '图文': '图文', '视频': '视频', video: '视频', unknown: '类型未知' }[type] ?? '类型未知')
export function descriptionFor(_platform: string, _selected: boolean, basis: MetricBasis = 'cumulative') { return basis === 'cumulative' ? '每篇内容取范围内最新累计记录，不代表当日新增；缺失记录不补零。' : '仅统计明确标记为当日新增的数据；计数按日求和，缺失记录不补零。' }
const path = (id: string) => `/projects/${encodeURIComponent(id)}/content-analytics`
const nativePath = (project: Project) => `/projects/${project.id}/${project.platform === 'article' ? 'wechat' : 'seednote'}-analytics`
export const contentAnalyticsApi = {
  overview: (id: string, params: AnalyticsParams, signal?: AbortSignal) => unwrap<AnalyticsOverview>(http.get(`${path(id)}/overview`, { params, signal })),
  contents: (id: string, params: AnalyticsListParams, signal?: AbortSignal) => unwrap<AnalyticsPage<AnalyticsContent>>(http.get(`${path(id)}/contents`, { params, signal })),
  detail: (id: string, contentId: string, params: AnalyticsParams, signal?: AbortSignal) => unwrap<AnalyticsDetail>(http.get(`${path(id)}/contents/${encodeURIComponent(contentId)}`, { params, signal })),
  observations: (id: string, contentId: string, params: AnalyticsParams & { offset: number; limit: number }, signal?: AbortSignal) => unwrap<AnalyticsPage<AnalyticsObservation>>(http.get(`${path(id)}/contents/${encodeURIComponent(contentId)}/observations`, { params, signal })),
  dates: (id: string, params: { year: number; metric_basis: MetricBasis; expected_revision?: number }, signal?: AbortSignal) => unwrap<{ revision: number; dates: string[] }>(http.get(`${path(id)}/dates`, { params, signal })),
  candidates: (projectId: string, params: { search?: string; offset?: number; limit?: number }, signal?: AbortSignal) => unwrap<{ items: AnalyticsCandidate[]; total: number }>(http.get(`${path(projectId)}/candidates`, { params, signal })),
  preview: (project: Project, uploadId: string) => unwrap<AnalyticsPreview>(http.post(`${nativePath(project)}/imports/preview`, { upload_id: uploadId, timezone: 'Asia/Shanghai' })),
  import: async (project: Project, payload: AnalyticsImportPayload) => {
    const result = await unwrap<{ revision: number; matched_rows?: number; batch?: { resolved_rows: number } }>(http.post(`${nativePath(project)}/imports`, payload))
    return { revision: result.revision, count: result.batch?.resolved_rows ?? result.matched_rows ?? 0, date: shanghaiDay(payload.data_as_of_at) }
  },
}
export function isAnalyticsRevisionConflict(error: unknown) { return (error as { response?: { status?: number } } | null)?.response?.status === 409 }
