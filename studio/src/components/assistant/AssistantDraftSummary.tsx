import type { InteractiveDraft } from '@/lib/api/interactive'
import type { AssistantModeID } from '@/lib/api/assistant'
import { platformDisplayName } from '@/lib/labels'
import { normalizeProjectProfile, PROJECT_PROFILE_DIMENSIONS } from '@/lib/project-profile'

type DraftFields = Record<string, unknown>

const PROFILE_FIELDS = [
  ['name', '人设名称'], ['platform', '创作平台'], ['description', '人设简介'], ['instructions', '创作要求'],
  ['identity', '身份与定位'], ['style', '表达风格'], ['audience', '目标受众'],
  ['platforms', '平台策略'], ['preferences', '偏好与红线'], ['memory', '长期记忆'],
  ['analysis_limits', '局限与待验证事项'], ['follow_up_questions', '待补充问题'],
] as const

const CREATION_FIELDS = [
  ['goal', '创作目标'], ['content_type', '内容类型'], ['topic', '创作话题'], ['angle', '切入角度'], ['expression_form', '表达形式'], ['target_audience', '目标受众'],
  ['profile_context', '沿用的人设信息'], ['temporary_overrides', '本次临时调整'],
  ['outline', '内容结构与大纲'], ['style_constraints', '风格与约束'],
  ['asset_plan', '素材与配图计划'], ['acceptance_criteria', '质量验收标准'],
  ['cost_estimate', '预计成本'], ['execution_options', '执行选项'], ['execution_profile', '执行配置'],
  ['image_ratio', '图片比例'], ['image_capability_key', '图像能力'],
] as const

const REVIEW_FIELDS = [
  ['summary', '复盘摘要'], ['dimension_recommendations', '六维人设改进建议'],
  ['evidence', '支持证据'], ['confidence', '结论可信度'], ['limitations', '局限与待验证事项'],
] as const

const SUMMARY_GROUPS: Record<AssistantModeID, { title: string; keys: string[]; secondary?: boolean }[]> = {
  profile: [
    { title: '人设要点', keys: ['identity', 'style', 'audience', 'platforms', 'preferences', 'memory'] },
    { title: '创作约定', keys: ['description', 'instructions'] },
    { title: '仍需了解', keys: ['analysis_limits', 'follow_up_questions'], secondary: true },
  ],
  creation: [
    { title: '内容方向', keys: ['topic', 'angle', 'expression_form', 'target_audience'] },
    { title: '内容安排', keys: ['outline', 'style_constraints', 'asset_plan', 'acceptance_criteria'] },
    { title: '人设与本次调整', keys: ['profile_context', 'temporary_overrides'] },
    { title: '执行设置', keys: ['cost_estimate', 'execution_options', 'execution_profile', 'image_ratio', 'image_capability_key'], secondary: true },
  ],
  review: [
    { title: '改进建议', keys: ['dimension_recommendations'] },
    { title: '判断依据', keys: ['evidence', 'confidence', 'limitations'], secondary: true },
  ],
}

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
  montage: '视频剪辑', 'video-edit': '全自动视频剪辑', video_edit: '全自动视频剪辑', short_video: '短视频', live_slice: '直播切片',
}

const EXECUTION_PROFILES: Record<string, string> = {
  effective: '性价比', balanced: '平衡', quality: '极致效果',
}

const IMAGE_CAPABILITIES: Record<string, string> = {
  standard: '标准', enhanced: '增强', professional: '专业',
}

function isRecord(value: unknown): value is DraftFields {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function assistantDraftFields(draft: InteractiveDraft | null, mode: AssistantModeID): DraftFields {
  const payload = draft?.payload
  if (mode === 'profile') {
    const source = payload ?? draft
    const project = isRecord(source?.project) ? source.project : {}
    const profile = normalizeProjectProfile(source?.profile)
    return {
      name: project.name, platform: project.platform, description: project.description, instructions: project.instructions,
      ...Object.fromEntries(PROJECT_PROFILE_DIMENSIONS.flatMap((name) => {
        const content = profile?.dimensions[name].content
        const entries = Object.entries(content ?? {})
        return entries.length ? [[name, entries.length === 1 ? entries[0][1] : content]] : []
      })),
      ...(profile?.analysis_limits.length ? { analysis_limits: profile.analysis_limits } : {}),
      ...(profile?.follow_up_questions.length ? { follow_up_questions: profile.follow_up_questions } : {}),
    }
  }
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
    if (value.length === 0) {
      const emptyText = field === 'profile_context' ? '无额外沿用信息' : field === 'temporary_overrides' ? '无临时调整' : '暂无'
      return <p className="text-muted-foreground">{emptyText}</p>
    }
    return <ul aria-label={label} className="list-disc space-y-1 pl-4">{value.map((item, index) => <li key={index}><DraftValue value={item} label={label} /></li>)}</ul>
  }
  if (isRecord(value)) {
    return <dl className="space-y-3">{Object.entries(value).map(([key, item]) => <div key={key}><dt className="text-xs text-muted-foreground">{labelForDetail(key)}</dt><dd className="mt-1"><DraftValue value={item} label={labelForDetail(key)} field={key} /></dd></div>)}</dl>
  }
  let text = value == null || value === ''
    ? field === 'profile_context' || field === 'temporary_overrides' ? '未记录' : '待补充'
    : typeof value === 'boolean' ? value ? '是' : '否' : String(value)
  if (field === 'content_type') text = CONTENT_TYPES[text] ?? text
  if (field === 'platform') text = platformDisplayName(text)
  if (field === 'execution_profile') text = EXECUTION_PROFILES[text] ?? text
  if (field === 'image_ratio' && text === 'auto') text = '智能适配'
  if (field === 'image_capability_key') text = IMAGE_CAPABILITIES[text] ?? text
  if (field === 'confidence') text = ({ high: '较高', medium: '中等', low: '较低' } as Record<string, string>)[text] ?? text
  return <p className="whitespace-pre-wrap break-words">{text}</p>
}

export function AssistantDraftSummary({ draft, mode, compact = false }: { draft: InteractiveDraft | null; mode: AssistantModeID; compact?: boolean }) {
  const fields = assistantDraftFields(draft, mode)
  const definitions = mode === 'profile' ? PROFILE_FIELDS : mode === 'creation' ? CREATION_FIELDS : REVIEW_FIELDS
  const hasCreationPlan = mode === 'creation' && Object.keys(fields).length > 0
  const visible = definitions.filter(([key]) => fields[key] !== undefined || (hasCreationPlan && (key === 'profile_context' || key === 'temporary_overrides')))
  if (visible.length === 0) {
    return <p className="text-sm leading-6 text-muted-foreground">{mode === 'profile' ? '讨论后，人设定位、表达风格和目标受众会整理在这里。' : mode === 'creation' ? '讨论后，创作目标、话题、角度和表达形式会整理在这里。' : '讨论后，复盘发现、改进建议和支持证据会整理在这里。'}</p>
  }
  const headlineKey = mode === 'profile' ? 'name' : mode === 'creation' ? 'goal' : 'summary'
  const headline = visible.find(([key]) => key === headlineKey)
  const metaKey = mode === 'profile' ? 'platform' : mode === 'creation' ? 'content_type' : undefined
  const meta = visible.find(([key]) => key === metaKey)
  return <div className={compact ? '@container space-y-6' : '@container space-y-8'}>
    {(headline || meta) && <div className="rounded-xl bg-muted/40 px-4 py-4 sm:px-5">
      {headline && <>
        <p className="mb-2 text-xs font-medium text-muted-foreground">{headline[1]}</p>
        <div className="text-lg font-medium leading-8 tracking-tight"><DraftValue value={fields[headlineKey]} label={headline[1]} field={headlineKey} /></div>
      </>}
      {meta && <div className={`${headline ? 'mt-3 ' : ''}inline-flex rounded-md border border-border/70 bg-background/80 px-2 py-0.5 text-xs text-muted-foreground`}><DraftValue value={fields[meta[0]]} label={meta[1]} field={meta[0]} /></div>}
    </div>}
    {SUMMARY_GROUPS[mode].map((group) => {
      const entries = visible.filter(([key]) => group.keys.includes(key))
      if (!entries.length) return null
      return <section key={group.title} aria-label={group.title} className={group.secondary ? 'rounded-xl bg-muted/30 p-4 sm:p-5' : ''}>
        <h3 className="mb-4 text-sm font-semibold">{group.title}</h3>
        <dl className="grid min-w-0 gap-x-8 gap-y-5 @min-[32rem]:grid-cols-2">
          {entries.map(([key, label]) => <div key={key} className={`min-w-0 ${['dimension_recommendations', 'evidence', 'outline', 'asset_plan', 'analysis_limits', 'follow_up_questions'].includes(key) ? '@min-[32rem]:col-span-2' : ''}`}>
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="mt-1.5 text-sm leading-6"><DraftValue value={fields[key]} label={label} field={key} /></dd>
          </div>)}
        </dl>
      </section>
    })}
  </div>
}
