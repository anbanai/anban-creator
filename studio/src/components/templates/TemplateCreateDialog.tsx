import { useCallback, useEffect, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCw, Sparkles } from 'lucide-react'
import { toast } from 'sonner'

import { ReferenceImageUpload } from '@/components/projects/ReferenceImageUpload'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import {
  SEEDNOTE_TEMPLATE_CATEGORIES,
  type CreateTemplateRequest,
  type SeednoteTemplateCategory,
  type Template,
  type TemplateVisibility,
  type UpdateTemplateRequest,
} from '@/types'

interface TemplateCreateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  template?: Template | null
}

export function TemplateCreateDialog({
  open,
  onOpenChange,
  template,
}: TemplateCreateDialogProps) {
  const queryClient = useQueryClient()
  const isEditing = Boolean(template)
  const [name, setName] = useState('')
  const [category, setCategory] = useState<SeednoteTemplateCategory>(SEEDNOTE_TEMPLATE_CATEGORIES[0])
  const [thumbnailUrl, setThumbnailUrl] = useState('')
  const [prompt, setPrompt] = useState('')
  const [visibility, setVisibility] = useState<TemplateVisibility>('public')
  const [sortOrder, setSortOrder] = useState(0)
  const [isActive, setIsActive] = useState(true)
  const [analyzing, setAnalyzing] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const sessionEpochRef = useRef(0)
  const analysisRequestRef = useRef(0)
  const promptEditVersionRef = useRef(0)

  useEffect(() => {
    if (!open) {
      setAnalyzing(false)
      setSubmitting(false)
      return
    }

    sessionEpochRef.current += 1
    analysisRequestRef.current += 1
    promptEditVersionRef.current = 0
    setAnalyzing(false)
    setSubmitting(false)
    setName(template?.name ?? '')
    setCategory(
      SEEDNOTE_TEMPLATE_CATEGORIES.includes(template?.category as SeednoteTemplateCategory)
        ? template!.category as SeednoteTemplateCategory
        : SEEDNOTE_TEMPLATE_CATEGORIES[0],
    )
    setThumbnailUrl(template?.thumbnail_url ?? '')
    setPrompt(template?.prompt ?? '')
    setVisibility(template?.visibility === 'private' ? 'private' : 'public')
    setSortOrder(template?.sort_order ?? 0)
    setIsActive(template?.is_active ?? true)
  }, [open, template])

  const analyzeThumbnail = useCallback(async (imageUrl: string, overwriteManualPrompt = false) => {
    if (!imageUrl) return
    const requestId = ++analysisRequestRef.current
    const sessionEpoch = sessionEpochRef.current
    const promptEditVersion = promptEditVersionRef.current
    setAnalyzing(true)
    try {
      const result = await api.templates.analyzeThumbnail({
        type: 'seednote',
        thumbnail_url: imageUrl,
      })
      if (
        requestId !== analysisRequestRef.current
        || sessionEpoch !== sessionEpochRef.current
        || promptEditVersionRef.current !== promptEditVersion
        || (!overwriteManualPrompt && promptEditVersion !== 0)
      ) return
      setPrompt(result.prompt)
    } catch (error) {
      if (requestId !== analysisRequestRef.current || sessionEpoch !== sessionEpochRef.current) return
      toast.error(getApiErrorMessage(error, '缩略图分析失败，请手动填写或重试'))
    } finally {
      if (requestId === analysisRequestRef.current && sessionEpoch === sessionEpochRef.current) {
        setAnalyzing(false)
      }
    }
  }, [])

  useEffect(() => {
    if (!open || !thumbnailUrl) return
    if (template && thumbnailUrl === template.thumbnail_url) return
    void analyzeThumbnail(thumbnailUrl)
  }, [analyzeThumbnail, open, template, thumbnailUrl])

  const createMutation = useMutation({
    mutationFn: (data: CreateTemplateRequest) => api.templates.create(data),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateTemplateRequest }) => api.templates.update(id, data),
  })

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast.error('请填写模板名称')
      return
    }
    if (!thumbnailUrl) {
      toast.error('请上传缩略图')
      return
    }
    if (!prompt.trim()) {
      toast.error('请填写视觉与版式 Prompt')
      return
    }

    const payload: CreateTemplateRequest = {
      name: name.trim(),
      type: 'seednote',
      category,
      thumbnail_url: thumbnailUrl,
      prompt: prompt.trim(),
      visibility,
      sort_order: Number.isFinite(sortOrder) ? sortOrder : 0,
      is_active: isActive,
    }
    const sessionEpoch = sessionEpochRef.current
    setSubmitting(true)
    try {
      if (isEditing && template) {
        await updateMutation.mutateAsync({ id: template.id, data: payload })
      } else {
        await createMutation.mutateAsync(payload)
      }
      if (sessionEpoch !== sessionEpochRef.current) return
      await queryClient.invalidateQueries({ queryKey: queryKeys.templates.all })
      toast.success(isEditing ? '模板已更新' : '模板已创建')
      onOpenChange(false)
    } catch (error) {
      if (sessionEpoch !== sessionEpochRef.current) return
      toast.error(getApiErrorMessage(error, isEditing ? '更新模板失败，请重试' : '创建模板失败，请重试'))
    } finally {
      if (sessionEpoch === sessionEpochRef.current) setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{isEditing ? '编辑模板' : '新建模板'}</DialogTitle>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="space-y-1.5">
            <Label htmlFor="template-name">模板名称</Label>
            <Input
              id="template-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="例如：清透成分说明书"
              maxLength={100}
            />
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="template-category">行业分类</Label>
              <Select
                items={SEEDNOTE_TEMPLATE_CATEGORIES.map((value) => ({ value, label: value }))}
                value={category}
                onValueChange={(value) => setCategory(value as SeednoteTemplateCategory)}
              >
                <SelectTrigger id="template-category" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SEEDNOTE_TEMPLATE_CATEGORIES.map((value) => (
                    <SelectItem
                      key={value}
                      value={value}
                      label={value}
                      onClick={() => setCategory(value)}
                    >
                      {value}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="template-sort-order">排序</Label>
              <Input
                id="template-sort-order"
                type="number"
                value={sortOrder}
                onChange={(event) => setSortOrder(Number(event.target.value))}
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between gap-3">
              <Label htmlFor="template-prompt">视觉与版式 Prompt</Label>
              {thumbnailUrl ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={analyzing}
                  onClick={() => void analyzeThumbnail(thumbnailUrl, true)}
                >
                  {analyzing ? <Sparkles className="animate-pulse" /> : <RefreshCw />}
                  {analyzing ? '分析中' : '重新分析'}
                </Button>
              ) : null}
            </div>
            <div className="grid gap-3 sm:grid-cols-[8rem_1fr]">
              <ReferenceImageUpload
                value={thumbnailUrl}
                onChange={setThumbnailUrl}
                purpose="project"
              />
              <Textarea
                id="template-prompt"
                value={prompt}
                onChange={(event) => {
                  promptEditVersionRef.current += 1
                  setPrompt(event.target.value)
                }}
                placeholder="描述视觉风格、封面构图、内容页版式、字体层级、页面节奏、图片处理和信息密度"
                maxLength={4000}
                className="min-h-32 resize-y"
              />
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex items-center justify-between rounded-md border border-border px-3 py-2">
              <Label htmlFor="template-public">公开模板</Label>
              <Switch
                id="template-public"
                checked={visibility === 'public'}
                onCheckedChange={(checked) => setVisibility(checked ? 'public' : 'private')}
              />
            </div>
            <div className="flex items-center justify-between rounded-md border border-border px-3 py-2">
              <Label htmlFor="template-active">启用模板</Label>
              <Switch id="template-active" checked={isActive} onCheckedChange={setIsActive} />
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            取消
          </Button>
          <Button type="button" onClick={() => void handleSubmit()} disabled={submitting || analyzing}>
            {submitting ? <Loader2 className="animate-spin" /> : null}
            保存模板
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
