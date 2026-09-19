import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'

import { AnalyzedImageField } from '@/components/image-analysis/AnalyzedImageField'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import { referenceSelectionFromValue } from '@/lib/reference-image'
import {
  SEEDNOTE_TEMPLATE_CATEGORIES,
  type CreateTemplateRequest,
  type ImageAnalysis,
  type ReferenceImageValue,
  type SeednoteTemplateCategory,
  type Template,
  type TemplateVisibility,
  type UpdateTemplateRequest,
} from '@/types'
import { isImageAnalysisActive, isImageAnalysisUpdateOlder } from '@/types'

interface TemplateCreateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  template?: Template | null
}

export function TemplateCreateDialog({ open, onOpenChange, template }: TemplateCreateDialogProps) {
  const queryClient = useQueryClient()
  const isEditing = Boolean(template)
  const [name, setName] = useState('')
  const [category, setCategory] = useState<SeednoteTemplateCategory>(SEEDNOTE_TEMPLATE_CATEGORIES[0])
  const [thumbnail, setThumbnail] = useState<ReferenceImageValue | null>(null)
  const [prompt, setPrompt] = useState('')
  const [analysis, setAnalysis] = useState<ImageAnalysis | null>(null)
  const [visibility, setVisibility] = useState<TemplateVisibility>('public')
  const [sortOrder, setSortOrder] = useState(0)
  const [isActive, setIsActive] = useState(true)
  const [uploading, setUploading] = useState(false)
  const promptBaselineRef = useRef('')
  const analysisUpdatedAtRef = useRef('')

  useEffect(() => {
    if (!open) return
    setName(template?.name ?? '')
    setCategory(SEEDNOTE_TEMPLATE_CATEGORIES.includes(template?.category as SeednoteTemplateCategory)
      ? template!.category as SeednoteTemplateCategory
      : SEEDNOTE_TEMPLATE_CATEGORIES[0])
    setThumbnail(template?.thumbnail ?? null)
    setPrompt(template?.prompt ?? '')
    promptBaselineRef.current = template?.prompt ?? ''
    setAnalysis(template?.image_analysis ?? null)
    analysisUpdatedAtRef.current = template?.image_analysis?.updated_at ?? ''
    setVisibility(template?.visibility === 'private' ? 'private' : 'public')
    setSortOrder(template?.sort_order ?? 0)
    setIsActive(template?.activate_when_ready ?? template?.is_active ?? true)
    setUploading(false)
  }, [open, template])

  const analysisQuery = useQuery({
    queryKey: queryKeys.templates.detail(template?.id ?? ''),
    queryFn: ({ signal }) => api.templates.get(template!.id, signal),
    enabled: open && Boolean(template) && isImageAnalysisActive(analysis),
    staleTime: 0,
    refetchOnMount: 'always',
    refetchInterval: isImageAnalysisActive(analysis) ? 2000 : false,
  })

  useEffect(() => {
    const refreshed = analysisQuery.data
    if (!refreshed) return
    const updatedAt = refreshed.image_analysis?.updated_at ?? ''
    if (isImageAnalysisUpdateOlder(updatedAt, analysisUpdatedAtRef.current)) return
    analysisUpdatedAtRef.current = updatedAt
    setAnalysis(refreshed.image_analysis ?? null)
    setPrompt(refreshed.prompt)
    promptBaselineRef.current = refreshed.prompt
    setThumbnail(refreshed.thumbnail ?? null)
  }, [analysisQuery.data])

  const createMutation = useMutation({ mutationFn: (data: CreateTemplateRequest) => api.templates.create(data) })
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateTemplateRequest }) => api.templates.update(id, data),
  })

  const refreshCurrent = async () => {
    if (!template) return
    const refreshed = await api.templates.get(template.id)
    queryClient.setQueryData(queryKeys.templates.detail(template.id), refreshed)
    analysisUpdatedAtRef.current = refreshed.image_analysis?.updated_at ?? ''
    setAnalysis(refreshed.image_analysis ?? null)
    setPrompt(refreshed.prompt)
    promptBaselineRef.current = refreshed.prompt
    setThumbnail(refreshed.thumbnail ?? null)
    await queryClient.invalidateQueries({ queryKey: queryKeys.templates.all })
  }

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast.error('请填写模板名称')
      return
    }
    const thumbnailImage = referenceSelectionFromValue(thumbnail)
    if (!thumbnailImage) {
      toast.error('请上传缩略图')
      return
    }
    const promptValue = prompt.trim()
    const promptChanged = promptValue !== promptBaselineRef.current.trim()
    const normalizedSortOrder = Number.isFinite(sortOrder) ? sortOrder : 0
    try {
      if (isEditing && template) {
        const payload: UpdateTemplateRequest = {}
        if (name.trim() !== template.name) payload.name = name.trim()
        if (category !== template.category) payload.category = category
        if (JSON.stringify(thumbnailImage) !== JSON.stringify(referenceSelectionFromValue(template.thumbnail ?? null))) {
          payload.thumbnail_image = thumbnailImage
        }
        if (promptChanged) payload.prompt = promptValue
        if (visibility !== template.visibility) payload.visibility = visibility
        if (normalizedSortOrder !== template.sort_order) payload.sort_order = normalizedSortOrder
        if (isActive !== (template.activate_when_ready ?? template.is_active)) payload.is_active = isActive
        await updateMutation.mutateAsync({ id: template.id, data: payload })
      } else {
        const payload: CreateTemplateRequest = {
          name: name.trim(),
          type: 'seednote',
          category,
          thumbnail_image: thumbnailImage,
          prompt: promptValue || undefined,
          visibility,
          sort_order: normalizedSortOrder,
          is_active: isActive,
        }
        await createMutation.mutateAsync(payload)
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.templates.all })
      toast.success(isEditing
        ? '模板已更新'
        : prompt.trim() ? '模板已创建' : '模板已创建，正在后台识别')
      onOpenChange(false)
    } catch (error) {
      toast.error(getApiErrorMessage(error, isEditing ? '更新模板失败，请重试' : '创建模板失败，请重试'))
    }
  }

  const submitting = createMutation.isPending || updateMutation.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader><DialogTitle>{isEditing ? '编辑模板' : '新建模板'}</DialogTitle></DialogHeader>
        <div className="grid gap-4">
          <div className="space-y-1.5">
            <Label htmlFor="template-name">模板名称</Label>
            <Input id="template-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：清透成分说明书" maxLength={100} />
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="template-category">行业分类</Label>
              <Select items={SEEDNOTE_TEMPLATE_CATEGORIES.map((value) => ({ value, label: value }))} value={category} onValueChange={(value) => setCategory(value as SeednoteTemplateCategory)}>
                <SelectTrigger id="template-category" className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>{SEEDNOTE_TEMPLATE_CATEGORIES.map((value) => <SelectItem key={value} value={value} label={value}>{value}</SelectItem>)}</SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="template-sort-order">排序</Label>
              <Input id="template-sort-order" type="number" value={sortOrder} onChange={(event) => setSortOrder(Number(event.target.value))} />
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="template-prompt">缩略图与视觉版式 Prompt</Label>
            <AnalyzedImageField
              asset={thumbnail}
              onAssetChange={setThumbnail}
              purpose="template_thumbnail"
              text={prompt}
              onTextChange={setPrompt}
              analysis={analysis}
              textId="template-prompt"
              placeholder="可手动填写；留空则在创建后由后台识别视觉风格与版式"
              onUploadingChange={setUploading}
              onBeforeAnalysisAction={() => template
                ? queryClient.cancelQueries({ queryKey: queryKeys.templates.detail(template.id) })
                : undefined}
              onAnalysisAction={refreshCurrent}
            />
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex items-center justify-between rounded-md border border-border px-3 py-2">
              <Label htmlFor="template-public">公开模板</Label>
              <Switch id="template-public" checked={visibility === 'public'} onCheckedChange={(checked) => setVisibility(checked ? 'public' : 'private')} />
            </div>
            <div className="flex items-center justify-between rounded-md border border-border px-3 py-2">
              <Label htmlFor="template-active">识别完成后启用</Label>
              <Switch id="template-active" checked={isActive} onCheckedChange={setIsActive} />
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>取消</Button>
          <Button type="button" onClick={() => void handleSubmit()} disabled={submitting || uploading || analysis?.status === 'queued' || analysis?.status === 'running'}>
            {submitting ? <Loader2 className="animate-spin" /> : null}保存模板
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
