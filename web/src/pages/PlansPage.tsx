import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type Plan, type PlanType, type CreatePlanRequest } from '@/lib/api'
import Button from '@/components/ui/Button'
import Badge from '@/components/ui/Badge'
import { Card } from '@/components/ui/Card'
import Modal from '@/components/ui/Modal'
import { Input } from '@/components/ui/Input'
import { Textarea } from '@/components/ui/Input'
import Select from '@/components/ui/Select'

const planTypeOptions = [
  { value: 'rednote', label: 'RedNote (Xiaohongshu)' },
  { value: 'article', label: 'Article (WeChat)' },
  { value: 'xls', label: 'XLS (Xiaolvshu)' },
]

function planStatusBadge(status: string) {
  return status === 'active' ? 'success' : 'neutral'
}

function cronToHuman(cron: string): string {
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return cron
  const [min, hour, , , weekday] = parts

  const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
  const dayList = weekday === '*' ? 'every day'
    : weekday.split(',').map((d) => dayNames[Number(d)] ?? d).join(', ')

  return `Every ${dayList} at ${hour.padStart(2, '0')}:${min.padStart(2, '0')}`
}

function formatNextRun(dateStr: string | null | undefined): string {
  if (!dateStr) return '--'
  const d = new Date(dateStr)
  return d.toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

interface PlanFormData {
  type: PlanType
  title: string
  description: string
  cron_expr: string
  topic_hint: string
}

const emptyForm: PlanFormData = {
  type: 'rednote',
  title: '',
  description: '',
  cron_expr: '0 9 * * 1,3,5',
  topic_hint: '',
}

export default function PlansPage() {
  const queryClient = useQueryClient()
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null)
  const [form, setForm] = useState<PlanFormData>(emptyForm)
  const [formError, setFormError] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['plans'],
    queryFn: () => api.plans.list({ limit: 100 }),
  })

  const plans = data?.items ?? []

  const createMutation = useMutation({
    mutationFn: (data: CreatePlanRequest) => api.plans.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      closeModal()
    },
    onError: () => {
      setFormError('Failed to create plan. Please check your input.')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: CreatePlanRequest }) => api.plans.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      closeModal()
    },
    onError: () => {
      setFormError('Failed to update plan. Please check your input.')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.plans.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
  })

  const pauseMutation = useMutation({
    mutationFn: (id: number) => api.plans.pause(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
  })

  const resumeMutation = useMutation({
    mutationFn: (id: number) => api.plans.resume(id),
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
      setFormError('Title is required.')
      return
    }
    if (!form.cron_expr.trim()) {
      setFormError('Cron expression is required.')
      return
    }

    const payload: CreatePlanRequest = {
      type: form.type,
      title: form.title.trim(),
      description: form.description.trim() || undefined,
      cron_expr: form.cron_expr.trim(),
      topic_hint: form.topic_hint.trim() || undefined,
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
          <h1 className="text-2xl font-bold text-gray-100">Plans</h1>
          <p className="mt-1 text-sm text-gray-400">Manage your content plans and schedules.</p>
        </div>
        <Button onClick={openCreate}>
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          New Plan
        </Button>
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
          <p className="text-sm text-gray-400">No plans yet</p>
          <p className="mt-1 text-xs text-gray-500">Create your first content plan to get started.</p>
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
                      {plan.type}
                    </Badge>
                    <Badge variant={planStatusBadge(plan.status)} className="shrink-0">
                      {plan.status}
                    </Badge>
                  </div>
                  {plan.description && (
                    <p className="mt-1 truncate text-xs text-gray-400">{plan.description}</p>
                  )}
                  <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500">
                    <span>{cronToHuman(plan.cron_expr)}</span>
                    <span className="font-mono text-[10px]">{plan.cron_expr}</span>
                    {plan.topic_hint && <span>Topic: {plan.topic_hint}</span>}
                  </div>
                  <p className="mt-1 text-xs text-gray-500">
                    Next run: {formatNextRun(plan.next_run_at)}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  {plan.status === 'active' && (
                    <Button variant="ghost" size="sm" onClick={() => pauseMutation.mutate(plan.id)}>
                      Pause
                    </Button>
                  )}
                  {plan.status === 'paused' && (
                    <Button variant="ghost" size="sm" onClick={() => resumeMutation.mutate(plan.id)}>
                      Resume
                    </Button>
                  )}
                  <Button variant="ghost" size="sm" onClick={() => openEdit(plan)}>
                    Edit
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-red-400 hover:text-red-300"
                    onClick={() => {
                      if (window.confirm('Delete this plan? This action cannot be undone.')) {
                        deleteMutation.mutate(plan.id)
                      }
                    }}
                  >
                    Delete
                  </Button>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Create/Edit Modal */}
      <Modal
        open={modalOpen}
        onClose={closeModal}
        title={editingPlan ? 'Edit Plan' : 'New Plan'}
        footer={
          <>
            <Button variant="secondary" onClick={closeModal}>Cancel</Button>
            <Button onClick={handleSubmit} loading={isSubmitting}>
              {editingPlan ? 'Update' : 'Create'}
            </Button>
          </>
        }
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <Select
            label="Content Type"
            options={planTypeOptions}
            value={form.type}
            onChange={(e) => setForm({ ...form, type: e.target.value as PlanType })}
          />

          <Input
            label="Title"
            placeholder="e.g. Weekly RedNote posts"
            value={form.title}
            onChange={(e) => setForm({ ...form, title: e.target.value })}
            required
          />

          <Textarea
            label="Description"
            placeholder="Optional description of this plan"
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />

          <Input
            label="Cron Expression"
            placeholder="0 9 * * 1,3,5"
            hint="e.g. 0 9 * * 1,3,5 = every Mon/Wed/Fri at 9:00"
            value={form.cron_expr}
            onChange={(e) => setForm({ ...form, cron_expr: e.target.value })}
            required
          />
          {form.cron_expr && (
            <p className="text-xs text-gray-400">
              Preview: {cronToHuman(form.cron_expr)}
            </p>
          )}

          <Input
            label="Topic Hint (optional)"
            placeholder="e.g. beauty tips, tech reviews"
            value={form.topic_hint}
            onChange={(e) => setForm({ ...form, topic_hint: e.target.value })}
          />

          {formError && (
            <div className="rounded-lg bg-red-900/50 px-3 py-2 text-sm text-red-300">
              {formError}
            </div>
          )}
        </form>
      </Modal>
    </div>
  )
}
