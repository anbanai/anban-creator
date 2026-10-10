import { http, unwrap } from '@/lib/http-client'
import type { AnalyticsPoint } from '@/lib/analytics-period'

export type WechatAccountMetric = 'read_users' | 'share_users' | 'like_users' | 'collection_users' | 'comment_count' | 'follower_gain_count' | 'published_articles' | 'original_read_users'
export type WechatTrafficMetric = 'read_users'
export type WechatTrafficSource = 'all' | 'message' | 'recommendation' | 'chat' | 'moments' | 'official_account_home' | 'search' | 'other'

export interface WechatCoverage {
  covered_days: number
  expected_days: number
  complete: boolean
}

export interface WechatComparison {
  label: string
  value: number | null
  from?: string
  to?: string
}

export interface WechatAccountOverview {
  revision: number
  from: string
  to: string
  data_through?: string | null
  covered_dates: string[]
  coverage: WechatCoverage
  series: AnalyticsPoint[]
  totals: Record<string, number | null>
  comparisons?: Record<string, WechatComparison | null>
  unavailable_metrics?: Record<string, string>
}

export interface WechatTrafficSourceItem {
  source: Exclude<WechatTrafficSource, 'all'> | string
  label: string
  value: number | null
  share: number | null
}

export interface WechatTrafficSourcesResponse {
  revision: number
  from: string
  to: string
  metric: WechatTrafficMetric
  total: number | null
  coverage: WechatCoverage
  covered_dates: string[]
  data_quality?: string | null
  series: AnalyticsPoint[]
  items: WechatTrafficSourceItem[]
}

export interface WechatAccountRange { from: string; to: string }
export type WechatAccountOverviewParams = WechatAccountRange
export interface WechatTrafficSourcesParams extends WechatAccountRange {
  metric: WechatTrafficMetric
  source?: Exclude<WechatTrafficSource, 'all'>
}

const path = (projectId: string) => `/projects/${encodeURIComponent(projectId)}/wechat-analytics`

export const wechatAccountAnalyticsApi = {
  overview: (projectId: string, params: WechatAccountOverviewParams, signal?: AbortSignal) =>
    unwrap<WechatAccountOverview>(http.get(`${path(projectId)}/overview`, { params, signal })),
  trafficSources: (projectId: string, params: WechatTrafficSourcesParams, signal?: AbortSignal) =>
    unwrap<WechatTrafficSourcesResponse>(http.get(`${path(projectId)}/traffic-sources`, { params, signal })),
}

export const wechatAccountMetrics: ReadonlyArray<{ key: WechatAccountMetric; label: string; color: string; summary?: boolean; format: (value: number) => string }> = [
  { key: 'read_users', label: '阅读人数', color: 'var(--primary)', summary: true, format: value => value.toLocaleString('zh-CN') },
  { key: 'share_users', label: '分享人数', color: '#3b82f6', summary: true, format: value => value.toLocaleString('zh-CN') },
  { key: 'like_users', label: '点赞人数', color: '#f59e0b', summary: true, format: value => value.toLocaleString('zh-CN') },
  { key: 'collection_users', label: '收藏人数', color: '#8b5cf6', summary: true, format: value => value.toLocaleString('zh-CN') },
  { key: 'comment_count', label: '评论数', color: '#10b981', summary: true, format: value => value.toLocaleString('zh-CN') },
  { key: 'original_read_users', label: '跳转阅读原文', color: '#0ea5e9', summary: true, format: value => value.toLocaleString('zh-CN') },
  { key: 'follower_gain_count', label: '净增关注', color: '#ef4444', format: value => value.toLocaleString('zh-CN') },
  { key: 'published_articles', label: '发布篇数', color: '#64748b', format: value => value.toLocaleString('zh-CN') },
]

export const wechatTrafficMetrics: ReadonlyArray<{ key: WechatTrafficMetric; label: string; color: string; format: (value: number) => string }> = [
  { ...wechatAccountMetrics[0], key: 'read_users' },
]

export const wechatTrafficSources: ReadonlyArray<{ value: WechatTrafficSource; label: string }> = [
  { value: 'all', label: '全部来源' },
  { value: 'message', label: '公众号消息' },
  { value: 'recommendation', label: '推荐' },
  { value: 'chat', label: '聊天' },
  { value: 'moments', label: '朋友圈' },
  { value: 'official_account_home', label: '公众号主页' },
  { value: 'search', label: '搜一搜' },
  { value: 'other', label: '其他' },
]

export function formatWechatComparison(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  const percent = `${Math.abs(value * 100).toFixed(1)}%`
  return value > 0 ? `+${percent}` : value < 0 ? `-${percent}` : '0.0%'
}
