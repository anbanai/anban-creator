import type { ReactNode } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { MontageSourceAssetInput } from './MontageSourceAssetInput'

interface MontageCreationPanelProps {
  form: UseFormReturn<any>
  fieldRoot: 'montage_input'
  onUploadingChange?: (uploading: boolean) => void
  briefField?: ReactNode
}

function parseTags(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function MontageCreationPanel({ form, fieldRoot, onUploadingChange, briefField }: MontageCreationPanelProps) {
  const control = form.control

  return (
    <div className="space-y-4">
      {briefField ?? <FormField control={control} name={`${fieldRoot}.brief`} render={({ field }) => (
        <FormItem>
          <FormLabel>视频 brief</FormLabel>
          <FormControl>
            <Textarea
              {...field}
              value={field.value ?? ''}
              placeholder="描述这次要生产的视频内容、素材用途、节奏和交付目标"
              rows={5}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />}

      <FormField control={control} name={`${fieldRoot}.source_assets`} render={({ field }) => (
        <FormItem>
          <FormLabel>来源素材</FormLabel>
          <FormControl>
            <MontageSourceAssetInput
              value={field.value ?? []}
              onChange={field.onChange}
              onUploadingChange={onUploadingChange}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />

      <div className="grid gap-3 md:grid-cols-3">
        <FormField control={control} name={`${fieldRoot}.pipeline_key`} render={({ field }) => (
          <FormItem>
            <FormLabel>Pipeline</FormLabel>
            <FormControl>
              <Input {...field} value={field.value ?? ''} placeholder="pipeline（可选）" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={control} name={`${fieldRoot}.preferences.aspect_ratio`} render={({ field }) => (
          <FormItem>
            <FormLabel>画幅</FormLabel>
            <FormControl>
              <Input {...field} value={field.value ?? ''} placeholder="画幅，如 9:16" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={control} name={`${fieldRoot}.preferences.duration_seconds`} render={({ field }) => (
          <FormItem>
            <FormLabel>时长（秒）</FormLabel>
            <FormControl>
              <Input
                aria-label="时长（秒）"
                type="number"
                min={1}
                max={600}
                value={field.value ?? ''}
                onChange={(event) => field.onChange(event.target.value ? Number(event.target.value) : undefined)}
                placeholder="时长（秒）"
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </div>

      <div className="grid gap-3 md:grid-cols-2">
        <FormField control={control} name={`${fieldRoot}.preferences.subtitle_mode`} render={({ field }) => (
          <FormItem>
            <FormLabel>字幕模式</FormLabel>
            <FormControl>
              <Input aria-label="字幕模式" {...field} value={field.value ?? ''} placeholder="例如 burned-in" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={control} name={`${fieldRoot}.preferences.voiceover_mode`} render={({ field }) => (
          <FormItem>
            <FormLabel>配音模式</FormLabel>
            <FormControl>
              <Input aria-label="配音模式" {...field} value={field.value ?? ''} placeholder="例如 narrated" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </div>

      <div className="grid gap-3 md:grid-cols-2">
        <FormField control={control} name={`${fieldRoot}.preferences.style`} render={({ field }) => (
          <FormItem>
            <FormLabel>风格偏好</FormLabel>
            <FormControl>
              <Textarea {...field} value={field.value ?? ''} placeholder="风格偏好（可选）" rows={3} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={control} name={`${fieldRoot}.preferences.music_prompt`} render={({ field }) => (
          <FormItem>
            <FormLabel>音乐提示</FormLabel>
            <FormControl>
              <Textarea
                aria-label="音乐提示"
                {...field}
                value={field.value ?? ''}
                placeholder="音乐风格、情绪与节奏"
                rows={3}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </div>

      <FormField control={control} name={`${fieldRoot}.delivery_targets`} render={({ field }) => (
        <FormItem>
          <FormLabel>交付目标</FormLabel>
          <FormControl>
            <TagInput
              value={(field.value ?? []).join(', ')}
              onChange={(value) => field.onChange(parseTags(value))}
              placeholder="输入交付目标后按回车"
              maxTags={20}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />
    </div>
  )
}
