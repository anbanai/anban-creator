import type { TaskStatus, PlanStatus, TaskType, CreditTransactionType } from '@/types'

// --- Label Maps ---

export const tierLabels: Record<string, string> = {
  free: '免费版',
  pro: '专业版',
  enterprise: '企业版',
}

export const tierDescriptions: Record<string, string> = {
  free: '最大 2 并发，API 限速 100 次/分钟',
  pro: '最大 5 并发，API 限速 300 次/分钟',
  enterprise: '最大 10 并发，API 限速 300 次/分钟',
}

export const taskStatusLabel: Record<TaskStatus, string> = {
  pending: '待执行',
  running: '运行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
}

export const planStatusLabel: Record<PlanStatus, string> = {
  active: '运行中',
  paused: '已暂停',
  completed: '已完成',
}

export const timelineItemTypeLabel: Record<string, string> = {
  task: '任务',
  plan: '计划',
}

export const transactionTypeLabel: Record<CreditTransactionType, string> = {
  sign_in: '签到',
  task_deduct: '任务消耗',
  task_refund: '任务退还',
  admin_grant: '管理员充值',
  image_gen: '图片生成',
  image_upload: '图片上传',
  article_write: '文章写作',
  convert: '格式转换',
  humanize: '文章润色',
  topic_research: '选题研究',
  seo: 'SEO优化',
  draft_publish: '草稿发布',
  outline: '大纲生成',
}

export const operationLabel: Record<string, string> = {
  image_gen: 'AI 生图',
  article_write: '文章写作',
  convert: '格式转换',
  humanize: '文章润色',
  topic_research: '选题研究',
  seo: 'SEO 优化',
  outline: '大纲生成',
}

export const taskTypeLabelCN: Record<string, string> = {
  article: '公众号文章',
  xls: '小绿书',
  rednote: '小红书',
}

// --- Single Source of Truth for Content Types ---

export const contentTypes = {
  rednote: { label: '小红书', platform: '小红书' },
  article: { label: '公众号文章', platform: '公众号' },
  xls: { label: '小绿书', platform: '小绿书' },
} as const satisfies Record<TaskType, { label: string; platform: string }>

export const contentTypeLabel = Object.fromEntries(
  Object.entries(contentTypes).map(([key, val]) => [key, val.label]),
) as Record<TaskType, string>

export const platformLabels = Object.fromEntries(
  Object.entries(contentTypes).map(([key, val]) => [key, val.platform]),
) as Record<TaskType, string>

export const platformDefaultRatio: Record<string, string> = {
  article: '16:9',
  rednote: '3:4',
  xls: '3:4',
}

export const platformRatioLabel: Record<string, string> = {
  article: '16:9（公众号默认）',
  rednote: '3:4（小红书默认）',
  xls: '3:4（小绿书默认）',
}

export const contentTypeOptions = Object.entries(contentTypes).map(([value, { label }]) => ({
  value,
  label,
}))

export const contentTypeFilterOptions = [
  { value: '', label: '全部内容' },
  ...contentTypeOptions,
]

// --- Option Arrays ---

export const timelineItemTypeOptions = [
  { value: '', label: '全部类型' },
  { value: 'task', label: '任务' },
  { value: 'plan', label: '计划' },
]

export const timelineStatusOptions = [
  { value: '', label: '全部状态' },
  { value: 'pending', label: '待执行' },
  { value: 'running', label: '运行中' },
  { value: 'completed', label: '已完成' },
  { value: 'failed', label: '失败' },
  { value: 'cancelled', label: '已取消' },
  { value: 'active', label: '运行中(计划)' },
  { value: 'paused', label: '已暂停(计划)' },
]

export const timelineSortOptions = [
  { value: 'date_asc', label: '日期 ↑' },
  { value: 'date_desc', label: '日期 ↓' },
  { value: 'status', label: '按状态' },
  { value: 'title', label: '按标题' },
]

// --- Badge Variants ---

export type BadgeVariant = 'success' | 'danger' | 'warning' | 'info' | 'neutral'

export function getBadgeVariant(status: string, type?: 'task' | 'plan'): BadgeVariant {
  if (type === 'plan' || status === 'active' || status === 'paused') {
    switch (status) {
      case 'active': return 'info'
      case 'paused': return 'warning'
      case 'completed': return 'success'
      default: return 'neutral'
    }
  }
  switch (status) {
    case 'completed': return 'success'
    case 'failed': return 'danger'
    case 'running': return 'warning'
    case 'cancelled': return 'neutral'
    default: return 'neutral'
  }
}

/** Convenience wrapper for task status badges */
export function statusBadgeVariant(status: string): BadgeVariant {
  return getBadgeVariant(status, 'task')
}

// --- Week Day Labels ---

export const weekDayLabels = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

// --- Cron to Human ---

export function cronToHuman(cron: string): string {
  const parts = cron.trim().split(/\s+/)
  if (parts.length < 5) return cron

  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts

  if (dayOfWeek !== '*' && month === '*' && dayOfMonth === '*') {
    const dayNum = parseInt(dayOfWeek, 10)
    const dayLabel = !isNaN(dayNum) && dayNum >= 0 && dayNum < weekDayLabels.length
      ? weekDayLabels[dayNum]
      : dayOfWeek
    return `每周${dayLabel} ${hour}:${minute.padStart(2, '0')}`
  }

  return `${hour}:${minute.padStart(2, '0')}`
}

// --- Date Formatting ---

export function formatDateTimeCN(dateStr: string): string {
  if (!dateStr) return '--'
  return new Date(dateStr).toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatFullDateTimeCN(dateStr: string): string {
  if (!dateStr) return '--'
  return new Date(dateStr).toLocaleString('zh-CN', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

export function formatDateLabelCN(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const diff = d.getTime() - today.getTime()
  if (diff === 0) return '今天'
  if (diff === 86400000) return '明天'
  if (diff === -86400000) return '昨天'
  return d.toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'short' })
}

export function formatMonthCN(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  return d.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long' })
}

export function formatTimeCN(dateStr: string): string {
  const d = new Date(dateStr)
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}

// --- Date Utilities ---

export function formatDateYMD(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

export function getWeekRange(today: Date): { from: string; to: string } {
  const day = today.getDay()
  const mondayOffset = day === 0 ? -6 : 1 - day
  const monday = new Date(today)
  monday.setDate(today.getDate() + mondayOffset)
  const sunday = new Date(monday)
  sunday.setDate(monday.getDate() + 6)
  return { from: formatDateYMD(monday), to: formatDateYMD(sunday) }
}

export function getMonthRange(today: Date): { from: string; to: string } {
  const first = new Date(today.getFullYear(), today.getMonth(), 1)
  const last = new Date(today.getFullYear(), today.getMonth() + 1, 0)
  return { from: formatDateYMD(first), to: formatDateYMD(last) }
}
