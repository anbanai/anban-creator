import { useState } from 'react'
import type { ProfileDimension } from '@/types/project'
import { normalizeProfileDimension } from '@/lib/project-profile'
import { Button } from '@/components/common/button'
import { Textarea } from '@/components/ui/textarea'

const fieldLabels: Record<string, string> = {
  name: '名称', summary: '概述', description: '说明', positioning: '定位',
  project_name: '人设名称', brand: '品牌', business: '业务', role: '身份',
  audience: '目标读者', target_audience: '目标读者', goals: '目标',
  direction: '内容方向', differentiation: '特色与优势', value_proposition: '提供的价值',
  tone: '表达语气', voice: '表达风格', style: '风格', preferences: '偏好',
  formats: '内容形式', topics: '内容主题', exclusions: '不做什么',
  boundaries: '边界', compliance: '合规要求', collaboration: '合作方式',
  platform: '平台', account_name: '账号名', profile_url: '主页链接',
  facts: '已有事实', hypotheses: '待验证想法', notes: '补充说明',
  original_certainty: '信息来源性质', certainty: '确定程度',
}

function ContentValue({ value, label, editing, disabled, onChange }: {
  value: unknown; label: string; editing: boolean; disabled: boolean; onChange: (value: unknown) => void
}) {
  if (typeof value === 'string') return editing
    ? <Textarea aria-label={label} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)} className="min-h-20" />
    : <p className="whitespace-pre-wrap break-words text-sm leading-6">{value === 'stated' ? '来自本人表述' : value === 'inferred' ? '推断，待确认' : value || '尚未填写'}</p>
  if (Array.isArray(value)) return value.length ? <ul className="space-y-2 border-l pl-3">
    {value.map((item, index) => <li key={index}><ContentValue value={item} label={`${label} ${index + 1}`} editing={editing} disabled={disabled} onChange={(next) => onChange(value.map((previous, i) => i === index ? next : previous))} /></li>)}
  </ul> : <p className="text-sm text-muted-foreground">暂无记录</p>
  if (value && typeof value === 'object') return <div className="space-y-3">
    {Object.entries(value).map(([key, item]) => <div key={key} className="space-y-1">
      <p className="text-xs text-muted-foreground">{fieldLabels[key] ?? key}</p>
      <ContentValue value={item} label={`${label} · ${fieldLabels[key] ?? key}`} editing={editing} disabled={disabled} onChange={(next) => onChange({ ...value, [key]: next })} />
    </div>)}
  </div>
  // Preserve unknown values and their types. Structural edits remain available in advanced mode.
  return <p className="text-sm">{value === null ? '尚不确定' : typeof value === 'boolean' ? (value ? '是' : '否') : String(value)}</p>
}

export function ProfileDimensionCard({ label, dimension, text, error, disabled = false, onTextChange, onSave }: {
  label: string; dimension: ProfileDimension; text: string; error?: string; disabled?: boolean
  onTextChange: (text: string) => void; onSave?: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [advanced, setAdvanced] = useState(false)
  const safeDimension = normalizeProfileDimension(dimension)
  const { sources, missing_fields: missingFields, evidence, content } = safeDimension
  const hasSupplementalInfo = sources.length > 0 || missingFields.length > 0 || evidence.length > 0
  return <section aria-label={`${label}资料`} className="space-y-4 border-b py-6 first:pt-0 last:border-b-0 last:pb-0">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h3 className="text-sm font-semibold">{label}</h3>
      <Button size="sm" variant="ghost" disabled={disabled} onClick={() => setEditing(!editing)}>{editing ? `收起${label}编辑` : `修改${label}`}</Button>
    </div>
    {advanced ? <Textarea aria-label={`${label}画像内容`} value={text} disabled={disabled} onChange={(event) => onTextChange(event.target.value)} className="min-h-32 font-mono text-xs" />
      : Object.keys(content).length || editing ? <ContentValue value={Object.keys(content).length ? content : { summary: '' }} label={label} editing={editing} disabled={disabled} onChange={(value) => onTextChange(JSON.stringify(value, null, 2))} />
        : <p className="text-sm text-muted-foreground">还没有足够信息，可以稍后补充。</p>}
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    {hasSupplementalInfo && <details className="text-xs text-muted-foreground">
      <summary className="cursor-pointer">补充信息</summary>
      <div className="mt-3 space-y-3 border-l pl-3">
        {sources.length > 0 && <p>来源：{sources.join('、')}</p>}
        {missingFields.length > 0 && <p className="text-amber-700">待补充：{missingFields.join('、')}</p>}
        {evidence.length > 0 && <div><p className="mb-1">依据</p><ul className="space-y-1">{evidence.map((item, index) => <li key={index} className="whitespace-pre-wrap break-words">{item}</li>)}</ul></div>}
      </div>
    </details>}
    {editing && <div className="flex flex-wrap gap-2">
      {onSave && <Button size="sm" variant="secondary" disabled={disabled || Boolean(error)} onClick={onSave}>保存此维度</Button>}
      <Button size="sm" variant="ghost" disabled={disabled || Boolean(error)} onClick={() => setAdvanced(!advanced)}>{advanced ? '返回易读编辑' : '高级编辑（JSON）'}</Button>
    </div>}
  </section>
}
