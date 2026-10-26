import { http, unwrap } from '@/lib/http-client'
import type { AnalyticsPoint } from '@/lib/analytics-period'

export type WechatAccountMetric = 'read_users' | 'share_users' | 'original_read_users' | 'like_users' | 'collection_users' | 'comment_count' | 'follower_gain_count' | 'follower_cancel_count' | 'follower_net_gain_count' | 'published_articles'
export type WechatTrafficSource = 'all' | 'message' | 'recommendation' | 'chat' | 'moments' | 'official_account_home' | 'search' | 'business_card' | 'qr_code' | 'article_account' | 'wechat_ads' | 'reprint' | 'mini_program' | 'channels' | 'live_stream' | 'other'

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

export interface WechatComparisonSet {
  previous_day?: WechatComparison | null
  previous_period?: WechatComparison | null
  previous_week?: WechatComparison | null
}

export interface WechatAccountOverview {
  revision: number
  from: string
  to: string
  data_through?: string | null
  covered_dates: string[]
  coverage: WechatCoverage
  metric_coverage?: Record<string, WechatCoverage>
  series: AnalyticsPoint[]
  totals: Record<string, number | null>
  comparisons?: Record<string, WechatComparisonSet | null>
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
  metric: WechatAccountMetric
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
  metric: WechatAccountMetric
  source?: Exclude<WechatTrafficSource, 'all'>
}
export interface WechatAccountAnalyticsSyncResult {
  revision: number
  batch: { id: string; status: string; source: string; data_as_of_at?: string }
  from: string
  to: string
  account_days: number
}

const path = (projectId: string) => `/projects/${encodeURIComponent(projectId)}/wechat-analytics`

export const wechatAccountAnalyticsApi = {
  overview: (projectId: string, params: WechatAccountOverviewParams, signal?: AbortSignal) =>
    unwrap<WechatAccountOverview>(http.get(`${path(projectId)}/overview`, { params, signal })),
  trafficSources: (projectId: string, params: WechatTrafficSourcesParams, signal?: AbortSignal) =>
    unwrap<WechatTrafficSourcesResponse>(http.get(`${path(projectId)}/traffic-sources`, { params, signal })),
  sync: (projectId: string, params: WechatAccountRange) =>
    unwrap<WechatAccountAnalyticsSyncResult>(http.post(`${path(projectId)}/sync`, params)),
}

export const wechatAccountMetrics: ReadonlyArray<{ key: WechatAccountMetric; label: string; color: string; summary?: boolean; format: (value: number) => string }> = [
  { key: 'follower_gain_count', label: '新增关注', color: '#ef4444', format: value => value.toLocaleString('zh-CN') },
  { key: 'follower_cancel_count', label: '取消关注', color: '#64748b', format: value => value.toLocaleString('zh-CN') },
  { key: 'follower_net_gain_count', label: '净增关注', color: '#14b8a6', format: value => value.toLocaleString('zh-CN') },
  { key: 'read_users', label: '阅读人数', color: 'var(--primary)', format: value => value.toLocaleString('zh-CN') },
  { key: 'share_users', label: '分享人数', color: '#3b82f6', format: value => value.toLocaleString('zh-CN') },
  { key: 'original_read_users', label: '跳转阅读原文', color: '#0ea5e9', format: value => value.toLocaleString('zh-CN') },
  { key: 'like_users', label: '点赞人数', color: '#f59e0b', format: value => value.toLocaleString('zh-CN') },
  { key: 'collection_users', label: '收藏人数', color: '#8b5cf6', format: value => value.toLocaleString('zh-CN') },
  { key: 'comment_count', label: '评论数', color: '#10b981', format: value => value.toLocaleString('zh-CN') },
  { key: 'published_articles', label: '发布篇数', color: '#64748b', format: value => value.toLocaleString('zh-CN') },
]

export const wechatTrafficSources: ReadonlyArray<{ value: WechatTrafficSource; label: string }> = [
  { value: 'all', label: '全部来源' },
  { value: 'message', label: '公众号消息' },
  { value: 'recommendation', label: '推荐' },
  { value: 'chat', label: '聊天' },
  { value: 'moments', label: '朋友圈' },
  { value: 'official_account_home', label: '公众号主页' },
  { value: 'search', label: '搜一搜' },
  { value: 'business_card', label: '名片分享' },
  { value: 'qr_code', label: '扫描二维码' },
  { value: 'article_account', label: '文章内账号名称' },
  { value: 'wechat_ads', label: '微信广告' },
  { value: 'reprint', label: '他人转载' },
  { value: 'mini_program', label: '小程序关注' },
  { value: 'channels', label: '视频号' },
  { value: 'live_stream', label: '直播' },
  { value: 'other', label: '其他' },
]

export const wechatTrafficMetrics: ReadonlyArray<{ key: WechatAccountMetric; label: string }> = [
  { key: 'read_users', label: '阅读人数' },
  { key: 'follower_gain_count', label: '新增关注' },
  { key: 'follower_cancel_count', label: '取消关注' },
]

export function formatWechatComparison(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  const percent = `${Math.abs(value * 100).toFixed(1)}%`
  return value > 0 ? `+${percent}` : value < 0 ? `-${percent}` : '0.0%'
}
