import { useState } from 'react'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

const labels: Record<string, string> = {
  name: '账号名称', summary: '概述', description: '说明', details: '详情', positioning: '定位', direction: '内容方向', goals: '运营目标',
  differentiation: '差异化', tone: '表达调性', formats: '内容形式', audience: '目标受众',
  preferences: '内容偏好', exclusions: '不做内容', collaboration: '合作边界',
  compliance: '合规红线', primary: '主要平台 / 对象', topics: '关注主题',
  strengths: '优势与资源', resources: '可用素材资源', profile_url: '主页链接',
}

function fieldLabel(key: string) {
  return labels[key] ?? key.replace(/_/g, ' ').replace(/\b\w/g, (letter: string) => letter.toUpperCase())
}

/** Preserve structured values; every leaf uses an ordinary form control. */
function ValueField({ label, value, disabled, onChange }: { label: string; value: unknown; disabled: boolean; onChange: (next: unknown) => void }) {
  const [numberMode] = useState(() => typeof value === 'number')
  const [numberText, setNumberText] = useState(() => value == null ? '' : String(value))
  if (Array.isArray(value)) {
    if (value.every((item) => typeof item === 'string')) {
      return <label className="block space-y-1.5 text-sm"><span>{label}</span><Textarea aria-label={label} value={value.join('\n')} disabled={disabled} onChange={(event) => onChange(event.target.value.split('\n'))} rows={2} /><span className="text-xs text-muted-foreground">每行一项</span></label>
    }
    return <fieldset disabled={disabled} className="space-y-3 border-l-2 border-border pl-3"><legend className="mb-2 text-sm font-medium">{label}</legend>{value.map((item, index) => <ValueField key={index} label={`${label} · ${index + 1}`} value={item} disabled={disabled} onChange={(next) => onChange(value.map((entry, entryIndex) => entryIndex === index ? next : entry))} />)}</fieldset>
  }
  if (value && typeof value === 'object') {
    return <fieldset disabled={disabled} className="space-y-3 border-l-2 border-border pl-3"><legend className="mb-2 text-sm font-medium">{label}</legend><ProfileContentFields content={value as Record<string, unknown>} prefix={label.includes(' · ') ? label : undefined} disabled={disabled} onChange={onChange} /></fieldset>
  }
  if (typeof value === 'boolean') {
    return <label className="flex min-h-11 items-center gap-3 text-sm"><input type="checkbox" aria-label={label} checked={value} disabled={disabled} onChange={(event) => onChange(event.target.checked)} className="size-4 accent-primary" />{label}</label>
  }
  if (numberMode) {
    return <label className="block space-y-1.5 text-sm"><span>{label}</span><Input aria-label={label} type="text" inputMode="decimal" value={numberText} disabled={disabled} onChange={(event) => {
      const next = event.target.value
      setNumberText(next)
      if (!next) { onChange(null); return }
      if (/^-?(?:\d+\.?\d*|\.\d+)$/.test(next)) onChange(Number(next))
    }} onBlur={() => {
      if (numberText && !/^-?(?:\d+\.?\d*|\.\d+)$/.test(numberText)) setNumberText(value == null ? '' : String(value))
    }} /></label>
  }
  const text = value == null ? '' : String(value)
  return <label className="block space-y-1.5 text-sm"><span>{label}</span><Textarea aria-label={label} value={text} disabled={disabled} onChange={(event) => onChange(event.target.value)} rows={text.includes('\n') ? 3 : 2} /></label>
}

export function ProfileContentFields({ content, prefix, disabled = false, onChange }: { content: Record<string, unknown>; prefix?: string; disabled?: boolean; onChange: (next: Record<string, unknown>) => void }) {
  return <>{Object.entries(content).map(([field, value]) => {
    const label = fieldLabel(field)
    const path = prefix ? `${prefix} · ${label}` : label
    return <ValueField key={field} label={path} value={value} disabled={disabled} onChange={(next) => onChange({ ...content, [field]: next })} />
  })}</>
}
