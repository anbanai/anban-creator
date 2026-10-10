import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

const labels: Record<string, string> = {
  name: '账号名称', positioning: '定位', direction: '内容方向', goals: '运营目标',
  differentiation: '差异化', tone: '表达调性', formats: '内容形式', audience: '目标受众',
  preferences: '内容偏好', exclusions: '不做内容', collaboration: '合作边界',
  compliance: '合规红线', primary: '主要平台 / 对象', topics: '关注主题',
  strengths: '优势与资源', resources: '可用素材资源', profile_url: '主页链接',
}

function fieldLabel(key: string) {
  return labels[key] ?? key.replace(/_/g, ' ').replace(/\b\w/g, (letter: string) => letter.toUpperCase())
}

/** Preserve structured values; every leaf uses an ordinary form control. */
function ValueField({ label, value, onChange }: { label: string; value: unknown; onChange: (next: unknown) => void }) {
  if (Array.isArray(value)) {
    if (value.every((item) => typeof item === 'string')) {
      return <label className="block space-y-1.5 text-sm"><span>{label}</span><Textarea aria-label={label} value={value.join('\n')} onChange={(event) => onChange(event.target.value.split('\n'))} rows={2} /><span className="text-xs text-muted-foreground">每行一项</span></label>
    }
    return <fieldset className="space-y-3 border-l-2 border-border pl-3"><legend className="mb-2 text-sm font-medium">{label}</legend>{value.map((item, index) => <ValueField key={index} label={`${label} · ${index + 1}`} value={item} onChange={(next) => onChange(value.map((entry, entryIndex) => entryIndex === index ? next : entry))} />)}</fieldset>
  }
  if (value && typeof value === 'object') {
    return <fieldset className="space-y-3 border-l-2 border-border pl-3"><legend className="mb-2 text-sm font-medium">{label}</legend><ProfileContentFields content={value as Record<string, unknown>} onChange={onChange} /></fieldset>
  }
  if (typeof value === 'boolean') {
    return <label className="flex min-h-11 items-center gap-3 text-sm"><input type="checkbox" aria-label={label} checked={value} onChange={(event) => onChange(event.target.checked)} className="size-4 accent-primary" />{label}</label>
  }
  if (typeof value === 'number') {
    return <label className="block space-y-1.5 text-sm"><span>{label}</span><Input aria-label={label} type="number" value={value} onChange={(event) => { if (event.target.value !== '') onChange(event.target.valueAsNumber) }} /></label>
  }
  const text = value == null ? '' : String(value)
  return <label className="block space-y-1.5 text-sm"><span>{label}</span><Textarea aria-label={label} value={text} onChange={(event) => onChange(event.target.value)} rows={text.includes('\n') ? 3 : 2} /></label>
}

export function ProfileContentFields({ content, onChange }: { content: Record<string, unknown>; onChange: (next: Record<string, unknown>) => void }) {
  return <>{Object.entries(content).map(([field, value]) => <ValueField key={field} label={fieldLabel(field)} value={value} onChange={(next) => onChange({ ...content, [field]: next })} />)}</>
}
