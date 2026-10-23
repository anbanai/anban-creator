import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import type { InputAttachment } from '@/types/input-attachment'
import type { VideoEditDefaults, VideoEditPreferences } from '@/types/video-edit'

interface AssistantVideoEditInputsProps {
  assets: InputAttachment[]
  brief: string
  preferences: VideoEditPreferences
  defaults?: VideoEditDefaults
  uploading: boolean
  onAssetsChange: (assets: InputAttachment[]) => void
  onBriefChange: (brief: string) => void
  onPreferencesChange: (preferences: VideoEditPreferences) => void
  onUploadingChange: (uploading: boolean) => void
  onFailuresChange: (failed: boolean) => void
}

export function AssistantVideoEditInputs({
  assets,
  brief,
  preferences,
  defaults,
  uploading,
  onAssetsChange,
  onBriefChange,
  onPreferencesChange,
  onUploadingChange,
  onFailuresChange,
}: AssistantVideoEditInputsProps) {
  const defaultPreferences = defaults?.preferences
  const setPreference = <K extends keyof VideoEditPreferences>(key: K, value: VideoEditPreferences[K] | undefined) => {
    onPreferencesChange({ ...preferences, [key]: value })
  }

  return (
    <section className="mt-6 space-y-4 rounded-lg border border-border p-4 text-left" aria-label="全自动视频剪辑输入">
      <h2 className="text-sm font-semibold">全自动视频剪辑</h2>
      <div className="space-y-2">
        <label htmlFor="assistant-video-edit-brief" className="text-sm font-medium">剪辑要求</label>
        <Textarea id="assistant-video-edit-brief" aria-label="剪辑要求" value={brief} onChange={(event) => onBriefChange(event.target.value)} rows={3} maxLength={5000} placeholder="说明主题、重点片段、节奏和交付平台" />
      </div>
      <div className="space-y-2">
        <p className="text-sm font-medium">剪辑素材</p>
        <ReferenceMaterialInput
          value={assets}
          onChange={onAssetsChange}
          allowedTypes={['video', 'audio', 'image']}
          maxCount={20}
          maxFileBytes={256 * 1024 * 1024}
          compact
          uploadPurpose="video_edit_asset"
          hint="上传本次剪辑要使用的视频、音频和图片。"
          onUploadingChange={onUploadingChange}
          onFailuresChange={onFailuresChange}
        />
        <p className="text-xs text-muted-foreground">最多 20 个文件，每个不超过 256 MB。</p>
        {uploading && <p role="status" className="text-xs text-muted-foreground">素材上传中</p>}
      </div>
      <section className="space-y-3 border-t border-border pt-4" aria-label="剪辑偏好">
        <h3 className="text-sm font-medium">剪辑偏好</h3>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="space-y-1.5 text-sm">
            <span>质量档位</span>
            <NativeSelect aria-label="质量档位" value={preferences.quality_tier ?? ''} onChange={(event) => setPreference('quality_tier', (event.target.value || undefined) as VideoEditPreferences['quality_tier'])}>
              <NativeSelectOption value="">使用人设默认值{defaultPreferences?.quality_tier ? `（${defaultPreferences.quality_tier}）` : ''}</NativeSelectOption>
              <NativeSelectOption value="effective">高效</NativeSelectOption><NativeSelectOption value="balanced">平衡</NativeSelectOption><NativeSelectOption value="quality">高质量</NativeSelectOption>
            </NativeSelect>
          </label>
          <label className="space-y-1.5 text-sm">
            <span>字幕</span>
            <NativeSelect aria-label="字幕" value={preferences.subtitle_mode ?? ''} onChange={(event) => setPreference('subtitle_mode', (event.target.value || undefined) as VideoEditPreferences['subtitle_mode'])}>
              <NativeSelectOption value="">使用人设默认值{defaultPreferences?.subtitle_mode ? `（${defaultPreferences.subtitle_mode}）` : ''}</NativeSelectOption>
              <NativeSelectOption value="off">关闭</NativeSelectOption><NativeSelectOption value="required">生成字幕</NativeSelectOption>
            </NativeSelect>
            <span className="block text-xs text-muted-foreground">生成字幕需要服务端配置转写凭据。</span>
          </label>
          <label className="space-y-1.5 text-sm">
            <span>目标时长（秒）</span>
            <Input aria-label="目标时长（秒）" type="number" min={1} max={1800} value={preferences.max_duration_seconds ?? ''} onChange={(event) => setPreference('max_duration_seconds', event.target.value ? Number(event.target.value) : undefined)} placeholder="使用人设默认值或不限制" />
          </label>
        </div>
        <label className="block space-y-1.5 text-sm">
          <span>本次剪辑风格</span>
          <Textarea aria-label="本次剪辑风格" value={preferences.style ?? ''} onChange={(event) => setPreference('style', event.target.value || undefined)} rows={2} maxLength={2000} placeholder={defaultPreferences?.style || '例如：保留现场原声，减少空镜，突出核心观点'} />
        </label>
      </section>
    </section>
  )
}
