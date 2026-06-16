import { useState, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, Sparkles } from 'lucide-react'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/Select'
import {
  ToggleGroup,
  ToggleGroupItem,
} from '@/components/ui/toggle-group'
import { ReferenceImageUpload } from '@/components/channels/ReferenceImageUpload'
import type { Template, TemplateType, TemplateVisibility } from '@/types'
import { toast } from 'sonner'

const TYPE_OPTIONS: { value: TemplateType; label: string }[] = [
  { value: 'poster', label: '海报' },
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号' },
]

interface TemplateCreateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** When provided, edits this template instead of creating a new one. */
  template?: Template | null
  /** Default type for new templates (e.g. seeded from a task/plan context). */
  defaultType?: TemplateType
}

export function TemplateCreateDialog({
  open,
  onOpenChange,
  template,
  defaultType = 'seednote',
}: TemplateCreateDialogProps) {
  const queryClient = useQueryClient()
  const isEditing = !!template

  const [name, setName] = useState('')
  const [type, setType] = useState<TemplateType>(defaultType)
  const [thumbnailUrl, setThumbnailUrl] = useState('')
  const [stylePrompt, setStylePrompt] = useState('')
  const [visibility, setVisibility] = useState<TemplateVisibility>('public')
  const [analyzing, setAnalyzing] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  // Sync form state when opening.
  useEffect(() => {
    if (!open) return
    if (template) {
      setName(template.name)
      setType(template.type)
      setThumbnailUrl(template.thumbnail_url)
      setStylePrompt(template.style_prompt)
      setVisibility(template.visibility === 'private' ? 'private' : 'public')
    } else {
      setName('')
      setType(defaultType)
      setThumbnailUrl('')
      setStylePrompt('')
      setVisibility('public')
    }
  }, [open, template, defaultType])

  // Auto-analyze style whenever a new image is uploaded (skip while editing initial load).
  useEffect(() => {
    if (!open) return
    if (!thumbnailUrl) return
    // Skip if this URL was loaded from an existing template (avoid re-analyzing on edit open).
    if (template && thumbnailUrl === template.thumbnail_url) return
    let cancelled = false
    setAnalyzing(true)
    api.channels
      .analyzeImage(thumbnailUrl)
      .then((res) => {
        if (cancelled) return
        // Only auto-fill when the user hasn't typed anything — respect their input.
        if (res.style && !stylePrompt.trim()) {
          setStylePrompt(res.style)
        }
      })
      .catch(() => {
        if (cancelled) return
        // Silent failure — user can still type the style manually.
      })
      .finally(() => {
        if (!cancelled) setAnalyzing(false)
      })
    return () => {
      cancelled = true
    }
  }, [thumbnailUrl, open, template, stylePrompt])

  const createMutation = useMutation({
    mutationFn: (data: {
      name: string
      type: TemplateType
      thumbnail_url: string
      style_prompt: string
      visibility: TemplateVisibility
    }) => api.templates.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.templates.all })
      toast.success('模板已创建')
      onOpenChange(false)
    },
    onError: () => toast.error('创建模板失败，请重试'),
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string
      data: {
        name: string
        type: TemplateType
        thumbnail_url: string
        style_prompt: string
        visibility: TemplateVisibility
      }
    }) => api.templates.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.templates.all })
      toast.success('模板已更新')
      onOpenChange(false)
    },
    onError: () => toast.error('更新模板失败，请重试'),
  })

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast.error('请输入模板名称')
      return
    }
    if (!thumbnailUrl) {
      toast.error('请上传一张图片')
      return
    }
    setSubmitting(true)
    try {
      const payload = {
        name: name.trim(),
        type,
        thumbnail_url: thumbnailUrl,
        style_prompt: stylePrompt.trim(),
        visibility,
      }
      if (isEditing && template) {
        await updateMutation.mutateAsync({ id: template.id, data: payload })
      } else {
        await createMutation.mutateAsync(payload)
      }
    } finally {
      setSubmitting(false)
    }
  }

  const busy = submitting || createMutation.isPending || updateMutation.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEditing ? '编辑模板' : '新建模板'}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* Name */}
          <div className="space-y-1.5">
            <Label htmlFor="tpl-name">名称</Label>
            <Input
              id="tpl-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如：暖系生活感"
              maxLength={100}
            />
          </div>

          {/* Type */}
          <div className="space-y-1.5">
            <Label>类别</Label>
            <Select value={type} onValueChange={(v) => setType(v as TemplateType)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TYPE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value} label={opt.label}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Image */}
          <div className="space-y-1.5">
            <Label>图片</Label>
            <div className="flex items-start gap-3">
              <ReferenceImageUpload
                value={thumbnailUrl}
                onChange={setThumbnailUrl}
                purpose="reference"
              />
              <p className="text-xs text-muted-foreground pt-1">
                上传后系统会自动识别视觉风格，你也可以在下面手动调整。
              </p>
            </div>
          </div>

          {/* Style prompt */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <Label htmlFor="tpl-style">自动识别的风格</Label>
              {analyzing && (
                <span className="flex items-center gap-1 text-xs text-muted-foreground">
                  <Sparkles className="h-3 w-3" />
                  识别中…
                </span>
              )}
            </div>
            <Textarea
              id="tpl-style"
              value={stylePrompt}
              onChange={(e) => setStylePrompt(e.target.value)}
              placeholder="描述视觉风格（艺术流派、画面氛围、质感…）"
              rows={3}
              maxLength={1024}
            />
          </div>

          {/* Visibility */}
          <div className="space-y-1.5">
            <Label>可见性</Label>
            <ToggleGroup
              value={[visibility]}
              onValueChange={(vals) => {
                const v = vals[0]
                if (v === 'public' || v === 'private') setVisibility(v)
              }}
              variant="outline"
              className="w-full"
            >
              <ToggleGroupItem value="public" className="flex-1">
                公开（所有人可见）
              </ToggleGroupItem>
              <ToggleGroupItem value="private" className="flex-1">
                私有（仅自己）
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            取消
          </Button>
          <Button onClick={handleSubmit} disabled={busy || analyzing}>
            {busy ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                保存中
              </>
            ) : isEditing ? (
              '保存'
            ) : (
              '创建'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
