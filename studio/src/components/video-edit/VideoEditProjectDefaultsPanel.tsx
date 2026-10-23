import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { TagInput } from '@/components/ui/TagInput'
import { Textarea } from '@/components/ui/textarea'
import type { VideoEditDefaults } from '@/types'

interface VideoEditProjectDefaultsPanelProps {
  value?: VideoEditDefaults
  onChange: (value: VideoEditDefaults) => void
}

function splitTags(value: string) {
  return value.split(',').map((tag) => tag.trim()).filter(Boolean)
}

export function VideoEditProjectDefaultsPanel({ value = {}, onChange }: VideoEditProjectDefaultsPanelProps) {
  const preferences = value.preferences ?? {}
  const updatePreference = (key: keyof NonNullable<VideoEditDefaults['preferences']>, next: string | number | undefined) => {
    const updated = { ...preferences, [key]: next }
    if (next === undefined || next === '') delete updated[key]
    onChange({ ...value, preferences: updated })
  }

  return (
    <section className="space-y-4 border-y border-border py-4" aria-label="视频剪辑默认值">
      <div>
        <h3 className="text-sm font-medium">视频剪辑默认值</h3>
        <p className="mt-1 text-xs text-muted-foreground">单次创作可以覆盖；留空时沿用服务端策略。</p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="space-y-1.5 text-sm">
          <span>默认质量档位</span>
          <NativeSelect aria-label="默认质量档位" value={preferences.quality_tier ?? ''} onChange={(event) => updatePreference('quality_tier', event.target.value || undefined)}>
            <NativeSelectOption value="">服务端默认</NativeSelectOption>
            <NativeSelectOption value="effective">高效</NativeSelectOption>
            <NativeSelectOption value="balanced">平衡</NativeSelectOption>
            <NativeSelectOption value="quality">高质量</NativeSelectOption>
          </NativeSelect>
        </label>
        <label className="space-y-1.5 text-sm">
          <span>默认字幕</span>
          <NativeSelect aria-label="默认字幕" value={preferences.subtitle_mode ?? ''} onChange={(event) => updatePreference('subtitle_mode', event.target.value || undefined)}>
            <NativeSelectOption value="">服务端默认</NativeSelectOption>
            <NativeSelectOption value="off">关闭</NativeSelectOption>
            <NativeSelectOption value="required">需要字幕</NativeSelectOption>
          </NativeSelect>
        </label>
        <label className="space-y-1.5 text-sm sm:col-span-2">
          <span>默认目标时长（秒）</span>
          <input aria-label="默认目标时长（秒）" className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm" type="number" min={0} max={1800} value={preferences.max_duration_seconds ?? ''} onChange={(event) => updatePreference('max_duration_seconds', event.target.value ? Number(event.target.value) : undefined)} placeholder="服务端默认" />
        </label>
      </div>
      <label className="block space-y-1.5 text-sm">
        <span>长期剪辑风格</span>
        <Textarea aria-label="长期剪辑风格" value={preferences.style ?? ''} onChange={(event) => updatePreference('style', event.target.value || undefined)} rows={3} maxLength={2000} placeholder="例如：保留自然停顿，优先观点完整，避免夸张转场" />
      </label>
      <label className="block space-y-1.5 text-sm">
        <span>默认交付目标</span>
        <TagInput aria-label="默认交付目标" value={(value.delivery_targets ?? []).join(', ')} onChange={(next) => onChange({ ...value, delivery_targets: splitTags(next) })} placeholder="输入后按回车添加" maxTags={20} />
      </label>
    </section>
  )
}
