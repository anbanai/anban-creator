import { useEffect, useState, type ReactNode } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { Input } from '@/components/ui/input'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import type { InputAttachment } from '@/types/input-attachment'
import type { VideoEditAsset } from '@/types'

interface VideoEditCreationPanelProps {
  form: UseFormReturn<any>
  briefField?: ReactNode
  onUploadingChange?: (uploading: boolean) => void
  onReadyChange?: (ready: boolean) => void
}

function toAttachment(asset: VideoEditAsset): InputAttachment {
  return { type: asset.type, url: asset.url, file_name: asset.file_name, content_type: asset.mime_type, size: asset.file_size }
}

function toAsset(attachment: InputAttachment): VideoEditAsset {
  return {
    type: attachment.type as VideoEditAsset['type'],
    url: attachment.url,
    file_name: attachment.file_name,
    mime_type: attachment.content_type,
    file_size: attachment.size,
  }
}

export function VideoEditCreationPanel({ form, briefField, onUploadingChange, onReadyChange }: VideoEditCreationPanelProps) {
  const assets = (useWatch({ control: form.control, name: 'video_edit_input.source_assets' }) ?? []) as VideoEditAsset[]
  const [hasUploadFailures, setHasUploadFailures] = useState(false)
  useEffect(() => onReadyChange?.(assets.length > 0 && !hasUploadFailures), [assets.length, hasUploadFailures, onReadyChange])

  return (
    <div className="space-y-4">
      {briefField ?? <FormField control={form.control} name="video_edit_input.brief" render={({ field }) => (
        <FormItem>
          <FormLabel>剪辑要求</FormLabel>
          <FormControl><Textarea {...field} value={field.value ?? ''} rows={4} placeholder="说明主题、重点片段、节奏和交付平台" /></FormControl>
          <FormMessage />
        </FormItem>
      )} />}

      <FormField control={form.control} name="video_edit_input.source_assets" render={({ field }) => (
        <FormItem>
          <FormLabel>剪辑素材</FormLabel>
          <FormControl>
            <ReferenceMaterialInput
              value={(field.value ?? []).map(toAttachment)}
              onChange={(next) => field.onChange(next.map(toAsset))}
              allowedTypes={['video', 'audio', 'image']}
              maxCount={20}
              maxFileBytes={256 * 1024 * 1024}
              compact
              uploadPurpose="video_edit_asset"
              hint="上传本次剪辑要使用的视频、音频和图片。"
              onUploadingChange={onUploadingChange}
              onFailuresChange={setHasUploadFailures}
            />
          </FormControl>
          <FormDescription>仅使用本次上传的素材；支持最多 20 个文件。</FormDescription>
          <FormMessage />
        </FormItem>
      )} />

      <section className="space-y-3 border-t border-border pt-4" aria-label="剪辑偏好">
        <h3 className="text-sm font-medium">剪辑偏好</h3>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField control={form.control} name="video_edit_input.preferences.quality_tier" render={({ field }) => (
            <FormItem>
              <FormLabel>质量档位</FormLabel>
              <FormControl><NativeSelect aria-label="质量档位" value={field.value ?? 'effective'} onChange={(event) => field.onChange(event.target.value)}>
                <NativeSelectOption value="effective">高效</NativeSelectOption><NativeSelectOption value="balanced">平衡</NativeSelectOption><NativeSelectOption value="quality">高质量</NativeSelectOption>
              </NativeSelect></FormControl>
              <FormMessage />
            </FormItem>
          )} />
          <FormField control={form.control} name="video_edit_input.preferences.subtitle_mode" render={({ field }) => (
            <FormItem>
              <FormLabel>字幕</FormLabel>
              <FormControl><NativeSelect aria-label="字幕" value={field.value ?? 'off'} onChange={(event) => field.onChange(event.target.value)}>
                <NativeSelectOption value="off">关闭</NativeSelectOption><NativeSelectOption value="required">生成字幕</NativeSelectOption>
              </NativeSelect></FormControl>
              <FormDescription>生成字幕需要服务端配置转写凭据。</FormDescription>
              <FormMessage />
            </FormItem>
          )} />
          <FormField control={form.control} name="video_edit_input.preferences.max_duration_seconds" render={({ field }) => (
            <FormItem>
              <FormLabel>目标时长（秒）</FormLabel>
              <FormControl><Input aria-label="目标时长（秒）" type="number" min={0} max={1800} value={field.value ?? ''} onChange={(event) => field.onChange(event.target.value ? Number(event.target.value) : undefined)} placeholder="不限制" /></FormControl>
              <FormMessage />
            </FormItem>
          )} />
        </div>
        <FormField control={form.control} name="video_edit_input.preferences.style" render={({ field }) => (
          <FormItem>
            <FormLabel>本次风格</FormLabel>
            <FormControl><Textarea aria-label="本次剪辑风格" {...field} value={field.value ?? ''} rows={3} maxLength={2000} placeholder="例如：保留现场原声，减少空镜，突出核心观点" /></FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </section>
    </div>
  )
}
