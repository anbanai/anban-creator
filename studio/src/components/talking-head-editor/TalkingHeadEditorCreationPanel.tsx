import { useEffect, useState, type ReactNode } from 'react'
import { useWatch, type UseFormReturn } from 'react-hook-form'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { Input } from '@/components/ui/input'
import { ReferenceMaterialInput } from '@/components/ReferenceMaterialInput'
import type { InputAttachment } from '@/types/input-attachment'

interface Props { form: UseFormReturn<any>; briefField?: ReactNode; onUploadingChange?: (uploading: boolean) => void; onReadyChange?: (ready: boolean) => void }

export function TalkingHeadEditorCreationPanel({ form, briefField, onUploadingChange, onReadyChange }: Props) {
  const primary = useWatch({ control: form.control, name: 'talking_head_editor_input.primary_video' })
  const [failures, setFailures] = useState(false)
  useEffect(() => onReadyChange?.(Boolean(primary?.asset_id || primary?.url) && !failures), [primary, failures, onReadyChange])
  const toAttachment = (asset: any): InputAttachment => ({ type: asset.type ?? (asset.mime_type?.startsWith('audio/') ? 'audio' : 'video'), asset_id: asset.asset_id, upload_id: asset.asset_id, url: asset.url, file_name: asset.file_name, content_type: asset.mime_type, size: asset.file_size })
  const toAsset = (attachment: InputAttachment, role: 'broll' | 'sfx' | 'bgm' | 'reference') => ({ role, asset_id: attachment.upload_id, url: attachment.url, file_name: attachment.file_name, mime_type: attachment.content_type, file_size: attachment.size })
  return <div className="space-y-4">
    <aside className="rounded-md border border-border bg-muted/30 p-3 text-sm" aria-label="video-use 配置引导">
      <p className="font-medium">首次使用前确认 Server 已配置 video-use</p>
      <p className="mt-1 text-xs text-muted-foreground">API Key 仅由 Server 注入口播精剪运行时，Studio 不显示也不保存密钥。缺少配置时任务会阻塞并提示管理员处理。</p>
      <a className="mt-2 inline-flex text-xs font-medium text-primary underline underline-offset-4" href="https://github.com/browser-use/video-use#api-key" target="_blank" rel="noreferrer">查看官方授权与 API Key 说明</a>
    </aside>
    {briefField ?? <FormField control={form.control} name="talking_head_editor_input.brief" render={({ field }) => <FormItem><FormLabel>剪辑要求</FormLabel><FormControl><Textarea {...field} rows={4} placeholder="例如：保留自然表达，突出核心观点，动效服务于话术" /></FormControl><FormMessage /></FormItem>} />}
    <FormField control={form.control} name="talking_head_editor_input.primary_video" render={({ field }) => <FormItem><FormLabel>主口播视频</FormLabel><FormControl><ReferenceMaterialInput value={field.value?.asset_id || field.value?.url ? [toAttachment(field.value)] : []} onChange={(next) => field.onChange(next[0] ? { ...toAsset(next[0], 'reference'), role: 'reference' } : undefined)} allowedTypes={['video']} maxCount={1} maxFileBytes={1024 * 1024 * 1024} compact uploadPurpose="talking_head_editor_asset" hint="上传一条已经录好的口播视频。" onUploadingChange={onUploadingChange} onFailuresChange={setFailures} /></FormControl><FormDescription>建议使用固定机位、4K、提词器靠近摄像头的原始视频。</FormDescription><FormMessage /></FormItem>} />
    <FormField control={form.control} name="talking_head_editor_input.supporting_assets" render={({ field }) => <FormItem><FormLabel>辅助素材</FormLabel><div className="space-y-3">{(['broll', 'sfx', 'bgm', 'reference'] as const).map((role) => { const roleAssets = (field.value ?? []).filter((item: any) => item.role === role); return <div key={role}><p className="mb-1 text-xs text-muted-foreground">{role === 'broll' ? 'B-roll' : role === 'sfx' ? '音效库' : role === 'bgm' ? 'BGM' : '风格/参考视频'}</p><ReferenceMaterialInput value={roleAssets.map(toAttachment)} onChange={(next) => field.onChange([...(field.value ?? []).filter((item: any) => item.role !== role), ...next.map((item) => toAsset(item, role))])} allowedTypes={role === 'bgm' || role === 'sfx' ? ['audio'] : role === 'broll' ? ['video', 'image'] : ['video', 'image']} maxCount={role === 'reference' ? 3 : 20} maxFileBytes={512 * 1024 * 1024} compact uploadPurpose="talking_head_editor_asset" hint="可选素材" onUploadingChange={onUploadingChange} onFailuresChange={setFailures} /></div> })}</div><FormMessage /></FormItem>} />
    <section className="space-y-3 border-t border-border pt-4" aria-label="口播精剪偏好"><h3 className="text-sm font-medium">口播精剪偏好</h3><div className="grid gap-3 sm:grid-cols-2">
      <FormField control={form.control} name="talking_head_editor_input.preferences.quality_tier" render={({ field }) => <FormItem><FormLabel>质量档位</FormLabel><FormControl><NativeSelect value={field.value ?? 'balanced'} onChange={(e) => field.onChange(e.target.value)}><NativeSelectOption value="effective">高效</NativeSelectOption><NativeSelectOption value="balanced">平衡</NativeSelectOption><NativeSelectOption value="quality">高质量</NativeSelectOption></NativeSelect></FormControl></FormItem>} />
      <FormField control={form.control} name="talking_head_editor_input.preferences.subtitle_mode" render={({ field }) => <FormItem><FormLabel>字幕</FormLabel><FormControl><NativeSelect value={field.value ?? 'required'} onChange={(e) => field.onChange(e.target.value)}><NativeSelectOption value="required">生成字幕</NativeSelectOption><NativeSelectOption value="off">关闭</NativeSelectOption></NativeSelect></FormControl></FormItem>} />
      <FormField control={form.control} name="talking_head_editor_input.preferences.target_duration_seconds" render={({ field }) => <FormItem><FormLabel>目标时长（秒）</FormLabel><FormControl><Input type="number" min={0} max={1800} value={field.value ?? ''} onChange={(e) => field.onChange(e.target.value ? Number(e.target.value) : undefined)} /></FormControl></FormItem>} />
      <FormField control={form.control} name="talking_head_editor_input.preferences.aspect_ratio" render={({ field }) => <FormItem><FormLabel>画幅</FormLabel><FormControl><NativeSelect value={field.value ?? '9:16'} onChange={(e) => field.onChange(e.target.value)}><NativeSelectOption value="9:16">9:16</NativeSelectOption><NativeSelectOption value="16:9">16:9</NativeSelectOption><NativeSelectOption value="1:1">1:1</NativeSelectOption></NativeSelect></FormControl></FormItem>} />
    </div><FormField control={form.control} name="talking_head_editor_input.preferences.style" render={({ field }) => <FormItem><FormLabel>本次风格</FormLabel><FormControl><Textarea {...field} rows={3} maxLength={2000} placeholder="例如：动效密集但不遮挡人声，保留自然节奏" /></FormControl><FormMessage /></FormItem>} /></section>
  </div>
}
