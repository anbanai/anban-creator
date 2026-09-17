import { useEffect } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { useMontageCapabilities } from '@/hooks/useMontageCapabilities'
import type { ProjectFormValues } from '@/lib/schemas'
import type { MontagePipelineCapability } from '@/types'
import { MontagePipelineSelector } from './MontagePipelineSelector'
import {
  includesMontageOption,
  montageDurationError,
  montageSubtitleOptions,
  montageVoiceoverOptions,
} from './montage-options'

interface MontageProjectDefaultsPanelProps {
  form: UseFormReturn<ProjectFormValues>
  onReadyChange?: (ready: boolean) => void
}

function parseTags(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function MontageProjectDefaultsPanel({ form, onReadyChange }: MontageProjectDefaultsPanelProps) {
  const pipelinePath = 'montage_defaults.default_pipeline' as const
  const durationPath = 'montage_defaults.preferences.duration_seconds' as const
  const pipelineKey = useWatch({ control: form.control, name: pipelinePath })
  const duration = useWatch({ control: form.control, name: durationPath })
  const {
    items,
    enabled,
    defaultPipeline,
    maxDurationSeconds,
    isLoading,
    isError,
    capabilityByKey,
  } = useMontageCapabilities()
  const selectedCapability = pipelineKey ? capabilityByKey(pipelineKey) : undefined
  const durationError = montageDurationError(duration, maxDurationSeconds)
  const catalogError = isLoading
    ? ''
    : isError
      ? '视频类型加载失败，暂时无法保存视频默认设置'
      : !enabled
        ? '视频创作当前不可用'
        : pipelineKey && !selectedCapability
          ? '当前视频类型已停用，请重新选择'
          : !pipelineKey
            ? '请选择视频类型'
            : ''
  const ready = !isLoading && !isError && enabled && !!selectedCapability && !durationError

  useEffect(() => {
    if (isLoading || isError || !enabled || pipelineKey || form.getFieldState(pipelinePath).isDirty) return
    const fallback = capabilityByKey(defaultPipeline) ?? items[0]
    if (!fallback) return
    form.setValue(pipelinePath, fallback.key, { shouldDirty: false, shouldValidate: true })
  }, [capabilityByKey, defaultPipeline, enabled, form, isError, isLoading, items, pipelineKey])

  useEffect(() => {
    if (!selectedCapability || form.getFieldState(durationPath).isDirty) return
    const currentDuration = form.getValues(durationPath)
    if (currentDuration !== undefined && currentDuration !== null && currentDuration !== 0) return
    form.setValue(durationPath, selectedCapability.recommended_duration_seconds, {
      shouldDirty: false,
      shouldValidate: true,
    })
  }, [form, selectedCapability])

  useEffect(() => {
    onReadyChange?.(ready)
  }, [onReadyChange, ready])

  function selectPipeline(capability: MontagePipelineCapability) {
    form.setValue(pipelinePath, capability.key, { shouldDirty: true, shouldTouch: true, shouldValidate: true })
    if (!form.getFieldState(durationPath).isDirty) {
      form.setValue(durationPath, capability.recommended_duration_seconds, {
        shouldDirty: false,
        shouldValidate: true,
      })
    }
  }

  return (
    <section aria-labelledby="montage-defaults-heading" className="flex flex-col gap-4 border-t border-border pt-4">
      <div>
        <h4 id="montage-defaults-heading" className="text-sm font-medium text-foreground">视频默认设置</h4>
        <p className="mt-1 text-xs text-muted-foreground">用于此项目的新任务和计划，创建时仍可单独调整。</p>
      </div>

      <MontagePipelineSelector
        items={items}
        value={pipelineKey}
        loading={isLoading}
        disabled={isLoading || isError || !enabled}
        error={catalogError}
        compact
        onChange={selectPipeline}
      />

      <div className="grid gap-3 md:grid-cols-3">
        <FormField control={form.control} name="montage_defaults.preferences.duration_seconds" render={({ field }) => (
          <FormItem>
            <FormLabel>{selectedCapability?.output_mode === 'multiple' ? '默认单条目标时长' : '默认时长'}</FormLabel>
            <FormControl>
              <Input
                aria-label="默认时长（秒）"
                type="number"
                min={1}
                max={maxDurationSeconds || undefined}
                aria-invalid={Boolean(durationError)}
                value={field.value ?? ''}
                onChange={(event) => field.onChange(event.target.value ? Number(event.target.value) : undefined)}
                placeholder="秒"
              />
            </FormControl>
            <p className="text-[11px] leading-4 text-muted-foreground">
              {selectedCapability
                ? `建议 ${selectedCapability.recommended_duration_seconds} 秒${maxDurationSeconds ? `，最长 ${maxDurationSeconds} 秒` : ''}`
                : '选择视频类型后显示建议时长'}
            </p>
            {durationError && <p role="alert" className="text-xs text-destructive">{durationError}</p>}
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="montage_defaults.preferences.subtitle_mode" render={({ field }) => (
          <FormItem>
            <FormLabel>默认字幕</FormLabel>
            <FormControl>
              <NativeSelect aria-label="默认字幕" className="w-full" {...field} value={field.value ?? ''}>
                {!includesMontageOption(montageSubtitleOptions, field.value) && field.value && (
                  <NativeSelectOption value={field.value}>已有自定义值：{field.value}</NativeSelectOption>
                )}
                {montageSubtitleOptions.map((option) => (
                  <NativeSelectOption key={option.value || 'auto'} value={option.value}>{option.label}</NativeSelectOption>
                ))}
              </NativeSelect>
            </FormControl>
            <FormMessage />
          </FormItem>
        )} />
        <FormField control={form.control} name="montage_defaults.preferences.voiceover_mode" render={({ field }) => (
          <FormItem>
            <FormLabel>默认配音</FormLabel>
            <FormControl>
              <NativeSelect aria-label="默认配音" className="w-full" {...field} value={field.value ?? ''}>
                {!includesMontageOption(montageVoiceoverOptions, field.value) && field.value && (
                  <NativeSelectOption value={field.value}>已有自定义值：{field.value}</NativeSelectOption>
                )}
                {montageVoiceoverOptions.map((option) => (
                  <NativeSelectOption key={option.value || 'auto'} value={option.value}>{option.label}</NativeSelectOption>
                ))}
              </NativeSelect>
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
