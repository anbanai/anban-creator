import { useEffect } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { useMontageCapabilities } from '@/hooks/useMontageCapabilities'
import type { ProjectFormValues } from '@/lib/schemas'
import type { MontagePipelineCapability } from '@/types'
import { MontagePipelineSelector } from './MontagePipelineSelector'
import {
  includesMontageOption,
  montageDurationError,
  montageOutputPreviewSize,
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
  const outputProfilePath = 'montage_defaults.output_profile_key' as const
  const outputProfileKey = useWatch({ control: form.control, name: outputProfilePath })
  const pipelineKey = useWatch({ control: form.control, name: pipelinePath })
  const duration = useWatch({ control: form.control, name: durationPath })
  const {
    items,
    outputProfiles = [],
    defaultOutputProfile = 'tiktok',
    styles = [],
    enabled,
    defaultPipeline,
    maxDurationSeconds,
    isLoading,
    isError,
    capabilityByKey,
  } = useMontageCapabilities()
  const selectableItems = items.filter((item) => item.available !== false && item.key !== 'framework-smoke')
  const selectedCapability = pipelineKey ? capabilityByKey(pipelineKey) : undefined
  const selectedProfile = outputProfiles.find((item) => item.key === outputProfileKey)
  const durationError = montageDurationError(duration, maxDurationSeconds)
  const catalogError = isLoading
    ? ''
    : isError
      ? '视频类型加载失败，暂时无法保存视频默认设置'
      : !enabled
        ? '视频创作当前不可用'
        : pipelineKey && (!selectedCapability || selectedCapability.available === false || selectedCapability.key === 'framework-smoke')
          ? '当前视频类型已停用，请重新选择'
          : !pipelineKey
            ? '请选择视频类型'
            : ''
  const outputProfileError = !isLoading && (outputProfiles.length === 0 || !selectedProfile)
    ? '成片规格缺失或无效，暂时无法保存视频默认设置'
    : ''
  const ready = !isLoading && !isError && enabled && !!selectedCapability && selectedCapability.available !== false && selectedCapability.key !== 'framework-smoke' && !outputProfileError && !durationError

  useEffect(() => {
    if (isLoading || isError || !enabled || pipelineKey || form.getFieldState(pipelinePath).isDirty) return
    const fallback = selectableItems.find((item) => item.key === defaultPipeline) ?? selectableItems[0]
    if (!fallback) return
    form.setValue(pipelinePath, fallback.key, { shouldDirty: false, shouldValidate: true })
  }, [capabilityByKey, defaultPipeline, enabled, form, isError, isLoading, selectableItems, pipelineKey])

  useEffect(() => {
    if (isLoading || outputProfileKey) return
    const profile = outputProfiles.find((item) => item.key === defaultOutputProfile) ?? outputProfiles[0]
    if (profile) form.setValue(outputProfilePath, profile.key, { shouldDirty: false })
  }, [defaultOutputProfile, form, isLoading, outputProfileKey, outputProfilePath, outputProfiles])

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
        <p className="mt-1 text-xs text-muted-foreground">用于此人设的新作品和排期，创建时仍可单独调整。</p>
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

      {outputProfiles.length > 0 && <fieldset className="space-y-2">
        <legend className="text-sm font-medium text-foreground">默认成片画幅</legend>
        <div className="flex flex-wrap gap-2">
          {outputProfiles.map((profile) => <label key={profile.key} className={`flex min-w-0 cursor-pointer items-center gap-2 rounded-md border px-3 py-2 text-sm ${outputProfileKey === profile.key ? 'border-primary bg-primary/5' : 'border-border'}`}>
            <input type="radio" name="montage-default-profile" checked={outputProfileKey === profile.key} onChange={() => form.setValue(outputProfilePath, profile.key, { shouldDirty: true, shouldValidate: true })} className="sr-only" />
            <span aria-hidden="true" className="flex size-8 shrink-0 items-center justify-center rounded-sm bg-muted"><span className="block border-2 border-foreground bg-background" style={montageOutputPreviewSize(profile.width, profile.height)} /></span>
            <span className="min-w-0 break-words">{profile.display_name}<span className="ml-1 text-[11px] text-muted-foreground">{profile.aspect_ratio}</span></span>
          </label>)}
        </div>
        {selectedProfile && <p className="text-[11px] text-muted-foreground">{selectedProfile.width} × {selectedProfile.height}</p>}
      </fieldset>}
      {outputProfileError && <p role="alert" className="text-xs text-destructive">{outputProfileError}</p>}

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
            <FormDescription>留空表示由每次创作自行决定。</FormDescription>
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
            <FormDescription>留空表示由每次创作自行决定。</FormDescription>
            <FormMessage />
          </FormItem>
        )} />
      </div>

      {styles.length > 0 && <FormField control={form.control} name="montage_defaults.preferences.playbook_key" render={({ field }) => (
        <FormItem>
          <FormLabel>默认风格模板</FormLabel>
          <FormControl>
            <NativeSelect aria-label="默认风格模板" className="w-full" {...field} value={field.value ?? ''}>
              <NativeSelectOption value="">不指定</NativeSelectOption>
              <NativeSelectOption value="auto">自动选择</NativeSelectOption>
              {styles.map((style) => <NativeSelectOption key={style.key} value={style.key}>{style.display_name}</NativeSelectOption>)}
            </NativeSelect>
          </FormControl>
          <FormDescription>新视频默认采用此模板，单次创作可覆盖。</FormDescription>
        </FormItem>
      )} />}
      <FormField control={form.control} name="montage_defaults.preferences.music_prompt" render={({ field }) => (
        <FormItem>
          <FormLabel>音乐提示</FormLabel>
          <FormControl>
            <Textarea aria-label="音乐提示" {...field} value={field.value ?? ''} rows={2} placeholder="音乐风格、情绪与节奏" />
          </FormControl>
          <FormDescription>默认配乐方向；单次创作可临时改写。</FormDescription>
          <FormMessage />
        </FormItem>
      )} />
      <FormField control={form.control} name="montage_defaults.asset_guidance" render={({ field }) => (
        <FormItem>
          <FormLabel>素材使用说明</FormLabel>
          <FormControl>
            <Textarea aria-label="素材使用说明" {...field} value={field.value ?? ''} rows={3} placeholder="素材优先级、必须保留内容与禁用方式" />
          </FormControl>
          <FormDescription>长期素材规则，例如品牌必须露出、禁用第三方素材。</FormDescription>
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
          <FormDescription>该人设视频作品的默认交付要求；单次创作可追加。</FormDescription>
          <FormMessage />
        </FormItem>
      )} />
    </section>
  )
}
