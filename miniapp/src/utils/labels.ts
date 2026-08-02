import type { TaskStatus, PlanStatus, TaskType } from '@/types'

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

// 电商素材模块目录。模块数量影响交付规模，不参与任务创建时的固定 SKU 价格。
export interface EcommerceModuleDef {
  key: string
  label: string
  hint: string
  ratio: string
  defaultQty: number
  minQty: number
  maxQty: number
  qtyStep: number
  qtyLabel: string
}

export const ecommerceModuleCatalog: EcommerceModuleDef[] = [
  { key: 'main_images', label: '主图套', hint: '点击主图 + 细节/场景/对比/资质', ratio: '1:1', defaultQty: 5, minQty: 1, maxQty: 10, qtyStep: 1, qtyLabel: '张' },
  { key: 'detail_page', label: '详情页（商详）', hint: '黄金结构叙事节拍，移动优先', ratio: '3:4', defaultQty: 8, minQty: 1, maxQty: 20, qtyStep: 1, qtyLabel: '节' },
  { key: 'cover_banner', label: '封面 / 类目 banner', hint: '品牌氛围 + 类目信息', ratio: '16:9', defaultQty: 1, minQty: 1, maxQty: 5, qtyStep: 1, qtyLabel: '张' },
  { key: 'share_image', label: '分享图', hint: '社交钩子，站外引流', ratio: '1:1', defaultQty: 1, minQty: 1, maxQty: 5, qtyStep: 1, qtyLabel: '张' },
  { key: 'sku_images', label: 'SKU 变体图', hint: '同构不同色/款', ratio: '1:1', defaultQty: 1, minQty: 1, maxQty: 20, qtyStep: 1, qtyLabel: '张' },
]

// 投放平台 / 语言（value = server 机器值，label = 展示文案），与 studio 一致。
export const ecommerceTargetPlatformOptions = [
  { value: 'taobao', label: '淘宝 / 天猫' },
  { value: 'jd', label: '京东' },
  { value: 'douyin', label: '抖音电商' },
  { value: 'xhs', label: '小红书电商' },
  { value: 'general', label: '通用' },
]

export const ecommerceLanguageOptions = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: 'English' },
]

export const taskTypeLabelCN: Record<string, string> = {
  article: '公众号',
  seednote: '种草笔记',
  ecommerce: '电商出图',
  moments: '朋友圈',
}

export const contentTypes = {
  seednote: { label: '种草笔记', platform: '种草笔记' },
  article: { label: '公众号', platform: '公众号' },
  ecommerce: { label: '电商出图', platform: '电商出图' },
  moments: { label: '朋友圈', platform: '朋友圈' },
} as const

export const contentTypeLabel = Object.fromEntries(
  Object.entries(contentTypes).map(([key, val]) => [key, val.label]),
) as Record<string, string>

export const platformDefaultRatio: Record<string, string> = {
  article: '16:9',
  seednote: '3:4',
  ecommerce: '1:1',
  moments: '3:4',
}

export const contentTypeOptions = Object.entries(contentTypes).map(([value, { label }]) => ({
  value,
  label,
}))

// Pipeline stage → 中文标签. stage slugs come from server/service/task_progress_stages.go
// (article / seednote / ecommerce three sets). Used on the task detail progress card.
// Unknown stage falls back to the raw slug.
export const progressStageLabel: Record<string, string> = {
  // shared
  research: '选题研究',
  writing: '内容写作',
  // article-only
  outline: '大纲生成',
  humanize: '文章润色',
  seo: 'SEO 优化',
  cover: '封面生成',
  illustration: '插图生成',
  html: 'HTML 转换',
  draft: '草稿提交',
  // seednote-only
  project: '项目信息',
  viral_analysis: '爆文拆解',
  image_generation: '图片生成',
  compliance: '合规检查',
  archive: '资源归档',
  // ecommerce-only
  analysis: '产品档案',
  copywriting: '卖点与文案',
  finalize: '完成',
}

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
