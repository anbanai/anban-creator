import { useEffect, useState, type ReactNode } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { hypitReadinessError } from '@/lib/hypit-form'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import { FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import type { HypitAsset, HypitInput, InputAttachment } from '@/types'

type HypitAttachment = InputAttachment & { hypitAsset?: HypitAsset }
function attachments(assets: HypitAsset[]): HypitAttachment[] {
  return assets.map(asset => ({ hypitAsset: asset, type: asset.type === 'video_url' ? 'video' : asset.type, url: asset.url, file_name: asset.file_name, content_type: asset.mime_type, size: asset.file_size }))
}
function assets(values: HypitAttachment[]): HypitAsset[] {
  return values.filter(v => ['image', 'video', 'audio'].includes(v.type)).map(v => ({ ...v.hypitAsset, type: v.hypitAsset?.type ?? v.type as HypitAsset['type'], url: v.url, file_name: v.file_name, mime_type: v.content_type, file_size: v.size }))
}
export function HypitCreationPanel({ form, defaults = false, remix = false, sourceTaskId, briefField, onReadyChange, onUploadingChange }: { form: UseFormReturn<any>; defaults?: boolean; remix?: boolean; sourceTaskId?: string; briefField?: ReactNode; onReadyChange?: (ready: boolean) => void; onUploadingChange?: (uploading: boolean) => void }) {
  const root = defaults ? 'hypit_defaults' : 'hypit_input'
  const input = useWatch({ control: form.control, name: root }) as HypitInput | undefined
  const query = useQuery({ queryKey: ['hypit-capabilities', sourceTaskId ?? null], queryFn: () => api.hypitCapabilities.list(sourceTaskId) })
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [assetsUploading, setAssetsUploading] = useState(false)
  const [referenceFailed, setReferenceFailed] = useState(false)
  const [assetsFailed, setAssetsFailed] = useState(false)
  const limits = query.data?.limits
  const unavailable = query.isPending ? '正在检查视频复刻能力…' : query.isError ? '能力检查失败，请点击重试。' : !query.data?.enabled ? '视频复刻尚未启用，请联系管理员开启内部验证。' : !query.data.configured ? '视频复刻配置未完成，请联系管理员补齐运行环境。' : ''
  const validation = limits && !defaults ? hypitReadinessError(input, limits, remix) : limits && (input?.preferences?.duration_seconds ?? 0) > limits.max_duration_seconds ? `时长不能超过 ${limits.max_duration_seconds} 秒` : ''
  useEffect(() => onReadyChange?.(!unavailable && !validation && !referenceFailed && !assetsFailed), [unavailable, validation, referenceFailed, assetsFailed, onReadyChange])
  useEffect(() => onUploadingChange?.(referenceUploading || assetsUploading), [referenceUploading, assetsUploading, onUploadingChange])
  return <section className="space-y-4" aria-label={defaults ? '视频复刻默认设置' : '视频复刻设置'}>
    {unavailable && <div role="alert" className="rounded border p-3 text-sm">{unavailable}{query.isError && <button type="button" onClick={() => query.refetch()}>重试</button>}</div>}
    {!defaults && <>{briefField != null ? <div className="space-y-2"><p className="text-sm font-medium">复刻要求</p>{briefField}</div> : <FormField control={form.control} name={`${root}.brief`} render={({ field }) => <FormItem><FormLabel>复刻要求</FormLabel><Textarea aria-label="复刻要求" {...field} value={field.value ?? ''} placeholder="描述希望保留的镜头、节奏及需要替换的内容" /><FormMessage /></FormItem>} />}
      <FormField control={form.control} name={`${root}.reference`} render={({ field }) => <FormItem><FormLabel>{remix ? '主参考视频（可选，沿用原工程）' : '主参考视频'}</FormLabel><Input aria-label="主参考视频链接" placeholder="https://… 主参考视频链接" value={field.value?.type === 'video_url' ? field.value.url ?? '' : ''} onChange={e => field.onChange(e.target.value ? { type: 'video_url', url: e.target.value } : undefined)} />
        <ReferenceMaterialInput value={field.value && field.value.type !== 'video_url' ? attachments([field.value]) : []} onChange={v => field.onChange(assets(v)[0])} allowedTypes={['video']} maxCount={1} maxFileBytes={limits?.max_asset_bytes} uploadPurpose="hypit_asset" onUploadingChange={setReferenceUploading} onFailuresChange={setReferenceFailed} hint="上传一个主参考视频，或填写上方链接。其他视频可放入下方补充素材。" /><FormMessage /></FormItem>} />
      <FormField control={form.control} name={`${root}.source_assets`} render={({ field }) => <FormItem><FormLabel>补充素材（可选）</FormLabel><ReferenceMaterialInput value={attachments(field.value ?? [])} onChange={v => field.onChange(assets(v))} allowedTypes={['image', 'video', 'audio']} maxCount={limits?.max_assets ?? 0} maxFileBytes={limits?.max_asset_bytes} uploadPurpose="hypit_asset" onUploadingChange={setAssetsUploading} onFailuresChange={setAssetsFailed} hint="支持图片、视频和音频，用于替换人物、产品、镜头或音频。" /></FormItem>} /></>}
    <div className="grid gap-3 sm:grid-cols-3">
      <FormField control={form.control} name={`${root}.preferences.duration_seconds`} render={({ field }) => <FormItem><FormLabel>{defaults ? '默认时长（可选）' : '目标时长（可选）'}</FormLabel><Input aria-label={defaults ? '默认时长（可选）' : '目标时长（可选）'} type="number" min={1} max={limits?.max_duration_seconds} value={field.value || ''} onChange={e => field.onChange(e.target.value ? Number(e.target.value) : undefined)} placeholder="跟随参考视频" /><FormDescription>单位：秒。留空跟随参考视频。</FormDescription><FormMessage /></FormItem>} />
      <FormField control={form.control} name={`${root}.preferences.aspect_ratio`} render={({ field }) => <FormItem><FormLabel>视频比例</FormLabel><select aria-label="复刻视频比例" className="h-9 rounded border bg-background px-2" {...field} value={field.value ?? 'source'}><option value="source">跟随参考视频</option> {['9:16', '16:9', '1:1'].map(v => <option key={v}>{v}</option>)}</select></FormItem>} />
      <FormField control={form.control} name={`${root}.preferences.language`} render={({ field }) => <FormItem><FormLabel>语言</FormLabel><Input aria-label="语言" {...field} value={field.value ?? ''} placeholder="跟随参考视频" /></FormItem>} />
    </div>
    {defaults && <FormField control={form.control} name={`${root}.asset_guidance`} render={({ field }) => <FormItem><FormLabel>素材使用说明</FormLabel><Textarea aria-label="素材使用说明" {...field} value={field.value ?? ''} /></FormItem>} />}
    {validation && <p role="alert" className="text-sm text-destructive">{validation}</p>}
  </section>
}
