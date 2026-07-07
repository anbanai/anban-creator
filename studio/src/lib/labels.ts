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
    platforms: ['Web 端', 'Claude Code', 'OpenClaw', 'Codex'],
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

export type TierKey = typeof tierBenefits[number]['key']

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
          pro: 'Web 端、Claude Code、OpenClaw、Codex',
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

export interface ModelCategory {
  key: 'basic' | 'advanced' | 'custom'
  label: string
  description: string
  available: Record<TierKey, boolean>
}

export const modelCategories: ModelCategory[] = [
  {
    key: 'basic',
    label: '基础模型',
    description: '日常任务的高性价比选择',
    available: { free: true, pro: true, enterprise: true },
  },
  {
    key: 'advanced',
    label: '高级模型',
    description: '顶级模型，长文与复杂任务质量更佳',
    available: { free: false, pro: false, enterprise: true },
  },
  {
    key: 'custom',
    label: '自定义模型 (BYOK)',
    description: '绑定自己的 API Key，全部操作免费',
    available: { free: false, pro: true, enterprise: true },
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
  task_deduct: '积分消耗',
  task_refund: '任务退还',
  admin_grant: '管理员充值',
  register_bonus: '注册奖励',
  invite_reward: '邀请奖励',
  agent_runtime_reserve: '运行预留',
  agent_runtime: 'Claude运行',
  agent_runtime_refund: '运行退还',
  image_gen: '图片生成',
  image_understanding: '图片理解',
  image_upload: '图片上传',
  article_write: '文章写作',
  convert: '格式转换',
  humanize: '文章润色',
  topic_research: '选题研究',
  seo: 'SEO优化',
  draft_publish: '草稿发布',
  outline: '大纲生成',
  viral_analysis: '爆文拆解',
  video_gen: '视频生成',
  video_understanding: '视频理解',
  poster_generation: '海报生成',
}

export const operationLabel: Record<string, string> = {
  image_gen: 'AI 生图',
  image_understanding: '图片理解',
  article_write: '文章写作',
  convert: '格式转换',
  humanize: '文章润色',
  topic_research: '选题研究',
  seo: 'SEO 优化',
  outline: '大纲生成',
  viral_analysis: '爆文拆解',
  video_gen: '视频生成',
  video_understanding: '视频理解',
  poster_generation: '海报生成',
}

export const taskTypeLabelCN: Record<string, string> = {
  article: '公众号',
  seednote: '种草笔记',
  ecommerce: '电商出图',
  viral_analysis: '爆文拆解',
  video: '视频生成',
}

// --- Content Types ---

// 电商平台类型统一用「电商出图」，与 contentTypeOptions / platformLabels /
// taskTypeLabelCN / TemplateCard 等一致。「电商素材包/模块」指的是交付物（按模块
// 计费的素材包），是另一个语义，保持不变。
export const contentTypeLabel: Record<string, string> = {
  seednote: '种草笔记',
  article: '公众号文章',
  ecommerce: '电商出图',
  video: '视频生成',
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
  viral_analysis: '爆文拆解',
  image_generation: '图片生成',
  compliance: '合规检查',
  archive: '资源归档',
  // ecommerce-only (slugs from claudecode/agents/ecommerce.md)
  analysis: '产品档案',
  copywriting: '卖点与文案',
  finalize: '完成',
}

export const contentTypeOptions = [
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号文章' },
  { value: 'video', label: '视频生成' },
  { value: 'ecommerce', label: '电商出图' },
]

export const platformLabels: Record<string, string> = {
  seednote: '种草笔记',
  article: '公众号',
  ecommerce: '电商出图',
}

export const platformDefaultRatio: Record<string, string> = {
  article: '16:9',
  seednote: '3:4',
  ecommerce: '1:1',
}

export const platformRatioLabel: Record<string, string> = {
  article: '16:9（公众号默认）',
  seednote: '3:4（种草笔记默认）',
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
  { value: 'xhs', label: '小红书电商' },
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
