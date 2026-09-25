import type { Task } from '@/types'

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

export const taskTypeLabelCN: Record<string, string> = {
  article: '公众号',
  seednote: '种草笔记',
  moments: '朋友圈',
  ecommerce: '电商出图',
  viral_analysis: '爆文拆解',
  montage: '视频生成',
  hypit: '视频复刻',
}

// --- Content Types ---

// 电商平台类型统一用「电商出图」，与 contentTypeOptions / platformLabels /
// taskTypeLabelCN / TemplateCard 等一致。「电商素材包/模块」指的是交付物（按模块
// 计费的素材包），是另一个语义，保持不变。
export const contentTypeLabel: Record<string, string> = {
  seednote: '种草笔记',
  article: '公众号文章',
  moments: '朋友圈',
  ecommerce: '电商出图',
  viral_analysis: '爆文拆解',
  montage: '视频生成',
  hypit: '视频复刻',
}

// Pipeline stage → 中文标签。stage 取值来自 server/service/task_progress_stages.go
// （article / seednote 两套），用于任务详情页进度卡片的 Badge 文案。
// 未知 stage 回退到原 slug。
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
  material_analysis: '素材分析',
  viral_analysis: '爆文拆解',
  image_generation: '图片生成',
  quality_review: '质量复核',
  compliance: '合规检查',
  archive: '资源归档',
  // ecommerce-only (slugs from harness/agents/ecommerce.md)
  analysis: '产品档案',
  copywriting: '卖点与文案',
  finalize: '完成',
}

export const taskErrorLabels: Record<string, { title: string; message: string; recovery?: string }> = {
  completion_report_failed: { title: '结果提交失败（可恢复）', message: '任务结果提交失败，但已有产物已保留。' },
  execution_identity_unavailable: { title: '执行环境未建立', message: '执行环境未建立，暂时无法生成或结算图片。', recovery: '修复执行环境后可从“图片生成”阶段继续。' },
  execution_identity_required: { title: '执行环境未建立', message: '执行环境未建立，暂时无法生成或结算图片。', recovery: '修复执行环境后可从“图片生成”阶段继续。' },
  execution_identity_mismatch: { title: '执行身份不匹配', message: '当前执行身份与任务不匹配，请重新启动任务。' },
}

const taskErrorMessageCodes: Record<string, string> = {
  '执行环境未建立，暂时无法生成或结算图片': 'execution_identity_unavailable',
  '执行环境未建立，暂时无法生成或结算图片。': 'execution_identity_unavailable',
}

export function taskFailurePresentation(task: Pick<Task, 'error_message'> & Partial<Pick<Task, 'outcome'>>): { code?: string; title: string; message: string; recovery?: string; raw?: string } | null {
  const diagnostic = task.outcome?.diagnostic
  if (diagnostic?.code === 'artifact_upload_failed' || diagnostic?.code === 'artifact_manifest_failed') {
    return {
      code: diagnostic.code,
      title: diagnostic.code === 'artifact_upload_failed' ? '产物上传失败' : '产物清单提交失败',
      message: diagnostic.summary,
      recovery: '已保留文件可查看或下载；继续执行会复用可用上下文。',
    }
  }
  const raw = task.error_message?.trim()
  if (!raw) return null
  let code: string | undefined = taskErrorLabels[raw] ? raw : taskErrorMessageCodes[raw]
  try {
    const parsed = JSON.parse(raw) as { code?: string; error_code?: string }
    code = parsed.error_code || parsed.code
  } catch { /* plain server message */ }
  const mapped = code ? taskErrorLabels[code] : undefined
  return mapped ? { code, ...mapped, raw } : { message: raw, title: '执行失败', raw }
}

export const contentTypeOptions = [
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号文章' },
  { value: 'moments', label: '朋友圈' },
  { value: 'montage', label: '视频生成' },
  { value: 'hypit', label: '视频复刻' },
  { value: 'ecommerce', label: '电商出图' },
]

export const platformLabels: Record<string, string> = {
  seednote: '种草笔记',
  article: '公众号',
  moments: '朋友圈',
  ecommerce: '电商出图',
  montage: '视频生成',
  hypit: '视频复刻',
}

export const platformDefaultRatio: Record<string, string> = {
  article: '16:9',
  seednote: '3:4',
  moments: '3:4',
  ecommerce: '1:1',
  montage: '9:16',
  hypit: '9:16',
}

export const platformRatioLabel: Record<string, string> = {
  article: '16:9（公众号默认）',
  seednote: '3:4（种草笔记默认）',
  moments: '3:4（朋友圈默认）',
  ecommerce: '1:1（电商主图默认）',
}

// --- E-commerce module catalog ---
// Keys match server credits.ecommerce_module_prices + model.EcommerceConfig.SelectedModules.
// `defaultQty` is the suggested quantity when the module is toggled on; pricing
// data is shown only as delivery-scale guidance, not the creation-time charge.

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

// Target sales platform — informs size/compliance spec injected into the agent.
export const ecommerceTargetPlatformOptions = [
  { value: 'taobao', label: '淘宝 / 天猫' },
  { value: 'jd', label: '京东' },
  { value: 'douyin', label: '抖音电商' },
  { value: 'xhs', label: '种草笔记电商' },
  { value: 'general', label: '通用' },
]

export const ecommerceLanguageOptions = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: 'English' },
]

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
