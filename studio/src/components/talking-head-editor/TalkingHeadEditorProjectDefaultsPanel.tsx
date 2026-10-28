import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { TagInput } from '@/components/ui/TagInput'
import { Textarea } from '@/components/ui/textarea'
import type { TalkingHeadEditorDefaults } from '@/types'

interface Props { value?: TalkingHeadEditorDefaults; onChange: (value: TalkingHeadEditorDefaults) => void }

export function TalkingHeadEditorProjectDefaultsPanel({ value = {}, onChange }: Props) {
  const preferences = value.preferences ?? {}
  const update = (key: keyof NonNullable<TalkingHeadEditorDefaults['preferences']>, next: string | number | undefined) => {
    const nextPreferences = { ...preferences, [key]: next }
    if (next === undefined || next === '') delete nextPreferences[key]
    onChange({ ...value, preferences: nextPreferences })
  }
  return <section className="space-y-4 border-y border-border py-4" aria-label="口播精剪项目默认值">
    <div><h3 className="text-sm font-medium">口播精剪默认值</h3><p className="mt-1 text-xs text-muted-foreground">单次创作可以覆盖；动效样片通过审核后才会加入声音。</p></div>
    <div className="grid gap-3 sm:grid-cols-2">
      <label className="space-y-1.5 text-sm"><span>默认质量档位</span><NativeSelect aria-label="默认质量档位" value={preferences.quality_tier ?? ''} onChange={(e) => update('quality_tier', e.target.value || undefined)}><NativeSelectOption value="">服务端默认</NativeSelectOption><NativeSelectOption value="effective">高效</NativeSelectOption><NativeSelectOption value="balanced">平衡</NativeSelectOption><NativeSelectOption value="quality">高质量</NativeSelectOption></NativeSelect></label>
      <label className="space-y-1.5 text-sm"><span>默认声音阶段</span><NativeSelect aria-label="默认声音阶段" value={preferences.sound_mode ?? ''} onChange={(e) => update('sound_mode', e.target.value || undefined)}><NativeSelectOption value="">服务端默认</NativeSelectOption><NativeSelectOption value="after_review">样片通过后</NativeSelectOption><NativeSelectOption value="provided_only">只用我的声音素材</NativeSelectOption><NativeSelectOption value="auto">自动寻找授权素材</NativeSelectOption></NativeSelect></label>
      <label className="space-y-1.5 text-sm"><span>默认动效密度</span><NativeSelect aria-label="默认动效密度" value={preferences.broll_mode ?? ''} onChange={(e) => update('broll_mode', e.target.value || undefined)}><NativeSelectOption value="">服务端默认</NativeSelectOption><NativeSelectOption value="auto">自动匹配话术</NativeSelectOption><NativeSelectOption value="provided_only">只用上传素材</NativeSelectOption></NativeSelect></label>
      <label className="space-y-1.5 text-sm"><span>默认画幅</span><NativeSelect aria-label="默认画幅" value={preferences.aspect_ratio ?? ''} onChange={(e) => update('aspect_ratio', e.target.value || undefined)}><NativeSelectOption value="">服务端默认</NativeSelectOption><NativeSelectOption value="9:16">9:16</NativeSelectOption><NativeSelectOption value="16:9">16:9</NativeSelectOption><NativeSelectOption value="1:1">1:1</NativeSelectOption><NativeSelectOption value="4:3">4:3</NativeSelectOption></NativeSelect></label>
    </div>
    <label className="block space-y-1.5 text-sm"><span>长期剪辑风格</span><Textarea aria-label="长期剪辑风格" value={preferences.style ?? ''} onChange={(e) => update('style', e.target.value || undefined)} rows={3} maxLength={2000} placeholder="例如：保留自然节奏，动效服务于每一句话" /></label>
    <label className="block space-y-1.5 text-sm"><span>默认交付目标</span><TagInput aria-label="默认交付目标" value={(value.delivery_targets ?? []).join(', ')} onChange={(next) => onChange({ ...value, delivery_targets: next.split(',').map((item) => item.trim()).filter(Boolean) })} placeholder="输入后按回车添加" maxTags={20} /></label>
  </section>
}
