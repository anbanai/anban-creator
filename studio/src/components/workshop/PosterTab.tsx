import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, Trash2, ImageIcon, SkipForward, Wand2, MessageCircle } from 'lucide-react'
import { api } from '@/lib/api'
import type { Template, CreatePosterRequest } from '@/types'
import { getApiErrorMessage } from '@/lib/http-client'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import EmptyState from '@/components/EmptyState'

export default function PosterTab() {
  // Template selection
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null)
  const [showTemplates, setShowTemplates] = useState(true)

  // Form fields
  const [title, setTitle] = useState('')
  const [sellingPoints, setSellingPoints] = useState<string[]>([''])
  const [brand, setBrand] = useState('')
  const [price, setPrice] = useState('')
  const [stylePreference, setStylePreference] = useState('')

  // Poster task state
  const [currentTaskId, setCurrentTaskId] = useState<string | null>(null)

  // Templates query
  const { data: templatesData, isLoading: templatesLoading } = useQuery({
    queryKey: ['templates', 'poster'],
    queryFn: () => api.templates.list({ type: 'poster', limit: 20 }),
  })

  const posterTemplates = templatesData?.items ?? []

  // Current task query
  const { data: currentTask, isLoading: taskLoading } = useQuery({
    queryKey: ['posters', currentTaskId],
    queryFn: () => api.posters.get(currentTaskId!),
    enabled: Boolean(currentTaskId),
    refetchInterval: (query) => {
      const task = query.state.data
      if (!task) return false
      if (task.status === 'completed' || task.status === 'failed') return false
      return 3000
    },
  })

  const createMutation = useMutation({
    mutationFn: (data: CreatePosterRequest) => api.posters.create(data),
    onSuccess: (task) => {
      toast.success('海报生成任务已创建')
      setCurrentTaskId(task.id)
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '创建海报失败，请重试'))
    },
  })

  function handleGenerate() {
    if (!title.trim()) {
      toast.error('请输入标题')
      return
    }
    const payload: CreatePosterRequest = {
      template_id: selectedTemplate?.id,
      input_content: {
        title: title.trim(),
        selling_points: sellingPoints.filter((s) => s.trim()),
        brand: brand.trim(),
        price: price.trim() || undefined,
      },
      style_preference: stylePreference.trim(),
    }
    createMutation.mutate(payload)
  }

  function handleAddSellingPoint() {
    setSellingPoints([...sellingPoints, ''])
  }

  function handleRemoveSellingPoint(index: number) {
    setSellingPoints(sellingPoints.filter((_, i) => i !== index))
  }

  function handleSellingPointChange(index: number, value: string) {
    const next = [...sellingPoints]
    next[index] = value
    setSellingPoints(next)
  }


  return (
    <div className="flex h-full gap-4">
      {/* Left panel - Template selection */}
      {showTemplates && (
        <div className="hidden w-56 shrink-0 flex-col rounded-lg border border-border bg-card md:flex">
          <div className="flex items-center justify-between border-b border-border px-3 py-3">
            <h3 className="text-sm font-medium text-foreground">模板</h3>
            <Button variant="ghost" size="sm" onClick={() => setShowTemplates(false)}>
              <SkipForward className="h-3.5 w-3.5" />
              跳过
            </Button>
          </div>
          <ScrollArea className="flex-1">
            {templatesLoading ? (
              <div className="space-y-3 p-3">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="aspect-[3/4] w-full rounded-md" />
                ))}
              </div>
            ) : posterTemplates.length === 0 ? (
              <p className="px-3 py-8 text-center text-xs text-muted-foreground">
                暂无模板
              </p>
            ) : (
              <div className="grid grid-cols-2 gap-2 p-2">
                {posterTemplates.map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    onClick={() => setSelectedTemplate(template)}
                    className={`group relative overflow-hidden rounded-md border transition-all ${
                      selectedTemplate?.id === template.id
                        ? 'border-primary ring-1 ring-primary/30'
                        : 'border-border hover:border-primary/50'
                    }`}
                  >
                    {template.thumbnail_url ? (
                      <img
                        src={template.thumbnail_url}
                        alt={template.name}
                        className="aspect-[3/4] w-full object-cover"
                      />
                    ) : (
                      <div className="flex aspect-[3/4] w-full items-center justify-center bg-muted">
                        <ImageIcon className="h-5 w-5 text-muted-foreground" />
                      </div>
                    )}
                    <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/60 to-transparent px-1.5 pb-1.5 pt-4">
                      <p className="truncate text-[11px] font-medium text-white">{template.name}</p>
                    </div>
                  </button>
                ))}
              </div>
            )}
          </ScrollArea>
        </div>
      )}

      {/* Main panel */}
      <div className="flex min-w-0 flex-1 flex-col gap-4 lg:flex-row">
        {/* Form + Images */}
        <div className="flex flex-1 flex-col gap-4">
          {/* Form */}
          <div className="space-y-4 rounded-lg border border-border bg-card p-4">
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">标题</label>
              <Input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="海报标题，如：夏日清凉特惠"
              />
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">卖点</label>
              <div className="space-y-1.5">
                {sellingPoints.map((point, i) => (
                  <div key={i} className="flex gap-1.5">
                    <Input
                      value={point}
                      onChange={(e) => handleSellingPointChange(i, e.target.value)}
                      placeholder={`卖点 ${i + 1}`}
                      className="flex-1"
                    />
                    {sellingPoints.length > 1 && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleRemoveSellingPoint(i)}
                        className="shrink-0 text-muted-foreground hover:text-destructive"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    )}
                  </div>
                ))}
                <Button variant="outline" size="sm" onClick={handleAddSellingPoint}>
                  <Plus className="h-3.5 w-3.5" />
                  添加卖点
                </Button>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">品牌</label>
                <Input
                  value={brand}
                  onChange={(e) => setBrand(e.target.value)}
                  placeholder="品牌名称"
                />
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">价格</label>
                <Input
                  value={price}
                  onChange={(e) => setPrice(e.target.value)}
                  placeholder="价格，如 ￥99"
                />
              </div>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">风格偏好</label>
              <Input
                value={stylePreference}
                onChange={(e) => setStylePreference(e.target.value)}
                placeholder="如：简约大气、可爱风、科技感"
              />
            </div>

            {!showTemplates && selectedTemplate && (
              <div className="flex items-center gap-2 rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
                <ImageIcon className="h-3.5 w-3.5" />
                已选模板：{selectedTemplate.name}
                <button
                  type="button"
                  onClick={() => setSelectedTemplate(null)}
                  className="ml-auto text-muted-foreground hover:text-foreground"
                >
                  更换
                </button>
              </div>
            )}

            <Button onClick={handleGenerate} loading={createMutation.isPending} className="w-full">
              <Wand2 className="h-4 w-4" />
              生成海报
            </Button>
          </div>

          {/* Generated images */}
          {taskLoading && (
            <div className="grid grid-cols-2 gap-3">
              {Array.from({ length: 2 }).map((_, i) => (
                <Skeleton key={i} className="aspect-[3/4] w-full rounded-lg" />
              ))}
            </div>
          )}

          {currentTask?.images && currentTask.images.length > 0 && (
            <div className="space-y-2">
              <h3 className="text-sm font-medium text-foreground">生成结果</h3>
              <div className="grid grid-cols-2 gap-3">
                {currentTask.images.map((img, i) => (
                  <img
                    key={i}
                    src={img.url}
                    alt={`海报 ${i + 1}`}
                    className="w-full rounded-lg border border-border object-cover"
                  />
                ))}
              </div>
            </div>
          )}

          {!currentTask && !createMutation.isPending && (
            <EmptyState
              icon={ImageIcon}
              title="海报制作"
              description="填写产品信息，选择模板风格，一键生成营销海报。"
            />
          )}
        </div>

        {/* Chat area for iteration */}
        {currentTask && (
          <div className="flex w-full flex-col lg:w-80 xl:w-96">
            <div className="flex h-[400px] items-center justify-center rounded-lg border border-border bg-card lg:h-full">
              <EmptyState
                icon={MessageCircle}
                title="对话迭代"
                description="通过对话调整海报风格和细节，该功能正在开发中。"
              />
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
