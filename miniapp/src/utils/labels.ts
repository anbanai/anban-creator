import type { TaskStatus, PlanStatus, TaskType, CreditTransactionType } from '@/types'

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

export const taskTypeLabelCN: Record<string, string> = {
  article: '公众号',
  seednote: '种草笔记',
}

export const contentTypes = {
  seednote: { label: '种草笔记', platform: '种草笔记' },
  article: { label: '公众号', platform: '公众号' },
} as const

export const contentTypeLabel = Object.fromEntries(
  Object.entries(contentTypes).map(([key, val]) => [key, val.label]),
) as Record<string, string>

export const platformDefaultRatio: Record<string, string> = {
  article: '16:9',
  seednote: '3:4',
}

export const contentTypeOptions = Object.entries(contentTypes).map(([value, { label }]) => ({
  value,
  label,
}))

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

export const weekDayLabels = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

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
