import type { InteractiveDraft } from '@/lib/api/interactive'
import type { AssistantModeID } from '@/lib/api/assistant'

type DraftFields = Record<string, unknown>

const CREATION_FIELDS = [
  ['goal', '创作目标'], ['content_type', '内容类型'], ['target_audience', '目标受众'],
  ['angle', '主题与切入角度'], ['outline', '内容结构与大纲'], ['style_constraints', '风格与约束'],
  ['asset_plan', '素材与配图计划'], ['acceptance_criteria', '质量验收标准'],
  ['cost_estimate', '预计成本'], ['execution_options', '执行选项'],
] as const

const REVIEW_FIELDS = [
  ['summary', '复盘摘要'], ['dimension_recommendations', '六维画像改进建议'],
  ['evidence', '支持证据'], ['confidence', '结论可信度'], ['limitations', '局限与待验证事项'],
] as const

const DETAIL_LABELS: Record<string, string> = {
  identity: '定位', style: '风格', audience: '受众', platforms: '平台', preferences: '偏好与红线', memory: '记忆',
  reason: '建议原因', evidence: '支持证据', updates: '建议调整', source: '来源', metrics: '表现数据',
  title: '标题', summary: '摘要', description: '说明', content: '内容', opening: '开头', tone: '语气',
  structure: '结构', format: '格式', length: '篇幅', topics: '主题', rules: '规则', examples: '示例',
  positioning: '定位', target_audience: '目标受众', keywords: '关键词', constraints: '约束',
  likes: '点赞量', reads: '阅读量', views: '浏览量', comments: '评论量', shares: '分享量', saves: '收藏量',
  impressions: '曝光量', click_rate: '点击率', completion_rate: '完读率', engagement_rate: '互动率',
  value: '数值', baseline: '对照数据', observation: '观察', finding: '发现', recommendation: '建议',
  data_at: '数据时间', date: '日期', url: '参考链接', note: '说明', notes: '补充说明',
}

const CONTENT_TYPES: Record<string, string> = {
  article: '文章', wechat_article: '公众号文章', 'wechat-article': '公众号文章',
  seednote: '种草笔记', picture: '图文', wechat_picture: '公众号图文',
  montage: '视频剪辑', short_video: '短视频', live_slice: '直播切片',
}

function isRecord(value: unknown): value is DraftFields {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function assistantDraftFields(draft: InteractiveDraft | null, mode: AssistantModeID): DraftFields {
  const payload = draft?.payload
  if (payload) {
    const nested = payload[mode === 'creation' ? 'plan' : 'review']
    return isRecord(nested) ? nested : payload
  }
  return draft?.fields ?? {}
}

function labelForDetail(key: string): string {
  // Recommendation values are open-ended; unrecognized machine keys should
  // not become UI copy. Keep the proposed content visible for user review.
  return DETAIL_LABELS[key] ?? (/\p{Script=Han}/u.test(key) ? key : '补充内容')
}

function DraftValue({ value, label, field }: { value: unknown; label: string; field?: string }) {
  if (Array.isArray(value)) {
    return <ul aria-label={label} className="list-disc space-y-1 pl-4">{value.map((item, index) => <li key={index}><DraftValue value={item} label={label} /></li>)}</ul>
  }
  if (isRecord(value)) {
    return <dl className="space-y-3">{Object.entries(value).map(([key, item]) => <div key={key}><dt className="text-xs text-muted-foreground">{labelForDetail(key)}</dt><dd className="mt-1"><DraftValue value={item} label={labelForDetail(key)} field={key} /></dd></div>)}</dl>
  }
  let text = value == null || value === '' ? '待补充' : typeof value === 'boolean' ? value ? '是' : '否' : String(value)
  if (field === 'content_type') text = CONTENT_TYPES[text] ?? text
  if (field === 'confidence') text = ({ high: '较高', medium: '中等', low: '较低' } as Record<string, string>)[text] ?? text
  return <p className="whitespace-pre-wrap break-words">{text}</p>
}

export function AssistantDraftSummary({ draft, mode, compact = false }: { draft: InteractiveDraft | null; mode: Exclude<AssistantModeID, 'profile'>; compact?: boolean }) {
  const fields = assistantDraftFields(draft, mode)
  const definitions = mode === 'creation' ? CREATION_FIELDS : REVIEW_FIELDS
  const visible = definitions.filter(([key]) => fields[key] !== undefined)
  if (visible.length === 0) {
    return <p className="text-sm leading-6 text-muted-foreground">{mode === 'creation' ? '讨论后，创作目标、大纲和执行选项会整理在这里。' : '讨论后，复盘发现、改进建议和支持证据会整理在这里。'}</p>
  }
  return <dl className={compact ? 'space-y-3' : 'grid gap-3 sm:grid-cols-2'}>
    {visible.map(([key, label]) => <div key={key} className={compact ? 'border-t border-border pt-3' : `rounded-md border border-border bg-background/70 p-3 ${key === 'dimension_recommendations' || key === 'evidence' || key === 'outline' ? 'sm:col-span-2' : ''}`}>
      <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
      <dd className="mt-2 text-sm leading-6"><DraftValue value={fields[key]} label={label} field={key} /></dd>
    </div>)}
  </dl>
}
