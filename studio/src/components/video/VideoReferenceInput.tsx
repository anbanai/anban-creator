import { useRef, useState } from 'react'
import { FileVideo, Loader2, Plus, Trash2, Upload } from 'lucide-react'
import { Button } from '@/components/common/button'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { http } from '@/lib/http-client'
import type { VideoReferenceAsset, VideoReferenceType } from '@/types'

const referenceRoles = [
  { value: 'subject identity', label: '主体一致性' },
  { value: 'product appearance', label: '产品外观' },
  { value: 'scene background', label: '场景背景' },
  { value: 'first frame', label: '首帧' },
  { value: 'last frame', label: '尾帧' },
  { value: 'action', label: '动作' },
  { value: 'camera movement', label: '镜头运动' },
  { value: 'rhythm', label: '节奏' },
  { value: 'voice tone', label: '声音/BGM' },
]

function referenceTypeForFile(file: File): VideoReferenceType {
  if (file.type.startsWith('audio/')) return 'audio_url'
  if (file.type.startsWith('video/')) return 'video_url'
  const ext = file.name.toLowerCase().split('.').pop() || ''
  if (['mp3', 'wav', 'm4a', 'aac', 'ogg'].includes(ext)) return 'audio_url'
  if (['mp4', 'mov', 'webm', 'm4v'].includes(ext)) return 'video_url'
  return 'image_url'
}

function referenceTypeLabel(type: string) {
  if (type === 'text') return '文本'
  if (type === 'audio_url') return '音频'
  if (type === 'video_url') return '视频'
  return '图片'
}

export function VideoReferenceInput({
  value = [],
  onChange,
}: {
  value?: VideoReferenceAsset[]
  onChange: (value: VideoReferenceAsset[]) => void
}) {
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')
  const [textRef, setTextRef] = useState('')
  const [textRole, setTextRole] = useState(referenceRoles[0].value)

  const updateAt = (index: number, patch: Partial<VideoReferenceAsset>) => {
    const next = value.map((item, i) => i === index ? { ...item, ...patch } : item)
    onChange(next)
  }

  const removeAt = (index: number) => {
    onChange(value.filter((_, i) => i !== index))
  }

  const uploadFile = async (file: File) => {
    setUploading(true)
    setError('')
    try {
      const form = new FormData()
      form.append('file', file)
      form.append('purpose', 'video_reference')
      const res = await http.post('/files/upload', form, {
        headers: { 'Content-Type': 'multipart/form-data' },
      })
      const data = res.data?.data ?? res.data
      onChange([
        ...value,
        {
          type: referenceTypeForFile(file),
          url: data.url,
          reference_role: referenceRoles[0].value,
          file_name: file.name,
          mime_type: data.type || file.type,
          file_size: data.size || file.size,
          input_duration_seconds: data.input_duration_seconds,
        },
      ])
    } catch (err: any) {
      setError(err?.response?.data?.msg || '上传失败，请重试')
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  const addTextReference = () => {
    const text = textRef.trim()
    if (!text) return
    onChange([...value, { type: 'text', text, reference_role: textRole }])
    setTextRef('')
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*,audio/*,video/*"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0]
            if (file) void uploadFile(file)
          }}
        />
        <Button type="button" variant="outline" size="sm" disabled={uploading} onClick={() => fileInputRef.current?.click()}>
          {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
          上传素材
        </Button>
        <span className="text-xs text-muted-foreground">支持图片、音频、视频，用于主体、产品、场景或动作一致性。</span>
      </div>
      {error && <p className="text-xs text-destructive">{error}</p>}

      <div className="grid gap-2 sm:grid-cols-[1fr_180px_auto]">
        <Textarea
          value={textRef}
          onChange={(event) => setTextRef(event.target.value)}
          placeholder="添加文本参考，例如品牌禁忌、产品锚点、不可改变的外观特征"
          className="min-h-[64px] resize-y"
        />
        <Select value={textRole} onValueChange={(role) => setTextRole(role || referenceRoles[0].value)}>
          <SelectTrigger><SelectValue /></SelectTrigger>
          <SelectContent>
            {referenceRoles.map((role) => (
              <SelectItem key={role.value} value={role.value}>{role.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="button" variant="secondary" onClick={addTextReference} disabled={!textRef.trim()}>
          <Plus className="h-4 w-4" />
          添加
        </Button>
      </div>

      {value.length > 0 && (
        <div className="divide-y divide-border rounded-lg border border-border">
          {value.map((ref, index) => (
            <div key={`${ref.type}-${ref.url || ref.text}-${index}`} className="grid gap-2 p-3 sm:grid-cols-[1fr_180px_auto] sm:items-center">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <FileVideo className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="text-xs text-muted-foreground">{referenceTypeLabel(ref.type)}</span>
                  <span className="truncate text-sm text-foreground">{ref.file_name || ref.text || ref.url}</span>
                </div>
                {ref.url && <p className="mt-1 truncate text-xs text-muted-foreground">{ref.url}</p>}
              </div>
              <Select value={ref.reference_role || referenceRoles[0].value} onValueChange={(role) => updateAt(index, { reference_role: role || referenceRoles[0].value })}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  {referenceRoles.map((role) => (
                    <SelectItem key={role.value} value={role.value}>{role.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button type="button" variant="ghost" size="sm" onClick={() => removeAt(index)} aria-label="移除参考素材">
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
