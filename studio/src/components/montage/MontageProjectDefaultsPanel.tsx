import type { UseFormReturn } from 'react-hook-form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import type { ProjectFormValues } from '@/lib/schemas'

interface MontageProjectDefaultsPanelProps {
  form: UseFormReturn<ProjectFormValues>
}

function parseTags(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function MontageProjectDefaultsPanel({ form }: MontageProjectDefaultsPanelProps) {
  return (
    <section aria-labelledby="montage-defaults-heading" className="flex flex-col gap-4 border-t border-border pt-4">
      <h4 id="montage-defaults-heading" className="text-sm font-medium text-muted-foreground">Montage 默认配置</h4>

      <div className="grid gap-3 sm:grid-cols-2">
        <FormField control={form.control} name="montage_defaults.default_pipeline" render={({ field }) => (
          <FormItem>
            <FormLabel>默认 Pipeline</FormLabel>
            <FormControl>
              <Input aria-label="默认 Pipeline" {...field} value={field.value ?? ''} placeholder="例如 social-short" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="montage_defaults.preferences.aspect_ratio" render={({ field }) => (
          <FormItem>
            <FormLabel>默认画幅</FormLabel>
            <FormControl>
              <Input aria-label="默认画幅" {...field} value={field.value ?? ''} placeholder="例如 9:16" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="montage_defaults.preferences.duration_seconds" render={({ field }) => (
          <FormItem>
            <FormLabel>默认时长（秒）</FormLabel>
            <FormControl>
              <Input
                aria-label="默认时长（秒）"
                type="number"
                min={1}
                max={600}
                value={field.value ?? ''}
                onChange={(event) => field.onChange(event.target.value ? Number(event.target.value) : undefined)}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="montage_defaults.preferences.subtitle_mode" render={({ field }) => (
          <FormItem>
            <FormLabel>字幕模式</FormLabel>
            <FormControl>
              <Input aria-label="字幕模式" {...field} value={field.value ?? ''} placeholder="例如 burned-in" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="montage_defaults.preferences.voiceover_mode" render={({ field }) => (
          <FormItem>
            <FormLabel>配音模式</FormLabel>
            <FormControl>
              <Input aria-label="配音模式" {...field} value={field.value ?? ''} placeholder="例如 narrated" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
      </div>

      <FormField control={form.control} name="montage_defaults.preferences.style" render={({ field }) => (
        <FormItem>
          <FormLabel>风格偏好</FormLabel>
          <FormControl>
            <Textarea aria-label="风格偏好" {...field} value={field.value ?? ''} rows={3} placeholder="镜头、色彩、节奏与整体视觉调性" />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />
      <FormField control={form.control} name="montage_defaults.preferences.music_prompt" render={({ field }) => (
        <FormItem>
          <FormLabel>音乐提示</FormLabel>
          <FormControl>
            <Textarea aria-label="音乐提示" {...field} value={field.value ?? ''} rows={2} placeholder="音乐风格、情绪与节奏" />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />
      <FormField control={form.control} name="montage_defaults.asset_guidance" render={({ field }) => (
        <FormItem>
          <FormLabel>素材使用说明</FormLabel>
          <FormControl>
            <Textarea aria-label="素材使用说明" {...field} value={field.value ?? ''} rows={3} placeholder="素材优先级、必须保留内容与禁用方式" />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />
      <FormField control={form.control} name="montage_defaults.delivery_targets" render={({ field }) => (
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
    </section>
  )
}
