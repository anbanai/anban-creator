import { HypitCreationPanel } from '@/components/hypit/HypitCreationPanel'
import { buildHypitInputForSubmit, initialHypitInput } from '@/lib/hypit-form'
import { useState, useEffect, useMemo, useRef, useCallback, type BaseSyntheticEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, FileText, Stamp, Loader2, Images, UserRound } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import type { AgentExecutionProfileID, PlatformConfig, Project, Plan, PlanType, CreatePlanRequest, UpdatePlanRequest } from '@/types'
import type { Resolver } from 'react-hook-form'
import { ProjectSelector } from '@/components/ProjectSelector'
import { AgentPromptInput } from '@/components/agent-prompt/AgentPromptInput'
import { SeednoteTemplateGallery } from '@/components/templates/SeednoteTemplateGallery'
import { GENERAL_AGENT_ATTACHMENT_POLICY } from '@/components/agent-prompt/attachment-admission'
import { ProjectContextControl } from '@/components/agent-prompt/ProjectContextControl'
import { usePromptAttachments } from '@/components/agent-prompt/usePromptAttachments'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import SchedulePicker from '@/components/SchedulePicker'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { planStatusLabel, contentTypeLabel, formatDateTimeCN, cronToHuman, getBadgeVariant } from '@/lib/labels'
import { platformBadgeVariant, platformBadgeClassName, platformBorderColor, platformHoverBorderColor } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { normalizeImageRatio, planSchema, type PlanFormValues } from '@/lib/schemas'
import { buildMontageInputForSubmit, initialMontageInput } from '@/lib/montage-form'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { useImageCapabilities } from '@/hooks/useImageCapabilities'
import PageHeader from '@/components/layout/PageHeader'
import { SimplePagination } from '@/components/SimplePagination'
import EmptyState from '@/components/EmptyState'
import { MontageCreationPanel } from '@/components/montage/MontageCreationPanel'
import { cn } from '@/lib/utils'
import { parseCreationIntent } from '@/lib/command-center'
import { taskCostFor } from '@/lib/pricing'
import { cheapestAvailableExecutionProfile } from '@/lib/pricing'
import { queryKeys } from '@/lib/query-keys'
import { useAgentExecutionProfiles } from '@/hooks/useAgentExecutionProfiles'
import type { PromptAttachment } from '@/types/input-attachment'
import { prepareReusableInputAttachments } from '@/lib/input-attachment-submit'
import { TaskComposerParameters } from '@/components/tasks/TaskComposerParameters'
import { AgentPackSchemaFields } from '@/components/agent-pack/AgentPackSchemaFields'
import { useAgentPacks } from '@/hooks/useAgentPacks'
import { TaskTimePricingNotice } from '@/components/billing/TaskTimePricingNotice'

function isPlanType(value: string | undefined): value is PlanType {
  return value === 'seednote' || value === 'article' || value === 'montage' || value === 'hypit'
}

function planToFormValues(plan: Plan): PlanFormValues {
  return {
    project_id: plan.project_id || '',
    execution_profile: plan.execution_profile,
    type: plan.type,
    cron_expr: plan.cron_expr,
    prompt: plan.type === 'hypit' ? plan.hypit_input?.brief || plan.prompt || '' : plan.prompt || '',
    image_capability_key: plan.image_capability_key || '',
    image_ratio: normalizeImageRatio(plan.image_ratio),
    skip_reference_image: plan.skip_reference_image || false,
    input_attachments: plan.input_attachments ?? [],
    agent_input: plan.agent_input ?? {},
    watermark: plan.watermark || false,
    has_content_image: plan.has_content_image ?? true,
    has_tail_image: plan.has_tail_image ?? false,
    article_with_cover: plan.article_with_cover ?? true,
    article_with_content_images: plan.article_with_content_images ?? true,
    hypit_input: plan.type === 'hypit' ? initialHypitInput(plan.prompt || '', plan.hypit_input) : undefined,
    montage_input: plan.type === 'montage' ? initialMontageInput(plan.prompt || '', plan.montage_input) : undefined,
  }
}

function cronTime(cron: string) {
  const [minute, hour] = cron.trim().split(/\s+/)
  if (hour === undefined || minute === undefined) return undefined
  return `${hour.padStart(2, '0')}:${minute.padStart(2, '0')}`
}

function cronWithTime(cron: string, value: string) {
  const parts = cron.trim().split(/\s+/)
  const [hour, minute] = value.split(':')
  if (parts.length !== 5 || hour === undefined || minute === undefined) return cron
  parts[0] = String(Number(minute))
  parts[1] = String(Number(hour))
  return parts.join(' ')
}

export default function PlansPage() {
  const agentPacksQuery = useAgentPacks()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const createIntent = parseCreationIntent(searchParams)
  const [projectFilter, setProjectFilter] = useState('')
  const [searchFilter, setSearchFilter] = useState('')
  const [page, setPage] = useState(1)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [promptAttachments, setPromptAttachments] = useState<PromptAttachment[]>([])
  const [attachmentSubmitError, setAttachmentSubmitError] = useState('')
  const [montageUploading, setMontageUploading] = useState(false)
  const [montageReady, setMontageReady] = useState(false)
  const [recommendationUnavailable, setRecommendationUnavailable] = useState(false)
  const [scheduleValid, setScheduleValid] = useState(true)
  const { submit } = useSubmitLock()
  const highlightedPlanId = searchParams.get('highlight') || ''
  const planRefs = useRef<Record<string, HTMLDivElement | null>>({})
  const attachmentsTouchedRef = useRef(false)
  const attachmentHydratingRef = useRef(false)
  const recommendationRequestRef = useRef(0)
  const scheduleManuallyChangedRef = useRef(false)

  const form = useForm<PlanFormValues>({
    resolver: zodResolver(planSchema) as Resolver<PlanFormValues>,
    defaultValues: {
      project_id: '',
      execution_profile: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
      image_capability_key: '',
      image_ratio: 'auto',
      input_attachments: [],
      agent_input: {},
      has_content_image: true,
      has_tail_image: false,
      article_with_cover: true,
      article_with_content_images: true,
      hypit_input: undefined,
      montage_input: undefined,
    },
  })
  const attachmentController = usePromptAttachments({
    adapter: { mode: 'direct', purpose: 'ai_entry_attachment' },
    policy: GENERAL_AGENT_ATTACHMENT_POLICY,
    attachments: promptAttachments,
    onAttachmentsChange: (next) => {
      setPromptAttachments(next)
      setAttachmentSubmitError('')
      if (attachmentHydratingRef.current) return
      attachmentsTouchedRef.current = true
      const pendingAttachments = next.filter((attachment) => attachment.status !== 'uploaded').map((attachment) => ({
        type: attachment.type,
        file_name: attachment.fileName,
        content_type: attachment.contentType,
        size: attachment.size,
        instruction: attachment.instruction,
        role: attachment.role,
      }))
      form.setValue('input_attachments', [
        ...attachmentController.toInputAttachments(),
        ...pendingAttachments,
      ], {
        shouldDirty: true,
        shouldValidate: true,
      })
    },
  })

  // Auto-focus title field when dialog opens
  useEffect(() => {
    if (modalOpen) {
      setTimeout(() => form.setFocus('cron_expr'), 100)
    }
  }, [modalOpen, form])

	const watchedType = useWatch({ control: form.control, name: 'type' })
	const watchedProjectId = useWatch({ control: form.control, name: 'project_id' })
	const watchedExecutionProfile = useWatch({ control: form.control, name: 'execution_profile' })
	const watchedImageCapabilityKey = useWatch({ control: form.control, name: 'image_capability_key' })
	const watchedImageRatio = useWatch({ control: form.control, name: 'image_ratio' })
	const watchedAgentInput = useWatch({ control: form.control, name: 'agent_input' }) ?? {}
	const isMontagePlan = watchedType === 'montage' || watchedType === 'hypit'
	const usesImageSettings = watchedType !== 'hypit'
	const {
		items: imageCapabilityOptions,
		defaultCapability: defaultImageCapability,
		isLoading: imageCapabilitiesLoading,
		isError: imageCapabilitiesError,
	} = useImageCapabilities(modalOpen && usesImageSettings)
	const effectiveImageCapabilityKey = watchedImageCapabilityKey || defaultImageCapability
	const selectedImageCapability = imageCapabilityOptions.find((option) => option.key === effectiveImageCapabilityKey)
	const imageCapabilityUnavailable = usesImageSettings
		&& !imageCapabilitiesLoading
		&& !imageCapabilitiesError
		&& (
			!selectedImageCapability
			|| selectedImageCapability.enabled !== true
			|| selectedImageCapability.price_available !== true
		)
	const imageCapabilityBlocker = !usesImageSettings
		? null
		: imageCapabilitiesError
			? '图像能力暂时无法加载，请稍后重试。'
			: imageCapabilitiesLoading
				? '正在加载图像能力，请稍候。'
				: imageCapabilityUnavailable
					? '当前图像能力不可用，请重新选择。'
					: null
	const selectedAgentPack = useMemo(
		() => agentPacksQuery.data?.packs?.find((pack) => pack.bindings.task_types?.includes(watchedType)),
		[agentPacksQuery.data, watchedType],
	)

  const handleScheduleValidityChange = useCallback((valid: boolean) => {
    setScheduleValid(valid)
    if (valid) form.clearErrors('cron_expr')
    else form.setError('cron_expr', { type: 'validate' })
  }, [form])

  // Warn before closing with unsaved changes
  useFormDirtyCheck(form, modalOpen)

  useEffect(() => { setPage(1) }, [projectFilter])

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['plans', projectFilter, page],
    queryFn: () => api.plans.list({
      limit: 50,
      offset: (page - 1) * 50,
      project_id: projectFilter || undefined,
    }),
  })

  const plans = data?.items ?? []
  const totalPlans = data?.total ?? 0
  const totalPages = Math.ceil(totalPlans / 50)
  const filteredPlans = useMemo(() => {
    if (!searchFilter.trim()) return plans
    const q = searchFilter.trim().toLowerCase()
    return plans.filter((p) => (p.prompt || '').toLowerCase().includes(q))
  }, [plans, searchFilter])

  useEffect(() => {
    if (!highlightedPlanId || isLoading) return
    planRefs.current[highlightedPlanId]?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }, [highlightedPlanId, isLoading, filteredPlans.length])

  // Fetch projects for name/avatar display
  const { data: allProjects } = useQuery({
    queryKey: ['projects-for-plans'],
    queryFn: () => api.projects.list(),
    staleTime: 60_000,
  })
  const { data: platformConfigs = [] } = useQuery({
    queryKey: ['platform-configs'],
    queryFn: () => api.projects.platformConfigs(),
    staleTime: Infinity,
  })
  const platformConfigMap = useMemo(() => new Map<string, PlatformConfig>(
    platformConfigs.map((config) => [config.id, config]),
  ), [platformConfigs])

  const projectMap = useMemo(() => {
    const map: Record<string, Project> = {}
    if (allProjects) {
      for (const ch of allProjects) {
        map[ch.id] = ch
      }
    }
    return map
  }, [allProjects])
  const planContextProjects = useMemo(
    () => (allProjects ?? []).filter((project) => isPlanType(project.platform)),
    [allProjects],
  )
  const selectedProject = projectMap[watchedProjectId ?? ''] ?? undefined
  const businessImageRatios = platformConfigMap.get(selectedProject?.platform ?? watchedType)?.supported_image_ratios ?? []

  const { data: billingCatalog, refetch: refetchBillingCatalog } = useQuery({
    queryKey: queryKeys.billing.catalog,
    queryFn: () => api.billing.catalog(),
    staleTime: 60_000,
  })

  useEffect(() => {
    const transition = billingCatalog?.task_time_pricing?.next_transition_at
    if (!modalOpen || !transition) return
    const delay = new Date(transition).getTime() - Date.now() + 250
    if (delay <= 0) {
      void refetchBillingCatalog()
      return
    }
    const timer = window.setTimeout(() => { void refetchBillingCatalog() }, delay)
    return () => window.clearTimeout(timer)
  }, [billingCatalog?.task_time_pricing?.next_transition_at, modalOpen, refetchBillingCatalog])
  const { data: billingWallet } = useQuery({
    queryKey: queryKeys.billing.wallet,
    queryFn: () => api.billing.wallet(),
    staleTime: 30_000,
  })
  const executionProfilesQuery = useAgentExecutionProfiles()
  const selectedExecutionProfileAvailable = executionProfilesQuery.data
    ?.find((profile) => profile.id === watchedExecutionProfile)
    ?.available === true
  const defaultExecutionProfile = cheapestAvailableExecutionProfile(
    executionProfilesQuery.data,
    billingCatalog,
    watchedType,
  )

  useEffect(() => {
    if (!modalOpen || form.getValues('execution_profile') || !defaultExecutionProfile) return
    form.setValue('execution_profile', defaultExecutionProfile, { shouldValidate: true })
  }, [defaultExecutionProfile, form, modalOpen])

  const createMutation = useMutation({
    mutationFn: (data: CreatePlanRequest) => api.plans.create(data),
    onSuccess: () => {
      toast.success('计划创建成功')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      resetModal()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '创建计划失败'))
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdatePlanRequest }) => api.plans.update(id, data),
    onSuccess: () => {
      toast.success('计划更新成功')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      resetModal()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '更新计划失败'))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.plans.delete(id),
    onSuccess: () => {
      toast.success('计划已删除')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
      setDeleteTarget(null)
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '删除计划失败'))
    },
  })

  const pauseMutation = useMutation({
    mutationFn: (id: string) => api.plans.pause(id),
    onSuccess: () => {
      toast.success('计划已暂停')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '暂停计划失败'))
    },
  })

  const resumeMutation = useMutation({
    mutationFn: (id: string) => api.plans.resume(id),
    onSuccess: () => {
      toast.success('计划已恢复')
      queryClient.invalidateQueries({ queryKey: ['plans'] })
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '恢复计划失败'))
    },
  })

  const openCreate = useCallback(() => {
    const requestedType: PlanType = createIntent.type === 'article' || createIntent.type === 'montage' || createIntent.type === 'hypit'
      ? createIntent.type
      : 'seednote'
    const intentProject = createIntent.projectId ? projectMap[createIntent.projectId] : undefined
    const intentProjectType = isPlanType(intentProject?.platform) ? intentProject.platform : undefined
    const selectedIntentProject = intentProject && intentProjectType
      ? intentProject
      : undefined
    const initialType = intentProjectType ?? requestedType
    setEditingPlan(null)
    scheduleManuallyChangedRef.current = false
    setRecommendationUnavailable(false)
    setMontageUploading(false)
    setMontageReady(false)
    setAttachmentSubmitError('')
    attachmentsTouchedRef.current = false
    attachmentHydratingRef.current = true
    attachmentController.clear()
    attachmentHydratingRef.current = false
    form.reset({
      project_id: selectedIntentProject?.id ?? '',
      execution_profile: '',
      type: initialType,
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
      image_capability_key: '',
      image_ratio: normalizeImageRatio(selectedIntentProject?.image_ratio),
      input_attachments: [],
      agent_input: {},
      has_content_image: true,
      has_tail_image: false,
      article_with_cover: true,
      article_with_content_images: true,
      hypit_input: initialType === 'hypit' ? initialHypitInput('', undefined, selectedIntentProject?.hypit_defaults) : undefined,
      montage_input: initialType === 'montage'
        ? initialMontageInput('', undefined, selectedIntentProject?.montage_defaults)
        : undefined,
    })
    setModalOpen(true)
    const requestID = ++recommendationRequestRef.current
    void refetchBillingCatalog()
    void api.plans.scheduleRecommendation().then((recommendation) => {
      if (requestID !== recommendationRequestRef.current || scheduleManuallyChangedRef.current) return
      form.setValue('cron_expr', cronWithTime(form.getValues('cron_expr'), recommendation.time), { shouldValidate: true })
      setRecommendationUnavailable(!recommendation.load_balanced)
    }).catch(async () => {
      if (requestID !== recommendationRequestRef.current || scheduleManuallyChangedRef.current) return
      const fallbackCatalog = billingCatalog ?? await queryClient.fetchQuery({ queryKey: queryKeys.billing.catalog, queryFn: () => api.billing.catalog() }).catch(() => undefined)
      const fallback = fallbackCatalog?.task_time_pricing?.off_peak_windows[0]?.start
      if (fallback) form.setValue('cron_expr', cronWithTime(form.getValues('cron_expr'), fallback), { shouldValidate: true })
      setRecommendationUnavailable(true)
    })
  }, [billingCatalog, createIntent.projectId, createIntent.type, form, projectMap, queryClient, refetchBillingCatalog])

  useEffect(() => {
    if (!createIntent.shouldCreate) return
    if (createIntent.projectId && !allProjects) return
    openCreate()
    setSearchParams({}, { replace: true })
  }, [allProjects, createIntent.projectId, createIntent.shouldCreate, openCreate, setSearchParams])

  function openEdit(plan: Plan) {
    recommendationRequestRef.current++
    setEditingPlan(plan)
    setRecommendationUnavailable(false)
    setAttachmentSubmitError('')
    setMontageUploading(false)
    setMontageReady(false)
    form.reset(planToFormValues(plan))
    attachmentsTouchedRef.current = false
    attachmentHydratingRef.current = true
    attachmentController.reset(plan.input_attachments ?? [])
    attachmentHydratingRef.current = false
    setModalOpen(true)
  }

  function closeModal() {
    if (form.formState.isDirty) {
      setShowDirtyDialog(true)
      return
    }
    resetModal()
  }

  function resetModal() {
    setModalOpen(false)
    setShowDirtyDialog(false)
    setEditingPlan(null)
    setMontageUploading(false)
    setMontageReady(false)
    setAttachmentSubmitError('')
    attachmentsTouchedRef.current = false
    attachmentHydratingRef.current = true
    attachmentController.clear()
    attachmentHydratingRef.current = false
    form.reset({
      project_id: '',
      execution_profile: '',
      type: 'seednote',
      cron_expr: '0 9 * * 1,3,5',
      prompt: '',
      image_capability_key: '',
		image_ratio: 'auto',
      input_attachments: [],
      has_content_image: true,
      has_tail_image: false,
      article_with_cover: true,
      article_with_content_images: true,
      hypit_input: undefined,
      montage_input: undefined,
    })
  }

  async function onSubmit(values: PlanFormValues) {
    const submittedProfileAvailable = executionProfilesQuery.data
      ?.find((profile) => profile.id === values.execution_profile)
      ?.available === true
    if (!submittedProfileAvailable) return
	const submittedImageCapability = imageCapabilityOptions.find(
		(option) => option.key === (values.image_capability_key || defaultImageCapability),
	)
	if (values.type !== 'hypit' && (
		!submittedImageCapability
		|| submittedImageCapability.enabled !== true
		|| submittedImageCapability.price_available !== true
	)) {
		toast.error('该图像能力已停用，请重新选择')
		return
	}
    // For edit (PUT), image_capability_key is a *string on the backend: nil = leave
    // unchanged, "" = clear to system default. Always send it so explicit
    // "system default" selection actually clears the previously saved value.
    // For create (POST), "" is also valid (means system default).
    let inputAttachments = editingPlan && !attachmentsTouchedRef.current
      ? undefined
      : values.input_attachments
    if (inputAttachments !== undefined) {
      const prepared = prepareReusableInputAttachments(inputAttachments, { allowExternalURLs: true })
      if (prepared.error) {
        setAttachmentSubmitError(prepared.error)
        return
      }
      inputAttachments = prepared.attachments ?? []
    }
    setAttachmentSubmitError('')

    const payload: CreatePlanRequest = {
      type: values.type,
      execution_profile: values.execution_profile as AgentExecutionProfileID,
      cron_expr: values.cron_expr.trim(),
      prompt: values.prompt?.trim() || undefined,
      project_id: values.project_id || undefined,
	  image_capability_key: values.image_capability_key,
	  image_ratio: values.image_ratio,
      ...(inputAttachments === undefined ? {} : { input_attachments: inputAttachments }),
      agent_input: values.agent_input,
      watermark: values.watermark || undefined,
      has_content_image: values.type === 'seednote' ? values.has_content_image : undefined,
      has_tail_image: values.type === 'seednote' ? values.has_tail_image : undefined,
      // Article image toggles (公众号文章): both default true; non-article omits.
      article_with_cover: values.type === 'article' ? values.article_with_cover : undefined,
      article_with_content_images: values.type === 'article' ? values.article_with_content_images : undefined,
      hypit_input: values.type === 'hypit' ? buildHypitInputForSubmit(values.prompt || '', values.hypit_input, selectedProject?.hypit_defaults) : undefined,
      montage_input: values.type === 'montage' ? buildMontageInputForSubmit(values.prompt, values.montage_input) : undefined,
    }

    if (editingPlan) {
      await submit(async () => updateMutation.mutateAsync({ id: editingPlan.id, data: payload })).catch(() => {})
    } else {
      await submit(async () => createMutation.mutateAsync(payload)).catch(() => {})
    }
  }

  function handlePlanSubmit(event?: BaseSyntheticEvent) {
    if (imageCapabilityBlocker || !scheduleValid || attachmentController.uploading || attachmentController.hasFailures || (isMontagePlan && (montageUploading || !montageReady))) {
      event?.preventDefault()
      return
    }
    return form.handleSubmit(onSubmit)(event)
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending
  const projectControl = editingPlan ? (
    <ProjectContextControl mode="readonly" project={selectedProject ?? null} compact />
  ) : (
    <ProjectContextControl
      mode="select"
      projects={planContextProjects}
      value={watchedProjectId || null}
      allowNoProject={false}
      placeholder="选择项目"
      disabled={isSubmitting}
      onValueChange={(id, project) => {
        const preserveHypitInput = form.getValues('type') === 'hypit' && project?.platform === 'hypit'
        if (!preserveHypitInput) {
          setMontageUploading(false)
          setMontageReady(false)
        }
        form.setValue('project_id', id ?? '', { shouldDirty: true, shouldValidate: true })
        if (!id || !isPlanType(project?.platform)) return
        const nextType = project.platform
        const fullProject = projectMap[id]
        form.setValue('type', nextType, { shouldDirty: true })
        form.setValue('image_ratio', normalizeImageRatio(fullProject?.image_ratio), { shouldDirty: true })
        form.setValue('agent_input', {}, { shouldDirty: true })
        form.setValue('hypit_input', nextType === 'hypit' ? initialHypitInput(form.getValues('prompt') || '', preserveHypitInput ? { ...form.getValues('hypit_input'), preferences: fullProject?.hypit_defaults?.preferences } : undefined, fullProject?.hypit_defaults) : undefined, { shouldDirty: false })
        form.setValue('montage_input', nextType === 'montage' ? initialMontageInput(form.getValues('prompt') || '', undefined, fullProject?.montage_defaults) : undefined, { shouldDirty: false })
      }}
      ariaLabel={selectedProject ? `项目：${selectedProject.name}` : '项目：未选择'}
      compact
    />
  )
  const promptComposer = (
    <>
      <AgentPromptInput
      value={{ prompt: form.watch('prompt') ?? '', attachments: watchedType === 'hypit' ? [] : promptAttachments }}
      onChange={(value) => {
        form.setValue('prompt', value.prompt, { shouldDirty: true, shouldValidate: true })
        if (watchedType === 'hypit') {
          form.setValue('hypit_input.brief', value.prompt, { shouldDirty: true, shouldValidate: true })
        }
        if (watchedType === 'montage') {
          form.setValue('montage_input.brief', value.prompt, { shouldDirty: true, shouldValidate: true })
        }
        if (value.attachments !== promptAttachments) setPromptAttachments(value.attachments)
      }}
      onSubmit={() => handlePlanSubmit()}
      attachmentController={attachmentController}
      attachmentPolicy={GENERAL_AGENT_ATTACHMENT_POLICY}
      attachmentsEnabled={watchedType !== 'hypit'}
      ariaLabel={watchedType === 'hypit' ? '复刻要求' : undefined}
      submitMode="external"
      placeholder={watchedType === 'hypit' ? '描述每次复刻需要保留和替换的内容' : '描述每次计划的创作方向、内容要求和素材使用方式...'}
      submitLabel={editingPlan ? '更新计划' : '创建计划'}
      submitting={isSubmitting}
      submitDisabled={!watchedProjectId || Boolean(imageCapabilityBlocker) || (isMontagePlan && !montageReady)}
      attachmentPreviewOwner={editingPlan ? { ownerType: 'plan', ownerId: editingPlan.id } : undefined}
      leadingTools={(
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          {projectControl}
          <TaskComposerParameters
            execution={{
              profiles: executionProfilesQuery.data ?? [],
              value: watchedExecutionProfile,
              onChange: (value) => form.setValue('execution_profile', value, { shouldDirty: true, shouldValidate: true }),
              loading: executionProfilesQuery.isLoading,
              disabled: executionProfilesQuery.isError,
              catalog: billingCatalog,
              taskType: watchedType,
              priceUnit: 'run',
            }}
            image={usesImageSettings ? {
              ratios: businessImageRatios,
              ratio: watchedImageRatio || 'auto',
              onRatioChange: (value) => form.setValue('image_ratio', value, { shouldDirty: true, shouldValidate: true }),
              capabilities: imageCapabilityOptions,
              capabilityKey: effectiveImageCapabilityKey,
              onCapabilityChange: (value) => form.setValue('image_capability_key', value, { shouldDirty: true, shouldValidate: true }),
              loading: imageCapabilitiesLoading,
              disabled: imageCapabilitiesError,
            } : undefined}
            disabled={isSubmitting}
          />
        </div>
      )}
      />
      {attachmentSubmitError ? (
        <p role="alert" className="text-sm text-destructive">{attachmentSubmitError}</p>
      ) : null}
    </>
  )

  return (
    <div className="space-y-6">
      <PageHeader title="计划" description="管理你的内容计划，定时自动创作发布。">
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建计划
        </Button>
      </PageHeader>

      {/* Project filter + Search */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="w-full sm:w-48">
          <ProjectSelector
            value={projectFilter}
            onChange={(id) => setProjectFilter(id)}
            excludePlatforms={['moments', 'ecommerce']}
          />
        </div>
        <div className="w-full sm:w-48 sm:ml-auto">
          <SearchInput
            value={searchFilter}
            onChange={setSearchFilter}
            placeholder="搜索本页计划..."
          />
        </div>
      </div>

      {!isLoading && !isError && (
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <p role="status">共 {totalPlans} 个计划 · 本页显示 {filteredPlans.length} 个</p>
          {(projectFilter || searchFilter) && <Button variant="ghost" size="sm" onClick={() => { setProjectFilter(''); setSearchFilter('') }}>清空筛选</Button>}
        </div>
      )}

      {isError ? (
        <QueryErrorState onRetry={() => refetch()} />
      ) : isLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="rounded-lg border border-border bg-card p-4 border-l-4 border-l-muted">
              <div className="flex items-start gap-3">
                <Skeleton className="h-8 w-8 rounded-full" />
                <div className="min-w-0 flex-1 space-y-2">
                  <Skeleton className="h-4 w-2/3" />
                  <Skeleton className="h-3 w-1/4" />
                  <div className="flex gap-2">
                    <Skeleton className="h-5 w-14 rounded-full" />
                    <Skeleton className="h-3 w-32" />
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : filteredPlans.length === 0 ? (
        <EmptyState
          icon={FileText}
          title={searchFilter.trim() || projectFilter ? "未找到匹配的计划" : "还没有计划"}
          description={searchFilter.trim() || projectFilter ? "试试其他关键词、清空筛选，或翻页查看其他计划。搜索仅匹配当前页。" : "创建你的第一个内容计划，让 AI 定时帮你创作。"}
          action={!searchFilter.trim() && !projectFilter ? { label: '新建计划', onClick: openCreate } : undefined}
        />
      ) : (
        <>
        <div className="space-y-3">
          {filteredPlans.map((plan) => {
            const project = projectMap[plan.project_id]
            const borderColor = platformBorderColor[plan.type] || ''
            const hoverBorderColor = platformHoverBorderColor[plan.type] || ''
            const platformBadge = platformBadgeVariant[plan.type] || ('neutral' as const)

            return (
              <div
                key={plan.id}
                ref={(node) => {
                  planRefs.current[plan.id] = node
                }}
                data-testid={`plan-card-${plan.id}`}
                data-plan-id={plan.id}
                data-highlighted={highlightedPlanId === plan.id ? 'true' : undefined}
                className={cn(
                  'group rounded-lg border border-border bg-card p-4 border-l-4 transition-all duration-200 hover:shadow-md',
                  borderColor,
                  hoverBorderColor,
                  highlightedPlanId === plan.id && 'ring-2 ring-primary/30 bg-primary/5',
                )}
              >
                <div className="flex items-start gap-3">
                  <PlatformAvatar avatarUrl={project?.avatar_url} name={project?.name} platform={plan.type} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-start justify-between gap-2">
                      <span className="truncate text-sm font-medium text-foreground">
                        {plan.prompt || contentTypeLabel[plan.type] + '计划'}
                      </span>
                      <Badge variant={getBadgeVariant(plan.status, 'plan')} className="shrink-0">
                        {planStatusLabel[plan.status] || plan.status}
                      </Badge>
                    </div>
                    {project?.name && (
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">{project.name}</p>
                    )}
                    <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                      <Badge variant={platformBadge} className={cn("text-[10px]", platformBadgeClassName[plan.type])}>
                        {contentTypeLabel[plan.type] || plan.type}
                      </Badge>
                      <span>{cronToHuman(plan.cron_expr)}</span>
                      <span>下次：{formatDateTimeCN(plan.next_run_at)}</span>
                    </div>
                  </div>
                </div>
                <div className="mt-2 flex justify-end gap-1.5 border-t border-border pt-2">
                  {plan.status === 'active' && (
                    <Button variant="ghost" size="xs" loading={pauseMutation.isPending} onClick={() => { void submit(async () => pauseMutation.mutateAsync(plan.id)).catch(() => {}) }}>
                      暂停
                    </Button>
                  )}
                  {plan.status === 'paused' && (
                    <Button variant="ghost" size="xs" loading={resumeMutation.isPending} onClick={() => { void submit(async () => resumeMutation.mutateAsync(plan.id)).catch(() => {}) }}>
                      恢复
                    </Button>
                  )}
                  <Button variant="ghost" size="xs" onClick={() => openEdit(plan)}>
                    编辑
                  </Button>
                  <Button
                    variant="destructive"
                    size="xs"
                    onClick={() => setDeleteTarget(plan.id)}
                  >
                    删除
                  </Button>
                </div>
              </div>
            )
          })}
        </div>

        </>
      )}

      {!isLoading && !isError && totalPages > 1 && (
        <div className="mt-4 flex justify-center">
          <SimplePagination
            page={page}
            totalPages={totalPages}
            onPageChange={setPage}
          />
        </div>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingPlan ? '编辑计划' : '新建计划'}</DialogTitle>
          </DialogHeader>
          <Form {...form}>
            <form id="plan-form" onSubmit={handlePlanSubmit} className="max-h-[60vh] space-y-4 overflow-y-auto">
              <SeednoteTemplateGallery
                platform={selectedProject?.platform}
                onApply={(templatePrompt) => {
                  form.setValue('prompt', templatePrompt, { shouldDirty: true, shouldValidate: true })
                }}
              />

              <FormField control={form.control} name="cron_expr" render={({ field }) => (
                <FormItem>
                  <FormLabel>排期设置</FormLabel>
                  <FormControl>
                    <SchedulePicker
                      value={field.value}
                      onChange={field.onChange}
                      onInteraction={() => { scheduleManuallyChangedRef.current = true }}
                      onValidityChange={handleScheduleValidityChange}
                      footer={(
                        <TaskTimePricingNotice
                          catalog={billingCatalog}
                          taskType={watchedType}
                          executionProfile={(watchedExecutionProfile || undefined) as AgentExecutionProfileID | undefined}
                          selectedTime={cronTime(field.value)}
                          recommendationUnavailable={!editingPlan && recommendationUnavailable}
                        />
                      )}
                    />
                  </FormControl>
                </FormItem>
              )} />

              {!isMontagePlan ? promptComposer : null}

              {watchedType === 'hypit' ? <HypitCreationPanel form={form} onReadyChange={setMontageReady} onUploadingChange={setMontageUploading} briefField={promptComposer} /> : isMontagePlan && (
                <MontageCreationPanel
                  form={form}
                  fieldRoot="montage_input"
                  onUploadingChange={setMontageUploading}
                  onReadyChange={setMontageReady}
                  briefField={promptComposer}
                />
              )}

              {!isMontagePlan && <FormField control={form.control} name="watermark" render={({ field }) => (
                <FormItem>
                  <button
                    type="button"
                    onClick={() => field.onChange(!field.value)}
                    className={`flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors ${
                      field.value
                        ? 'border-primary bg-primary/5'
                        : 'border-border hover:border-foreground/20'
                    }`}
                  >
                    <Stamp className={`mt-0.5 h-5 w-5 shrink-0 ${field.value ? 'text-primary' : 'text-muted-foreground'}`} />
                    <div className="min-w-0">
                      <p className={`text-sm font-medium ${field.value ? 'text-foreground' : 'text-muted-foreground'}`}>
                        水印
                      </p>
                      <p className="mt-0.5 text-xs text-muted-foreground">仅在所选图像能力支持水印时生效</p>
                    </div>
                  </button>
                  <FormMessage />
                </FormItem>
              )} />}

              {/* Image composition (seednote only) */}
              {watchedType === 'seednote' && (
                <FormField control={form.control} name="has_content_image" render={({ field }) => {
                  const hasTail = form.watch('has_tail_image')
                  const total = 1 + (field.value ? 1 : 0) + (hasTail ? 1 : 0)
                  return (
                    <FormItem>
                      <div className="rounded-lg border border-border p-3">
                        <div className="flex items-start gap-3">
                          <Images className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                          <div className="min-w-0 flex-1">
                            <p className="text-sm font-medium text-foreground">图片构成</p>
                            <p className="mt-0.5 text-xs text-muted-foreground">
                              封面始终生成；勾选要额外生成的图。
                            </p>
                          </div>
                        </div>
                        <div className="mt-3 divide-y divide-border">
                          <div className="flex items-center justify-between py-2">
                            <div className="flex items-center gap-2">
                              <span className="text-sm font-medium text-foreground">封面图</span>
                              <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">必选</span>
                            </div>
                            <Switch checked disabled />
                          </div>
                          <div className="flex items-center justify-between py-2">
                            <div className="min-w-0">
                              <p className="text-sm font-medium text-foreground">内容图</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">承载 2-4 个信息点（image_01.png）</p>
                            </div>
                            <Switch checked={!!field.value} onCheckedChange={field.onChange} />
                          </div>
                          <div className="flex items-center justify-between py-2">
                            <div className="min-w-0">
                              <p className="text-sm font-medium text-foreground">尾图</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">行动召唤 / 关注引导（tail.png）</p>
                            </div>
                            <Switch checked={!!hasTail} onCheckedChange={(v) => form.setValue('has_tail_image', v, { shouldDirty: true })} />
                          </div>
                        </div>
                        <p className="mt-2 text-xs text-muted-foreground">
                          当前将生成 {total} 张图片
                        </p>
                      </div>
                      <FormMessage />
                    </FormItem>
                  )
                }} />
              )}

              {/* Image composition (公众号 article): cover + content images each
                  independently toggleable — unlike seednote, the article cover is
                  NOT mandatory (both default on → legacy behavior). */}
              {watchedType === 'article' && (
                <FormField control={form.control} name="article_with_cover" render={({ field }) => {
                  const withContent = form.watch('article_with_content_images')
                  const summary = field.value && withContent
                    ? '将生成封面 + 正文配图（默认）'
                    : field.value
                      ? '仅生成封面图，不生成正文配图'
                      : withContent
                        ? '仅生成正文配图；发布草稿不设封面'
                        : '纯文字文章，不生成任何图片；发布草稿不设封面'
                  return (
                    <FormItem>
                      <div className="rounded-lg border border-border p-3">
                        <div className="flex items-start gap-3">
                          <Images className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" />
                          <div className="min-w-0 flex-1">
                            <p className="text-sm font-medium text-foreground">图片构成</p>
                            <p className="mt-0.5 text-xs text-muted-foreground">
                              独立选择是否生成封面与正文配图。两者都关 = 纯文字文章。
                            </p>
                          </div>
                        </div>
                        <div className="mt-3 divide-y divide-border">
                          <div className="flex items-center justify-between py-2">
                            <div className="min-w-0">
                              <p className="text-sm font-medium text-foreground">封面图</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">按任务比例生成，保护微信中心分享卡安全区</p>
                            </div>
                            <Switch aria-label="生成封面图" checked={!!field.value} onCheckedChange={(checked) => {
                              field.onChange(checked)
                            }} />
                          </div>
                          <div className="py-2">
                            <div className="flex items-center justify-between gap-3">
                              <div className="flex min-w-0 items-start gap-2">
                                <UserRound className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                                <div className="min-w-0">
                                  <p className="text-sm font-medium text-foreground">人物参考</p>
                                  <p className="mt-0.5 text-xs text-muted-foreground">每次执行自动提供项目人物参考，由 Agent 判断用途</p>
                                </div>
                              </div>
                            </div>
                            {selectedProject?.portrait_reference_image ? (
                              <div className="mt-3 flex items-center gap-3 rounded-md border border-border bg-muted/30 p-2">
                                <img src={selectedProject.portrait_reference_image.download_url} alt="项目人物参考" className="h-14 w-14 rounded-md border object-cover" />
                                <span className="text-xs text-muted-foreground">已作为输入提供，由 Agent 按内容决定是否用于封面</span>
                              </div>
                            ) : (
                              <div className="mt-3 flex items-center justify-between gap-3 rounded-md border border-dashed border-border p-2 text-xs text-muted-foreground">
                                <span>项目尚未设置人物参考图</span>
                                <Link className="text-primary hover:underline" to={`/projects?edit=${selectedProject?.id ?? ''}`}>去设置</Link>
                              </div>
                            )}
                          </div>
                          <div className="flex items-center justify-between py-2">
                            <div className="min-w-0">
                              <p className="text-sm font-medium text-foreground">正文配图</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">按排版节奏插入的章节插图</p>
                            </div>
                            <Switch aria-label="生成正文配图" checked={!!withContent} onCheckedChange={(v) => form.setValue('article_with_content_images', v, { shouldDirty: true })} />
                          </div>
                        </div>
                        <p className="mt-2 text-xs text-muted-foreground">{summary}</p>
                      </div>
                      <FormMessage />
                    </FormItem>
                  )
                }} />
              )}

              <AgentPackSchemaFields
                pack={selectedAgentPack}
                surface="plan"
                value={watchedAgentInput}
                onChange={(value) => form.setValue('agent_input', value, { shouldDirty: true, shouldValidate: true })}
              />
              {/* Each scheduled run resolves the active immutable task SKU at admission. */}
              {(() => {
                const perRun = taskCostFor(billingCatalog, watchedType as string, watchedExecutionProfile || undefined)
                const balance = billingWallet?.balance ?? 0
                const remaining = perRun === undefined ? undefined : balance - perRun
                return (
                  <div className="space-y-1 rounded-md border border-border bg-muted/50 p-3 text-sm">
                    <p className="text-muted-foreground">
                      当前每次执行固定价：<span className="font-medium text-foreground">
                        {perRun === undefined ? '暂不可用' : `${perRun.toLocaleString()} 积分`}
                      </span>
                    </p>
                    <p className="text-xs text-muted-foreground">每次触发时按当时生效的 SKU 计价；Claude Code token 不向用户计费。</p>
                    <p className="text-xs text-muted-foreground">任务内成功交付的图片、视频等增值操作使用各自固定 SKU。</p>
                    {remaining !== undefined && (
                      <p className="text-muted-foreground">
                        余额：{balance.toLocaleString()} →{' '}
                        <span className={`font-medium ${remaining < 0 ? 'text-red-500' : 'text-foreground'}`}>
                          {remaining.toLocaleString()}
                        </span>
                      </p>
                    )}
                    {((billingWallet?.debt ?? 0) > 0 || (remaining !== undefined && remaining < 0)) && (
                      <p className="text-sm font-medium text-red-500">当前钱包无法准入新一次执行，请先充值。</p>
                    )}
                    {imageCapabilityBlocker ? (
                      <p role="alert" className="text-sm font-medium text-red-500">{imageCapabilityBlocker}</p>
                    ) : null}
                  </div>
                )
              })()}
            </form>
          </Form>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button
              type="submit"
              form="plan-form"
              loading={isSubmitting}
              disabled={
                !watchedProjectId
                || !watchedExecutionProfile
                || executionProfilesQuery.isError
                || !selectedExecutionProfileAvailable
                || Boolean(imageCapabilityBlocker)
                || taskCostFor(billingCatalog, watchedType as string, watchedExecutionProfile || undefined) === undefined
                || attachmentController.uploading
                || attachmentController.hasFailures
                || (isMontagePlan && (montageUploading || !montageReady))}
            >
              {editingPlan ? '更新' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(v) => { if (!v) setDeleteTarget(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定删除此计划？</AlertDialogTitle>
            <AlertDialogDescription>此操作不可撤销。删除后计划将无法恢复。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={deleteMutation.isPending} onClick={() => { if (deleteTarget) void submit(async () => deleteMutation.mutateAsync(deleteTarget)).catch(() => {}) }}>
              {deleteMutation.isPending && <Loader2 className="size-3.5 animate-spin" />}
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Dirty form confirmation */}
      <AlertDialog open={showDirtyDialog} onOpenChange={setShowDirtyDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃编辑？</AlertDialogTitle>
            <AlertDialogDescription>你有未保存的更改，确定要关闭吗？</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction onClick={resetModal}>放弃</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
