import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type Plan, type PlanType, type CreatePlanRequest } from '@/lib/api'
import { ChannelSelector } from '@/components/ChannelSelector'
import { Button } from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/Input'
import { Textarea } from '@/components/ui/Input'
import Select from '@/components/ui/Select'
import SchedulePicker from '@/components/SchedulePicker'
import { planStatusLabel, contentTypeLabel, contentTypeOptions, formatDateTimeCN } from '@/lib/labels'

function planStatusBadge(status: string) {
  return status === 'active' ? 'success' : 'neutral'
}

function cronToHuman(cron: string): string {
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return cron
  const [min, hourStr, , , weekday] = parts
  const time = `${hourStr.padStart(2, '0')}:${min.padStart(2, '0')}`
  if (weekday === '*') return `每天 ${time}`
  const dayNames = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']
  const dayList = weekday.split(',').map((d) => dayNames[Number(d)] ?? d).join('、')
  return `每${dayList} ${time}`
}


interface PlanFormData {
  type: PlanType
  title: string
  description: string
  cron_expr: string
  topic_hint: string
  channel_id: string
  channel_platform: string
}

const emptyForm: PlanFormData = {
  type: 'rednote',
  title: '',
  description: '',
  cron_expr: '0 9 * * 1,3,5',
  topic_hint: '',
  channel_id: '',
  channel_platform: '',
}

export default function PlansPage() {
  const queryClient = useQueryClient()
  const [channelFilter, setChannelFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null)
  const [form, setForm] = useState<PlanFormData>(emptyForm)
  const [formError, setFormError] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['plans', channelFilter],
    queryFn: () => api.plans.list({
      limit: 100,
      channel_id: channelFilter || undefined,
    }),
  })

  const plans = data?.items ?? []

  const createMutation = useMutation({
    mutationFn: (data: CreatePlanRequest) => api.plans.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      closeModal()
    },
    onError: () => {
      setFormError('创建计划失败，请检查输入。')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: CreatePlanRequest }) => api.plans.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      closeModal()
    },
    onError: () => {
      setFormError('更新计划失败，请检查输入。')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.plans.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
  })

  const pauseMutation = useMutation({
    mutationFn: (id: string) => api.plans.pause(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
  })

  const resumeMutation = useMutation({
    mutationFn: (id: string) => api.plans.resume(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
  })

  function openCreate() {
    setEditingPlan(null)
    setForm(emptyForm)
    setFormError('')
    setModalOpen(true)
  }

  function openEdit(plan: Plan) {
    setEditingPlan(plan)
    setForm({
      type: plan.type,
      title: plan.title,
      description: plan.description,
      cron_expr: plan.cron_expr,
      topic_hint: plan.topic_hint,
      channel_id: plan.channel_id || '',
      channel_platform: '',
    })
    setFormError('')
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingPlan(null)
    setForm(emptyForm)
    setFormError('')
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setFormError('')

    if (!form.title.trim()) {
      setFormError('标题不能为空。')
      return
    }
    if (!form.cron_expr.trim()) {
      setFormError('请设置排期。')
      return
    }

    const payload: CreatePlanRequest = {
      type: form.type,
      title: form.title.trim(),
      description: form.description.trim() || undefined,
      cron_expr: form.cron_expr.trim(),
      topic_hint: form.topic_hint.trim() || undefined,
      channel_id: form.channel_id || undefined,
    }

    if (editingPlan) {
      updateMutation.mutate({ id: editingPlan.id, data: payload })
    } else {
      createMutation.mutate(payload)
    }
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-100">计划</h1>
          <p className="mt-1 text-sm text-gray-400">管理你的内容计划，定时自动创作发布。</p>
        </div>
        <Button onClick={openCreate}>
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          新建计划
        </Button>
      </div>

      {/* Channel filter */}
      <div className="w-full sm:w-48">
        <ChannelSelector
          value={channelFilter}
          onChange={(id) => setChannelFilter(id)}
        />
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-16">
          <svg className="h-8 w-8 animate-spin text-blue-500" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
      ) : plans.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-xl border border-gray-700 bg-gray-800 py-16">
          <svg className="mb-4 h-12 w-12 text-gray-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
          </svg>
          <p className="text-sm text-gray-400">还没有计划</p>
          <p className="mt-1 text-xs text-gray-500">创建你的第一个内容计划，让 AI 定时帮你创作。</p>
        </div>
      ) : (
        <div className="space-y-3">
          {plans.map((plan) => (
            <Card key={plan.id}>
              <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <h3 className="truncate text-sm font-medium text-gray-100">{plan.title}</h3>
                    <Badge variant="outline" className="shrink-0 text-[10px]">
                      {contentTypeLabel[plan.type] || plan.type}
                    </Badge>
                    <Badge variant={planStatusBadge(plan.status)} className="shrink-0">
                      {planStatusLabel[plan.status] || plan.status}
                    </Badge>
                  </div>
                  {plan.description && (
                    <p className="mt-1 truncate text-xs text-gray-400">{plan.description}</p>
                  )}
                  <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500">
                    <span>{cronToHuman(plan.cron_expr)}</span>
                    {plan.topic_hint && <span>主题：{plan.topic_hint}</span>}
                  </div>
                  <p className="mt-1 text-xs text-gray-500">
                    下次执行：{formatDateTimeCN(plan.next_run_at)}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  {plan.status === 'active' && (
                    <Button variant="ghost" size="sm" onClick={() => pauseMutation.mutate(plan.id)}>
                      暂停
                    </Button>
                  )}
                  {plan.status === 'paused' && (
                    <Button variant="ghost" size="sm" onClick={() => resumeMutation.mutate(plan.id)}>
                      恢复
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" onClick={() => openEdit(plan)}>
                    编辑
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-red-400 hover:text-red-300"
                    onClick={() => {
                      if (window.confirm('确定删除此计划？此操作不可撤销。')) {
                        deleteMutation.mutate(plan.id)
                      }
                    }}
                  >
                    删除
                  </Button>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingPlan ? '编辑计划' : '新建计划'}</DialogTitle>
          </DialogHeader>
          <div className="max-h-[60vh] overflow-y-auto">
            <form onSubmit={handleSubmit} className="space-y-4">
              <div>
                <label className="mb-1.5 block text-sm font-medium text-gray-300">频道</label>
                <ChannelSelector
                  value={form.channel_id}
                  onChange={(id, platform) => {
                    setForm({
                      ...form,
                      channel_id: id,
                      channel_platform: id ? platform : '',
                      type: id ? (platform as PlanType) || form.type : form.type,
                    })
                  }}
                />
                <p className="mt-1 text-xs text-gray-500">选择频道以自动填充内容类型和配置。</p>
              </div>

              <Select
                label="内容类型"
                options={contentTypeOptions}
                value={form.type}
                onChange={(e) => setForm({ ...form, type: e.target.value as PlanType })}
              />

              <Input
                label="标题"
                placeholder="例如：每周小红书发布"
                value={form.title}
                onChange={(e) => setForm({ ...form, title: e.target.value })}
                required
              />

              <Textarea
                label="描述"
                placeholder="可选的计划描述"
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
              />

              <div>
                <label className="mb-1.5 block text-sm font-medium text-gray-300">排期设置</label>
                <SchedulePicker
                  value={form.cron_expr}
                  onChange={(cron) => setForm({ ...form, cron_expr: cron })}
                />
              </div>

              <Input
                label="主题方向（可选）"
                placeholder="例如：美妆技巧、科技评测"
                value={form.topic_hint}
                onChange={(e) => setForm({ ...form, topic_hint: e.target.value })}
              />

              {formError && (
                <div className="rounded-lg bg-red-900/50 px-3 py-2 text-sm text-red-300">
                  {formError}
                </div>
              )}
            </form>
          </div>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button onClick={handleSubmit} loading={isSubmitting}>
              {editingPlan ? '更新' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
