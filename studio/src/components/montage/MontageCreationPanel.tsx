import { useEffect, useId, useState, type ReactNode } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { ChevronRight } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { useMontageCapabilities } from '@/hooks/useMontageCapabilities'
import { cn } from '@/lib/utils'
import type { MontageAsset, MontagePipelineCapability } from '@/types'
import { MontagePipelineSelector } from './MontagePipelineSelector'
import { MontageSourceAssetInput } from './MontageSourceAssetInput'
import {
  includesMontageOption,
  montageDurationError,
  montageSubtitleOptions,
  montageVoiceoverOptions,
} from './montage-options'

interface MontageCreationPanelProps {
  form: UseFormReturn<any>
  fieldRoot: 'montage_input'
  onUploadingChange?: (uploading: boolean) => void
  onReadyChange?: (ready: boolean) => void
  briefField?: ReactNode
}

function parseTags(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function sourceRequirementError(capability: MontagePipelineCapability | undefined, assets: MontageAsset[]): string {
  if (!capability) return ''
  const hasLocator = (asset: MontageAsset) => Boolean(asset.url?.trim() || asset.task_file_id?.trim())
  const hasVideo = assets.some((asset) => (asset.type === 'video' || asset.type === 'video_url') && hasLocator(asset))
  const hasAudio = assets.some((asset) => (asset.type === 'audio' || asset.type === 'audio_url') && hasLocator(asset))
  if (capability.source_requirement === 'video' && !hasVideo) return '请添加至少一段视频素材'
  if (capability.source_requirement === 'video_or_audio' && !hasVideo && !hasAudio) return '请添加至少一段视频或音频素材'
  return ''
}

export function MontageCreationPanel({
  form,
  fieldRoot,
  onUploadingChange,
  onReadyChange,
  briefField,
}: MontageCreationPanelProps) {
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const groupID = useId()
  const control = form.control
  const pipelinePath = `${fieldRoot}.pipeline_key`
  const durationPath = `${fieldRoot}.preferences.duration_seconds`
  const pipelineKey = useWatch({ control, name: pipelinePath }) as string | undefined
  const duration = useWatch({ control, name: durationPath }) as number | undefined
  const assets = (useWatch({ control, name: `${fieldRoot}.source_assets` }) ?? []) as MontageAsset[]
  const {
    items,
    enabled,
    defaultPipeline,
    maxDurationSeconds,
    maxAssets,
    isLoading,
    isError,
    capabilityByKey,
  } = useMontageCapabilities()
  const selectedCapability = pipelineKey ? capabilityByKey(pipelineKey) : undefined
  const sourceError = sourceRequirementError(selectedCapability, assets)
  const assetsError = maxAssets > 0 && assets.length > maxAssets ? `来源素材不能超过 ${maxAssets} 个` : ''
  const durationError = montageDurationError(duration, maxDurationSeconds)
  const catalogError = isLoading
    ? ''
    : isError
      ? '视频类型加载失败，暂时无法创建视频'
      : !enabled
        ? '视频创作当前不可用'
        : pipelineKey && !selectedCapability
          ? '当前视频类型已停用，请重新选择'
          : !pipelineKey
            ? '请选择视频类型'
            : ''
  const ready = !isLoading && !isError && enabled && !!selectedCapability && !sourceError && !assetsError && !durationError

  useEffect(() => {
    if (isLoading || isError || !enabled || pipelineKey || form.getFieldState(pipelinePath).isDirty) return
    const fallback = capabilityByKey(defaultPipeline) ?? items[0]
    if (!fallback) return
    form.setValue(pipelinePath, fallback.key, { shouldDirty: false, shouldValidate: true })
  }, [capabilityByKey, defaultPipeline, enabled, form, isError, isLoading, items, pipelineKey, pipelinePath])

  useEffect(() => {
    if (!selectedCapability || form.getFieldState(durationPath).isDirty) return
    const currentDuration = form.getValues(durationPath)
    if (currentDuration !== undefined && currentDuration !== null && currentDuration !== 0 && currentDuration !== '') return
    form.setValue(durationPath, selectedCapability.recommended_duration_seconds, {
      shouldDirty: false,
      shouldValidate: true,
    })
  }, [durationPath, form, selectedCapability])

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
    <div className="space-y-4">
      {briefField ?? <FormField control={control} name={`${fieldRoot}.brief`} render={({ field }) => (
        <FormItem>
          <FormLabel>视频 brief</FormLabel>
          <FormControl>
            <Textarea
              {...field}
              value={field.value ?? ''}
              placeholder="描述视频内容、叙事重点、节奏和期望效果"
              rows={4}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )} />}

      <MontagePipelineSelector
        items={items}
        value={pipelineKey}
        loading={isLoading}
        disabled={isLoading || isError || !enabled}
        error={catalogError}
        onChange={selectPipeline}
      />

      <FormField control={control} name={`${fieldRoot}.source_assets`} render={({ field }) => (
        <FormItem>
          <FormLabel>来源素材</FormLabel>
          <FormControl>
            <MontageSourceAssetInput
              value={field.value ?? []}
              onChange={field.onChange}
              onUploadingChange={onUploadingChange}
              hint={selectedCapability?.source_hint ?? '选择视频类型后查看素材要求'}
              maxCount={maxAssets || 20}
            />
          </FormControl>
          {(sourceError || assetsError) && (
            <p role="alert" className="text-xs text-destructive">{sourceError || assetsError}</p>
          )}
          <FormMessage />
        </FormItem>
      )} />

      <section aria-labelledby={`${groupID}-output-settings`} className="space-y-3">
        <h3 id={`${groupID}-output-settings`} className="text-sm font-medium text-foreground">成片设置</h3>
        <div className="grid gap-3 md:grid-cols-3">
          <FormField control={control} name={durationPath} render={({ field }) => (
            <FormItem>
              <FormLabel>{selectedCapability?.output_mode === 'multiple' ? '单条目标时长' : '目标时长'}</FormLabel>
              <FormControl>
                <Input
                  aria-label={selectedCapability?.output_mode === 'multiple' ? '单条目标时长（秒）' : '目标时长（秒）'}
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
          <FormField control={control} name={`${fieldRoot}.preferences.subtitle_mode`} render={({ field }) => {
            const customValue = !includesMontageOption(montageSubtitleOptions, field.value) ? field.value : undefined
            return (
              <FormItem>
                <FormLabel>字幕</FormLabel>
                <FormControl>
                  <NativeSelect aria-label="字幕" className="w-full" {...field} value={field.value ?? ''}>
                    {customValue && <NativeSelectOption value={customValue}>已有自定义值：{customValue}</NativeSelectOption>}
                    {montageSubtitleOptions.map((option) => (
                      <NativeSelectOption key={option.value || 'auto'} value={option.value}>{option.label}</NativeSelectOption>
                    ))}
                  </NativeSelect>
                </FormControl>
                <FormMessage />
              </FormItem>
            )
          }} />
          <FormField control={control} name={`${fieldRoot}.preferences.voiceover_mode`} render={({ field }) => {
            const customValue = !includesMontageOption(montageVoiceoverOptions, field.value) ? field.value : undefined
            return (
              <FormItem>
                <FormLabel>配音</FormLabel>
                <FormControl>
                  <NativeSelect aria-label="配音" className="w-full" {...field} value={field.value ?? ''}>
                    {customValue && <NativeSelectOption value={customValue}>已有自定义值：{customValue}</NativeSelectOption>}
                    {montageVoiceoverOptions.map((option) => (
                      <NativeSelectOption key={option.value || 'auto'} value={option.value}>{option.label}</NativeSelectOption>
                    ))}
                  </NativeSelect>
                </FormControl>
                <FormMessage />
              </FormItem>
            )
          }} />
        </div>
      </section>

      <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
        <CollapsibleTrigger className="group flex w-full items-center gap-1 rounded-md py-1 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground">
          <ChevronRight className={cn('size-4 transition-transform', advancedOpen && 'rotate-90')} />
          更多创作要求
        </CollapsibleTrigger>
        <CollapsibleContent className="space-y-3 pt-3">
          <div className="grid gap-3 md:grid-cols-2">
            <FormField control={control} name={`${fieldRoot}.preferences.style`} render={({ field }) => (
              <FormItem>
                <FormLabel>风格偏好</FormLabel>
                <FormControl>
                  <Textarea aria-label="风格偏好" {...field} value={field.value ?? ''} placeholder="镜头、色彩、节奏与整体调性" rows={3} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )} />
            <FormField control={control} name={`${fieldRoot}.preferences.music_prompt`} render={({ field }) => (
              <FormItem>
                <FormLabel>音乐提示</FormLabel>
                <FormControl>
                  <Textarea aria-label="音乐提示" {...field} value={field.value ?? ''} placeholder="音乐风格、情绪与节奏" rows={3} />
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
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}
