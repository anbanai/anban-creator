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

export const tierBenefits = [
  {
    key: 'free',
    name: '免费版',
    description: '适合轻量体验',
    creditMultiplier: '1.0x',
    platforms: ['Web 端'],
    concurrentTasks: 2,
    recommended: false,
  },
  {
    key: 'pro',
    name: '专业版',
    description: '适合稳定创作',
    creditMultiplier: '1.2x',
    platforms: ['Web 端', 'Claude Code', 'OpenClaw'],
    concurrentTasks: 5,
    recommended: true,
  },
  {
    key: 'enterprise',
    name: '企业版',
    description: '适合团队和深度运营',
    creditMultiplier: '1.5x',
    platforms: ['全部平台', '业务指导与辅助'],
    concurrentTasks: 10,
    recommended: false,
  },
] as const

type TierKey = typeof tierBenefits[number]['key']

export type MembershipComparisonValue = string | boolean

export interface MembershipComparisonRow {
  label: string
  values: Record<TierKey, MembershipComparisonValue>
}

export interface MembershipComparisonGroup {
  title: string
  rows: MembershipComparisonRow[]
}

export const membershipComparisonGroups: MembershipComparisonGroup[] = [
  {
    title: '基础权益',
    rows: [
      {
        label: '积分倍率',
        values: {
          free: '1.0x',
          pro: '1.2x',
          enterprise: '1.5x',
        },
      },
      {
        label: '并发任务',
        values: {
          free: '2 个',
          pro: '5 个',
          enterprise: '10 个',
        },
      },
    ],
  },
  {
    title: '创作能力',
    rows: [
      {
        label: '可用平台',
        values: {
          free: 'Web 端',
          pro: 'Web 端、Claude Code、OpenClaw',
          enterprise: '全部平台',
        },
      },
      {
        label: '继承权益',
        values: {
          free: '基础权益',
          pro: '包含免费版',
          enterprise: '包含专业版',
        },
      },
    ],
  },
  {
    title: '模型能力',
    rows: [
      {
        label: '自定义模型',
        values: {
          free: false,
          pro: '支持',
          enterprise: '包含专业版',
        },
      },
      {
        label: '高级模型',
        values: {
          free: false,
          pro: false,
          enterprise: '企业专享',
        },
      },
    ],
  },
  {
    title: '服务支持',
    rows: [
      {
        label: '业务指导与辅助',
        values: {
          free: false,
          pro: false,
          enterprise: true,
        },
      },
    ],
  },
]

export const taskStatusLabel: Record<string, string> = {
  pending: '待执行',
  running: '运行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
}

export const planStatusLabel: Record<string, string> = {
  active: '运行中',
  paused: '已暂停',
  completed: '已完成',
}

export const timelineItemTypeLabel: Record<string, string> = {
  task: '任务',
  plan: '计划',
}

export const transactionTypeLabel: Record<string, string> = {
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
  viral_analysis: '爆文拆解',
}

export const operationLabel: Record<string, string> = {
  image_gen: 'AI 生图',
  article_write: '文章写作',
  convert: '格式转换',
  humanize: '文章润色',
  topic_research: '选题研究',
  seo: 'SEO 优化',
  outline: '大纲生成',
  viral_analysis: '爆文拆解',
}

export const taskTypeLabelCN: Record<string, string> = {
  article: '公众号',
  seednote: '种草笔记',
  viral_analysis: '爆文拆解',
}

// --- Content Types ---

export const contentTypeLabel: Record<string, string> = {
  seednote: '种草笔记',
  article: '公众号文章',
}

export const contentTypeOptions = [
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号文章' },
]

export const platformLabels: Record<string, string> = {
  seednote: '种草笔记',
  article: '公众号',
}

export const platformDefaultRatio: Record<string, string> = {
  article: '16:9',
  seednote: '3:4',
}

export const platformRatioLabel: Record<string, string> = {
  article: '16:9（公众号默认）',
  seednote: '3:4（种草笔记默认）',
}

// --- Option Arrays ---

export const contentTypeFilterOptions = [
  { value: '', label: '全部内容' },
  ...contentTypeOptions,
]

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

export type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline' | 'ghost' | 'link'

export function getBadgeVariant(status: string, type?: 'task' | 'plan'): BadgeVariant {
  if (type === 'plan' || status === 'active' || status === 'paused') {
    switch (status) {
      case 'active': return 'default'
      case 'paused': return 'outline'
      case 'completed': return 'secondary'
      default: return 'secondary'
    }
  }
  switch (status) {
    case 'completed': return 'secondary'
    case 'failed': return 'destructive'
    case 'running': return 'outline'
    case 'cancelled': return 'secondary'
    default: return 'secondary'
  }
}

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
