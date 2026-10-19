import type { Project, ProjectProfile } from '@/types'
import { normalizeProjectProfile, PROJECT_PROFILE_DIMENSIONS } from './project-profile'

export const portraitLabels = { identity: '定位', audience: '受众', style: '表达风格', platforms: '平台', preferences: '偏好与红线', memory: '经验与素材' } as const
export const accountPlatformLabels: Record<string, string> = { wechat: '微信公众号', seednote: '小红书', douyin: '抖音', channels: '视频号', bilibili: '哔哩哔哩', weibo: '微博', zhihu: '知乎', kuaishou: '快手' }

// Provenance metadata alone is not a populated portrait dimension.
export function portraitText(value: unknown): string {
  if (typeof value === 'string') return value.trim()
  if (Array.isArray(value)) return value.map(portraitText).filter(Boolean).join('；')
  if (value && typeof value === 'object') return Object.entries(value).filter(([key]) => !['original_certainty', 'certainty', 'evidence', 'sources'].includes(key)).map(([, item]) => portraitText(item)).filter(Boolean).join('；')
  return ''
}

export function portraitProgress(raw: unknown) {
  const profile = normalizeProjectProfile(raw)
  const count = profile ? PROJECT_PROFILE_DIMENSIONS.filter(key => portraitText(profile.dimensions[key].content)).length : 0
  return { count, percent: Math.round(count / 6 * 100) }
}

export function portraitStatus(raw: ProjectProfile | undefined | null) {
  if (raw?.initialization_status === 'running' || raw?.initialization_status === 'queued') return '画像分析中'
  if (raw?.initialization_status === 'failed') return portraitProgress(raw).count ? '已保存 · 分析未完成' : '画像分析未完成'
  if (raw?.status === 'confirmed' || raw?.initialization_status === 'ready') return '画像已保存'
  return portraitProgress(raw).count ? '已保存 · 可继续完善' : '待补充画像'
}

export function portraitSummary(project: Project) {
  return portraitText(project.profile?.dimensions?.identity?.content) || project.instructions || project.positioning || '还没有定位，聊几句就能开始完善。'
}

export function updatePortraitHref(id: string) {
  return `/chat?${new URLSearchParams({ agent: 'project-creator', intent: 'update-project-profile', project_id: id, return_to: 'portraits' })}`
}
