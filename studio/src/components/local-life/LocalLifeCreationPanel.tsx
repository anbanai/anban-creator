import { useEffect, useMemo, type ReactNode } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { useLocalLifeCapabilities } from '@/hooks/useLocalLifeCapabilities'
import type { InputAttachment } from '@/types/input-attachment'
import type { LocalLifeAsset, LocalLifeInput } from '@/types'

type AdapterAttachment = InputAttachment & { __localLifeAsset?: LocalLifeAsset }

function toAttachment(asset: LocalLifeAsset): AdapterAttachment {
  return {
    type: asset.type,
    url: asset.url,
    file_name: asset.file_name,
    content_type: asset.mime_type,
    size: asset.file_size,
    __localLifeAsset: asset,
  }
}

function toAsset(attachment: AdapterAttachment): LocalLifeAsset {
  return {
    ...(attachment.__localLifeAsset || {}),
    type: attachment.type === 'image' ? 'image' : 'video',
    url: attachment.url,
    file_name: attachment.file_name,
    mime_type: attachment.content_type,
    file_size: attachment.size,
  }
}

export function LocalLifeCreationPanel({
  form,
  fieldRoot = 'local_life_input',
  briefField,
  onReadyChange,
  onUploadingChange,
}: {
  form: UseFormReturn<any>
  fieldRoot?: 'local_life_input'
  briefField?: ReactNode
  onReadyChange?: (ready: boolean) => void
  onUploadingChange?: (uploading: boolean) => void
}) {
  const input = (useWatch({ control: form.control, name: fieldRoot }) ?? {}) as LocalLifeInput
  const { types, enabled, isLoading, isError, defaultMixType, defaultVariantCount, maxVariantCount, defaultDurationSeconds, maxDurationSeconds, aspectRatios, typeByID } = useLocalLifeCapabilities()
  const selectedType = typeByID(input.mix_type)
  const assets = input.source_assets ?? []
  const catalogError = isLoading ? '正在加载素材混剪类型…' : isError ? '素材混剪类型加载失败，请稍后重试。' : !enabled ? '素材混剪当前未启用。' : !selectedType ? '当前类型已停用，请重新选择。' : ''
  const validationError = !input.brief?.trim()
    ? '请填写门店介绍要求'
    : assets.length === 0
      ? '请至少上传一条视频或图片素材'
      : input.variant_count < 1 || input.variant_count > maxVariantCount
        ? `变体数量需在 1-${maxVariantCount} 条之间`
        : input.target_duration_seconds < 10 || input.target_duration_seconds > maxDurationSeconds
          ? `目标时长需在 10-${maxDurationSeconds} 秒之间`
          : ''
  const ready = !catalogError && !validationError

  useEffect(() => {
    if (!input.mix_type && !form.getFieldState(`${fieldRoot}.mix_type`).isDirty) {
      form.setValue(`${fieldRoot}.mix_type`, defaultMixType, { shouldDirty: false })
    }
    if (!input.variant_count) form.setValue(`${fieldRoot}.variant_count`, defaultVariantCount, { shouldDirty: false })
    if (!input.target_duration_seconds) form.setValue(`${fieldRoot}.target_duration_seconds`, defaultDurationSeconds, { shouldDirty: false })
  }, [defaultDurationSeconds, defaultMixType, defaultVariantCount, fieldRoot, form, input.mix_type, input.target_duration_seconds, input.variant_count])

  useEffect(() => onReadyChange?.(ready), [onReadyChange, ready])

  const attachmentValue = useMemo(() => assets.map(toAttachment), [assets])
  return (
    <section className="space-y-4" aria-label="素材混剪设置">
      {catalogError ? <div role="alert" className="rounded border p-3 text-sm text-destructive">{catalogError}</div> : null}
      {briefField ?? (
        <FormField control={form.control} name={`${fieldRoot}.brief`} render={({ field }) => (
          <FormItem><FormLabel>门店介绍要求</FormLabel><FormControl><Textarea {...field} value={field.value ?? ''} rows={3} placeholder="例如：突出锅底、菜品特写和真实到店理由" /></FormControl><FormDescription>只会使用你提供的事实，未提供的价格、优惠、评价、功效和地点不会被补写。</FormDescription><FormMessage /></FormItem>
        )} />
      )}

      <FormField control={form.control} name={`${fieldRoot}.mix_type`} render={({ field }) => (
        <FormItem><FormLabel>内容类型</FormLabel><FormControl><NativeSelect {...field} value={field.value ?? ''} disabled={isLoading || isError || !enabled}>{types.map((item) => <NativeSelectOption key={item.id} value={item.id}>{item.name}</NativeSelectOption>)}</NativeSelect></FormControl>
          {selectedType ? <div className="rounded-md border bg-muted/20 p-3 text-xs text-muted-foreground"><p className="font-medium text-foreground">{selectedType.description}</p><p className="mt-1">适用场景：{selectedType.use_cases}</p><p className="mt-1">推荐素材：{selectedType.recommended_assets.join('、')}</p><p className="mt-1">镜头顺序：{selectedType.shot_order}</p><p className="mt-1">默认风格：{selectedType.voice_style}</p></div> : null}
          <FormMessage /></FormItem>
      )} />

      <FormField control={form.control} name={`${fieldRoot}.merchant_facts`} render={({ field }) => (
        <FormItem><FormLabel>商家事实（可选）</FormLabel><FormControl><Textarea {...field} value={field.value ?? ''} rows={3} placeholder="店名、地址、营业时间、已确认的价格或活动" /></FormControl><FormDescription>把可公开的信息写在这里，Agent 会把它们作为事实边界。</FormDescription><FormMessage /></FormItem>
      )} />

      <FormField control={form.control} name={`${fieldRoot}.source_assets`} render={({ field }) => (
        <FormItem><FormLabel>视频和照片素材</FormLabel><FormControl><ReferenceMaterialInput value={attachmentValue} onChange={(value) => field.onChange(value.map((item) => toAsset(item as AdapterAttachment)))} allowedTypes={['video', 'image']} maxCount={50} compact hint="支持门头、环境、商品特写和现场过程；素材不足时会允许重复使用，并在批次清单中提示。" uploadPurpose="montage_asset" onUploadingChange={onUploadingChange} /></FormControl><FormMessage /></FormItem>
      )} />

      <div className="grid gap-3 sm:grid-cols-3">
        <FormField control={form.control} name={`${fieldRoot}.variant_count`} render={({ field }) => <FormItem><FormLabel>视频条数</FormLabel><Input type="number" min={1} max={maxVariantCount} value={field.value ?? ''} onChange={(event) => field.onChange(Number(event.target.value) || 0)} /><FormDescription>默认 {defaultVariantCount} 条，最多 {maxVariantCount} 条。</FormDescription><FormMessage /></FormItem>} />
        <FormField control={form.control} name={`${fieldRoot}.target_duration_seconds`} render={({ field }) => <FormItem><FormLabel>单条目标时长（秒）</FormLabel><Input type="number" min={10} max={maxDurationSeconds} value={field.value ?? ''} onChange={(event) => field.onChange(Number(event.target.value) || 0)} /><FormDescription>默认 {defaultDurationSeconds} 秒，最长 {maxDurationSeconds} 秒。</FormDescription><FormMessage /></FormItem>} />
        <FormField control={form.control} name={`${fieldRoot}.aspect_ratio`} render={({ field }) => <FormItem><FormLabel>视频比例</FormLabel><NativeSelect {...field} value={field.value ?? '9:16'}>{aspectRatios.map((ratio) => <NativeSelectOption key={ratio} value={ratio}>{ratio}</NativeSelectOption>)}</NativeSelect><FormDescription>默认竖屏，按投放渠道选择。</FormDescription><FormMessage /></FormItem>} />
      </div>
      {validationError ? <p role="alert" className="text-sm text-destructive">{validationError}</p> : null}
    </section>
  )
}
