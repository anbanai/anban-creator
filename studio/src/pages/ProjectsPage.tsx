import { useState, useEffect, useMemo, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, Inbox, Minus } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/lib/api'
import type { ImageAnalysis, Project, ProjectPlatform, ProjectStats, CreateProjectRequest, PlatformConfig } from '@/types'
import { isImageAnalysisActive, isImageAnalysisUpdateOlder } from '@/types'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import { ProjectCard } from '@/components/ProjectCard'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { ReferenceAssetUpload } from '@/components/projects/ReferenceAssetUpload'
import { AnalyzedImageField } from '@/components/image-analysis/AnalyzedImageField'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { PersonaBlock } from '@/components/templates/PersonaBlock'
import { ThemePicker } from '@/components/templates/ThemePicker'
import { ImageCapabilitySelector } from '@/components/ImageCapabilitySelector'
import { ImageAspectRatioField } from '@/components/tasks/ImageAspectRatioField'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { projectSchema, type ProjectFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { renderPlatformIcon } from '@/lib/PlatformIcon'
import { ecommerceModuleCatalog, ecommerceTargetPlatformOptions } from '@/lib/labels'
import { useImageCapabilities } from '@/hooks/useImageCapabilities'
import { parseCreationIntent, projectCreatedReturnHref } from '@/lib/command-center'
import { MontageProjectDefaultsPanel } from '@/components/montage/MontageProjectDefaultsPanel'
import { AgentPackSchemaFields } from '@/components/agent-pack/AgentPackSchemaFields'
import { useAgentPacks } from '@/hooks/useAgentPacks'
import { referenceSelectionFromValue } from '@/lib/reference-image'
import { useAuth } from '@/contexts/AuthContext'

const platformOptions: { value: ProjectPlatform; label: string }[] = [
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号' },
  { value: 'moments', label: '朋友圈' },
  { value: 'ecommerce', label: '电商出图' },
  { value: 'montage', label: 'Montage' },
]

const adminOnlyPlatforms = new Set<ProjectPlatform>(['moments', 'ecommerce'])

function canViewPlatform(platform: ProjectPlatform, isAdmin: boolean) {
  return isAdmin || !adminOnlyPlatforms.has(platform)
}

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '活跃', value: 'active' },
  { label: '已归档', value: 'archived' },
]

const CHANNEL_FORM_DEFAULTS: ProjectFormValues = {
  platform: 'article',
  agent_config: {},
  name: '',
  profile_url: '',
  avatar_url: '',
  wechat_app_id: '',
  wechat_secret: '',
  keywords: '',
  instructions: '',
  visual_style: '',
  writer: '',
  theme: '',
  author: '',
  portrait_reference_image: null,
  ecommerce_default_selected_modules: {},
  ecommerce_target_platform: '',
  ecommerce_brand_brief: '',
  ecommerce_image_capability_key: '',
  montage_defaults: {
    default_pipeline: '',
    preferences: {
      duration_seconds: undefined,
      style: '',
      music_prompt: '',
      subtitle_mode: '',
      voiceover_mode: '',
    },
    asset_guidance: '',
    delivery_targets: [],
  },
  reference_image: null,
  image_ratio: '3:4',
}

function projectPlatformFromIntent(type: string | undefined, isAdmin: boolean): ProjectPlatform {
  switch (type) {
    case 'article':
    case 'seednote':
    case 'montage':
      return type
    case 'moments':
    case 'ecommerce':
      return isAdmin ? type : CHANNEL_FORM_DEFAULTS.platform
    default:
      return CHANNEL_FORM_DEFAULTS.platform
  }
}

function projectToForm(ch: Project): ProjectFormValues {
  return {
    platform: ch.platform,
    agent_config: ch.agent_config ?? {},
    name: ch.name || '',
    profile_url: ch.profile_url || '',
    avatar_url: ch.avatar_url || '',
    wechat_app_id: ch.config?.wechat_app_id || '',
    wechat_secret: '',
    keywords: ch.keywords || '',
    instructions: ch.instructions || ch.positioning || '',
    visual_style: ch.visual_style || '',
    writer: ch.writer || '',
    theme: ch.theme || '',
    author: ch.author || '',
    ecommerce_default_selected_modules: ch.ecommerce_defaults?.default_selected_modules || {},
    ecommerce_target_platform: ch.ecommerce_defaults?.target_platform || '',
    ecommerce_brand_brief: ch.ecommerce_defaults?.brand_brief || '',
    ecommerce_image_capability_key: ch.ecommerce_defaults?.image_capability_key || '',
    montage_defaults: {
      ...CHANNEL_FORM_DEFAULTS.montage_defaults,
      ...(ch.montage_defaults || {}),
      preferences: {
        ...CHANNEL_FORM_DEFAULTS.montage_defaults?.preferences,
        ...(ch.montage_defaults?.preferences || {}),
      },
      delivery_targets: ch.montage_defaults?.delivery_targets || [],
    },
    reference_image: ch.reference_image ?? null,
    portrait_reference_image: ch.portrait_reference_image ?? null,
    image_ratio: (ch.image_ratio as ProjectFormValues['image_ratio']) || 'auto',
  }
}

export default function ProjectsPage() {
  const { user } = useAuth()
  const isAdmin = user?.is_admin === true
  const agentPacksQuery = useAgentPacks()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const createIntent = parseCreationIntent(searchParams)
  const returnTo = searchParams.get('return_to')
  const [statusFilter, setStatusFilter] = useState('all')
  const [searchFilter, setSearchFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const [fetchingProfile, setFetchingProfile] = useState(false)
  const [profileFetchHint, setProfileFetchHint] = useState<string | null>(null)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [editingAnalysis, setEditingAnalysis] = useState<ImageAnalysis | null>(null)
  const editingAnalysisUpdatedAtRef = useRef('')
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [montageDefaultsReady, setMontageDefaultsReady] = useState(false)
  const { submit } = useSubmitLock()

  const form = useForm<ProjectFormValues>({
    resolver: zodResolver(projectSchema) as Resolver<ProjectFormValues>,
    defaultValues: CHANNEL_FORM_DEFAULTS,
  })

  const selectedPlatform = useWatch({ control: form.control, name: 'platform' })
  const watchedAgentConfig = useWatch({ control: form.control, name: 'agent_config' }) ?? {}
  const selectedAgentPack = useMemo(
    () => agentPacksQuery.data?.packs.find((pack) => pack.bindings.project_platforms?.includes(selectedPlatform)),
    [agentPacksQuery.data, selectedPlatform],
  )
  const isWechat = selectedPlatform === 'article'
  const isSeednote = selectedPlatform === 'seednote'
  const isMoments = selectedPlatform === 'moments'
	const isEcommerce = selectedPlatform === 'ecommerce'
	const isMontage = selectedPlatform === 'montage'
	const supportsVisualReference = true
  const profileUrl = useWatch({ control: form.control, name: 'profile_url' })
  const enablePublishing = isWechat
  const referenceImage = useWatch({ control: form.control, name: 'reference_image' })
  const portraitReferenceImage = useWatch({ control: form.control, name: 'portrait_reference_image' })
  const authorValue = useWatch({ control: form.control, name: 'author' })
  const writerValue = useWatch({ control: form.control, name: 'writer' })
  const themeValue = useWatch({ control: form.control, name: 'theme' })
  const ecommerceModules = useWatch({ control: form.control, name: 'ecommerce_default_selected_modules' })
  const {
    items: imageCapabilityOptions,
    defaultCapability: defaultImageCapability,
    isLoading: imageCapabilitiesLoading,
  } = useImageCapabilities()

  const setEcommerceModuleQty = (key: string, qty: number) => {
    const cur = form.getValues('ecommerce_default_selected_modules') ?? {}
    const next = { ...cur }
    if (qty >= 1) next[key] = qty
    else delete next[key]
    form.setValue('ecommerce_default_selected_modules', next, { shouldDirty: true })
  }

  // Auto-focus profile_url field when dialog opens
  useEffect(() => {
    if (modalOpen) {
      setTimeout(() => form.setFocus('profile_url'), 100)
    }
  }, [modalOpen, form])

  // Warn before closing with unsaved changes
  useFormDirtyCheck(form, modalOpen)

  const { data: platformConfigs } = useQuery({
    queryKey: ['platform-configs'],
    queryFn: () => api.projects.platformConfigs(),
    staleTime: Infinity,
  })

  const platformConfigMap = useMemo(() => {
    const map: Record<string, PlatformConfig> = {}
    if (platformConfigs) {
      for (const pc of platformConfigs) {
        map[pc.id] = pc
      }
    }
    return map
  }, [platformConfigs])

  const currentPlatformConfig = platformConfigMap[selectedPlatform]
  const hasProfileField = currentPlatformConfig?.fields?.some((field) => field.key === 'profile_url') ?? false

  function extractSupportedProfileUrl(value: string) {
    const match = value.match(/https?:\/\/((m\.|www\.)?xiaohongshu\.com|xhslink\.com)\/[^\s"'<>，。！？；、]+/i)
    return match?.[0]?.replace(/[.,;:!?)]}）】。！？；，、]+$/g, '') || ''
  }

  function hasSupportedProfileUrl(value?: string) {
    return !!value && !!extractSupportedProfileUrl(value)
  }

  const skipAutoFetchRef = useRef(false)
  useEffect(() => {
    if (skipAutoFetchRef.current) {
      skipAutoFetchRef.current = false
      return
    }
    if (!modalOpen || !profileUrl || !selectedPlatform || !hasSupportedProfileUrl(profileUrl)) return
    const pc = platformConfigMap[selectedPlatform]
    if (!pc?.supports_auto_fetch) return
    const timer = setTimeout(() => {
      void handleFetchProfile(profileUrl, { silent: true })
    }, 800)
    return () => clearTimeout(timer)
  }, [modalOpen, profileUrl, selectedPlatform, platformConfigMap])

  async function handleFetchProfile(url: string, options?: { silent?: boolean }) {
    if (!url || !selectedPlatform) return
    if (!hasSupportedProfileUrl(url)) {
      setProfileFetchHint('请先输入包含种草笔记链接的主页链接或分享文本')
      return
    }
    setFetchingProfile(true)
    setProfileFetchHint(null)
    try {
      const appId = form.getValues('wechat_app_id')
      const secret = form.getValues('wechat_secret')
      const profile = await api.projects.fetchProfile(selectedPlatform, url, appId, secret)
      if (profile.name) form.setValue('name', profile.name)
      if (profile.avatar_url) form.setValue('avatar_url', profile.avatar_url)
      if (profile.positioning) form.setValue('instructions', profile.positioning)
      if (profile.keywords) form.setValue('keywords', profile.keywords)
      if (profile.style) form.setValue('visual_style', profile.style)
      setProfileFetchHint('已更新项目信息')
      if (!options?.silent) {
        toast.success('已自动获取项目信息')
      }
    } catch {
      setProfileFetchHint('暂时无法自动获取，请继续手动填写')
      if (!options?.silent) {
        toast.error('获取项目信息失败，请手动填写')
      }
    } finally {
      setFetchingProfile(false)
    }
  }

  const { data: projects, isLoading, isError, refetch } = useQuery({
    queryKey: ['projects', statusFilter],
    queryFn: () =>
      api.projects.list({
        status: statusFilter === 'all' ? undefined : statusFilter,
      }),
    refetchInterval: (query) => {
      const items = query.state.data ?? []
      return items.some((project) => isImageAnalysisActive(project.image_analysis)) ? 2000 : false
    },
  })

  const visibleProjects = useMemo(
    () => (projects ?? []).filter((project) => canViewPlatform(project.platform, isAdmin)),
    [isAdmin, projects],
  )

  const filteredProjects = useMemo(() => {
    if (!searchFilter.trim()) return visibleProjects
    const q = searchFilter.toLowerCase()
    return visibleProjects.filter((ch) => ch.name.toLowerCase().includes(q))
  }, [searchFilter, visibleProjects])

  const { data: projectStats = {} } = useQuery({
    queryKey: ['project-stats', statusFilter, visibleProjects.map((project) => project.id).join(',')],
    queryFn: async () => {
      if (visibleProjects.length === 0) return {} as Record<string, ProjectStats>
      return api.projects.stats(visibleProjects.map((project) => project.id))
    },
    enabled: visibleProjects.length > 0,
  })

  const editingProjectQuery = useQuery({
    queryKey: queryKeys.projects.detail(editingProject?.id ?? ''),
    queryFn: ({ signal }) => api.projects.get(editingProject!.id, signal),
    enabled: modalOpen && Boolean(editingProject) && isImageAnalysisActive(editingAnalysis),
    staleTime: 0,
    refetchOnMount: 'always',
    refetchInterval: isImageAnalysisActive(editingAnalysis) ? 2000 : false,
  })

  useEffect(() => {
    const refreshed = editingProjectQuery.data?.project
    if (!refreshed) return
    const updatedAt = refreshed.image_analysis?.updated_at ?? ''
    if (isImageAnalysisUpdateOlder(updatedAt, editingAnalysisUpdatedAtRef.current)) return
    editingAnalysisUpdatedAtRef.current = updatedAt
    setEditingAnalysis(refreshed.image_analysis ?? null)
    form.setValue('visual_style', refreshed.visual_style ?? '')
    form.setValue('reference_image', refreshed.reference_image ?? null)
  }, [editingProjectQuery.data, form])

  useEffect(() => {
    const editID = searchParams.get('edit')
    if (!editID || modalOpen) return
    const project = visibleProjects.find((item) => item.id === editID)
    if (project) openEdit(project)
  }, [modalOpen, searchParams, visibleProjects])

  const createMutation = useMutation({
    mutationFn: (data: CreateProjectRequest) => api.projects.create(data),
    onSuccess: (created) => {
      toast.success(created.project.image_analysis ? '项目已创建，正在后台识别视觉风格' : '项目创建成功')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
      const createdProject = created.project
      if (returnTo && createdProject?.id) {
        resetModal()
        navigate(projectCreatedReturnHref({
          returnTo,
          type: createdProject.platform,
          projectId: createdProject.id,
          intent: createIntent.intent,
        }))
        return
      }
      resetModal()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '创建项目失败，请重试'))
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<CreateProjectRequest> }) =>
      api.projects.update(id, data),
    onSuccess: () => {
      toast.success('项目更新成功')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
      resetModal()
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '更新项目失败，请重试'))
    },
  })

  const archiveMutation = useMutation({
    mutationFn: (id: string) => api.projects.archive(id),
    onSuccess: () => {
      toast.success('项目已归档')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
    },
    onError: () => {
      toast.error('归档项目失败，请重试')
    },
  })

  const restoreMutation = useMutation({
    mutationFn: (id: string) => api.projects.restore(id),
    onSuccess: () => {
      toast.success('项目已恢复')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
    },
    onError: () => {
      toast.error('恢复项目失败，请重试')
    },
  })

  function openCreate() {
    setEditingProject(null)
    setProfileFetchHint(null)
    const platform = projectPlatformFromIntent(createIntent.type, isAdmin)
    form.reset({
      ...CHANNEL_FORM_DEFAULTS,
      platform,
      image_ratio: (platformConfigMap[platform]?.default_image_ratio || 'auto') as ProjectFormValues['image_ratio'],
    })
    setEditingAnalysis(null)
    editingAnalysisUpdatedAtRef.current = ''
    setReferenceUploading(false)
    setMontageDefaultsReady(false)
    setModalOpen(true)
  }

  useEffect(() => {
    if (!createIntent.shouldCreate || modalOpen) return
    openCreate()
  }, [createIntent.shouldCreate, modalOpen])

  function openEdit(project: Project) {
    setEditingProject(project)
    setProfileFetchHint(null)
    setEditingAnalysis(project.image_analysis ?? null)
    editingAnalysisUpdatedAtRef.current = project.image_analysis?.updated_at ?? ''
    setReferenceUploading(false)
    setMontageDefaultsReady(false)
    form.reset(projectToForm(project))
    skipAutoFetchRef.current = true
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
    setEditingProject(null)
    setProfileFetchHint(null)
    setEditingAnalysis(null)
    editingAnalysisUpdatedAtRef.current = ''
    setReferenceUploading(false)
    setMontageDefaultsReady(false)
    form.reset(CHANNEL_FORM_DEFAULTS)
    if (createIntent.shouldCreate) {
      setSearchParams({}, { replace: true })
    } else if (searchParams.has('edit')) {
      const next = new URLSearchParams(searchParams)
      next.delete('edit')
      setSearchParams(next, { replace: true })
    }
  }

  async function onSubmit(values: ProjectFormValues) {
    const submittedImageCapability = imageCapabilityOptions.find(
      (option) => option.key === (values.ecommerce_image_capability_key || defaultImageCapability),
    )
    if (
      values.platform === 'ecommerce'
      && (
        !submittedImageCapability
        || submittedImageCapability.enabled !== true
        || submittedImageCapability.price_available !== true
      )
    ) {
      toast.error('该图像能力已停用，请重新选择')
      return
    }
    const payload: CreateProjectRequest = {
      platform: values.platform,
      agent_config: values.agent_config,
      name: values.name?.trim() || undefined,
      profile_url: values.profile_url?.trim() || undefined,
      avatar_url: values.avatar_url?.trim() || undefined,
      keywords: values.keywords?.trim() || undefined,
      instructions: values.instructions?.trim() || undefined,
      writer: values.writer?.trim() || undefined,
      theme: values.theme?.trim() || undefined,
      author: values.platform === 'article' || values.platform === 'moments'
        ? values.author?.trim() || undefined
        : undefined,
      image_ratio: values.image_ratio,
      wechat_app_id: values.wechat_app_id?.trim() || undefined,
      wechat_secret: values.wechat_secret?.trim() || undefined,
    }
    if (values.platform !== 'montage' && (!editingProject || form.formState.dirtyFields.visual_style)) {
      payload.visual_style = values.visual_style?.trim() ?? ''
    }
    if (values.platform === 'ecommerce') {
      payload.ecommerce_defaults = {
        default_selected_modules: values.ecommerce_default_selected_modules || {},
        target_platform: values.ecommerce_target_platform || undefined,
        brand_brief: values.ecommerce_brand_brief?.trim() || undefined,
        image_capability_key: values.ecommerce_image_capability_key || undefined,
      }
    }
    if (values.platform === 'montage') {
      payload.montage_defaults = {
        default_pipeline: values.montage_defaults?.default_pipeline?.trim() || undefined,
        preferences: {
          duration_seconds: values.montage_defaults?.preferences?.duration_seconds,
          style: values.montage_defaults?.preferences?.style?.trim() || undefined,
          music_prompt: values.montage_defaults?.preferences?.music_prompt?.trim() || undefined,
          subtitle_mode: values.montage_defaults?.preferences?.subtitle_mode?.trim() || undefined,
          voiceover_mode: values.montage_defaults?.preferences?.voiceover_mode?.trim() || undefined,
        },
        asset_guidance: values.montage_defaults?.asset_guidance?.trim() || undefined,
        delivery_targets: values.montage_defaults?.delivery_targets || [],
      }
    }
    const referenceImage = referenceSelectionFromValue(values.reference_image)
    const portraitReferenceImage = referenceSelectionFromValue(values.portrait_reference_image)
    if (values.platform === 'article') payload.portrait_reference_image = portraitReferenceImage
    if (editingProject) {
      const updatePayload = {
        ...payload,
        ...(form.formState.dirtyFields.reference_image ? { reference_image: referenceImage } : {}),
        ...(form.formState.dirtyFields.portrait_reference_image ? { portrait_reference_image: portraitReferenceImage } : {}),
      }
      await submit(async () => updateMutation.mutateAsync({ id: editingProject.id, data: updatePayload })).catch(() => {})
    } else {
      const createPayload = {
        ...payload,
        ...(referenceImage ? { reference_image: referenceImage } : {}),
        ...(portraitReferenceImage ? { portrait_reference_image: portraitReferenceImage } : {}),
      }
      await submit(async () => createMutation.mutateAsync(createPayload)).catch(() => {})
    }
  }

  function handleProjectSubmit(event?: React.BaseSyntheticEvent) {
    if (referenceUploading || (isMontage && !montageDefaultsReady)) {
      event?.preventDefault()
      return
    }
    return form.handleSubmit(onSubmit)(event)
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-6">
      <PageHeader title="项目">
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建项目
        </Button>
      </PageHeader>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <ToggleGroup
          value={[statusFilter]}
          onValueChange={(val) => setStatusFilter(val[0] || 'all')}
          variant="outline"
          size="sm"
          spacing={0}
          aria-label="项目状态"
        >
          {statusTabs.map((tab) => (
            <ToggleGroupItem key={tab.value} value={tab.value}>
              {tab.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>

        <SearchInput
          value={searchFilter}
          onChange={setSearchFilter}
          placeholder="搜索项目"
          className="w-full sm:w-64"
        />
      </div>

      {isError ? (
        <QueryErrorState onRetry={() => refetch()} />
      ) : isLoading ? (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="rounded-xl border border-border bg-card p-4 space-y-3">
              <div className="flex items-center gap-3">
                <Skeleton className="h-10 w-10 rounded-full" />
                <div className="space-y-1.5 flex-1">
                  <Skeleton className="h-4 w-24" />
                  <Skeleton className="h-3 w-16" />
                </div>
              </div>
              <Skeleton className="h-3 w-full" />
              <div className="flex gap-2">
                <Skeleton className="h-5 w-12" />
                <Skeleton className="h-5 w-12" />
                <Skeleton className="h-5 w-12" />
              </div>
            </div>
          ))}
        </div>
      ) : filteredProjects.length === 0 ? (
        <EmptyState
          icon={Inbox}
          title={!visibleProjects.length ? (statusFilter === 'all' ? '还没有项目' : statusFilter === 'active' ? '没有活跃的项目' : '没有已归档的项目') : '未找到匹配的项目'}
          description={!visibleProjects.length ? '创建项目后即可开始创作。' : '换个关键词试试'}
          action={!visibleProjects.length ? { label: '新建项目', onClick: openCreate } : undefined}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filteredProjects.map((project) => (
            <ProjectCard
              key={project.id}
              project={project}
              stats={projectStats[project.id]}
              onEdit={openEdit}
              archiving={archiveMutation.isPending}
              restoring={restoreMutation.isPending}
              onArchive={(id) => { void submit(async () => archiveMutation.mutateAsync(id)).catch(() => {}) }}
              onRestore={(id) => { void submit(async () => restoreMutation.mutateAsync(id)).catch(() => {}) }}
            />
          ))}
        </div>
      )}

      {/* Create/Edit Dialog */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingProject ? '编辑项目' : '新建项目'}</DialogTitle>
          </DialogHeader>
          <Form {...form}>
            <form id="project-form" onSubmit={handleProjectSubmit} className="max-h-[72vh] space-y-4 overflow-y-auto p-1">
              <FormField control={form.control} name="platform" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">平台</FormLabel>
                  <FormControl>
                    <Select
                      value={field.value}
                      onValueChange={(v) => {
                        setMontageDefaultsReady(false)
                        field.onChange(v)
                        form.setValue('agent_config', {}, { shouldDirty: true })
                        form.setValue(
                          'image_ratio',
                          (v ? platformConfigMap[v]?.default_image_ratio : 'auto') as ProjectFormValues['image_ratio'],
                          { shouldDirty: true, shouldValidate: true },
                        )
                        form.setValue('wechat_app_id', '')
                        form.setValue('wechat_secret', '')
                      }}
                      disabled={!!editingProject}
                    >
                      <SelectTrigger className="w-full">
                        {selectedPlatform ? (
                          <span className="flex items-center gap-1.5">
                            {selectedPlatform ? renderPlatformIcon(selectedPlatform) : null}
                            {platformOptions.find(o => o.value === selectedPlatform)?.label || selectedPlatform}
                          </span>
                        ) : (
                          <SelectValue placeholder="选择平台" />
                        )}
                      </SelectTrigger>
                      <SelectContent>
                        {platformOptions.filter((opt) => canViewPlatform(opt.value, isAdmin)).map((opt) => (
                          <SelectItem key={opt.value} value={opt.value} label={opt.label}>
                            <span className="flex items-center gap-1.5">
                              {renderPlatformIcon(opt.value)}
                              {opt.label}
                            </span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <AgentPackSchemaFields
                pack={selectedAgentPack}
                surface="project"
                value={watchedAgentConfig}
                onChange={(value) => form.setValue('agent_config', value, { shouldDirty: true, shouldValidate: true })}
              />

              {hasProfileField && <FormField control={form.control} name="profile_url" render={({ field }) => (
                <FormItem>
                  <div className="flex items-start gap-3">
                    <FormLabel className="shrink-0 w-20 text-right pt-2">项目主页</FormLabel>
                    <FormControl>
                      <div className="flex gap-2 flex-1">
                        <Textarea
                          className="flex-1 min-w-0"
                          placeholder="粘贴种草笔记主页链接或分享文本..."
                          {...field}
                        />
                        {currentPlatformConfig?.supports_auto_fetch && (
                          <Button
                            type="button"
                            variant="secondary"
                            size="sm"
                            loading={fetchingProfile}
                            disabled={!field.value || !hasSupportedProfileUrl(field.value || '')}
                            onClick={() => void handleFetchProfile(field.value || '')}
                          >
                            获取
                          </Button>
                        )}
                      </div>
                    </FormControl>
                  </div>
                  {currentPlatformConfig?.supports_auto_fetch && (
                    <FormDescription>
                      {profileFetchHint || '粘贴种草笔记分享文本后会自动提取链接、分析项目和代表作品。'}
                    </FormDescription>
                  )}
                  <FormMessage />
                </FormItem>
              )} />}

              <FormField control={form.control} name="name" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">项目名称</FormLabel>
                  <FormControl>
                    <Input className="flex-1 min-w-0" placeholder="例如 我的科技博客" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="avatar_url" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">头像</FormLabel>
                  <FormControl>
                    <Input className="flex-1 min-w-0" placeholder="自动获取或手动填写 URL" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="instructions" render={({ field }) => (
                <FormItem>
                  <FormLabel>项目定位</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="例如 面向开发者的实用 AI 教程"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              {isWechat && (
                <>
                  <div className="border-t border-border pt-4">
                    <h4 className="mb-3 text-sm font-medium text-muted-foreground">发布配置</h4>
                  </div>

                  <div className="rounded-md border bg-muted/20 px-3 py-2 text-sm">
                    <p className="font-medium">自动识别公众号能力</p>
                    <p className="mt-1 text-xs text-muted-foreground">系统先创建草稿；具备正式发布权限时继续发布，无权限时保留草稿并提示人工发布。</p>
                  </div>

                  {enablePublishing && (
                    <>
                      <FormField control={form.control} name="wechat_app_id" render={({ field }) => (
                        <FormItem>
                          <FormLabel>微信 AppID</FormLabel>
                          <FormControl>
                            <Input className="flex-1 min-w-0" placeholder="wx..." {...field} />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )} />

                      <FormField control={form.control} name="wechat_secret" render={({ field }) => (
                        <FormItem>
                          <FormLabel>微信 AppSecret</FormLabel>
                          <FormControl>
                            <Input
                              type="password"
                              placeholder={editingProject ? '留空则保持原有密钥不变' : '创建后不可查看'}
                              {...field}
                            />
                          </FormControl>
                          {editingProject && (
                            <FormDescription>留空则保持原有密钥不变</FormDescription>
                          )}
                          <FormMessage />
                        </FormItem>
                      )} />
                    </>
                  )}
                </>
              )}

              <div className="border-t border-border pt-4">
                <h4 className="mb-3 text-sm font-medium text-muted-foreground">高级设置</h4>
              </div>

              <FormField control={form.control} name="keywords" render={({ field }) => (
                <FormItem>
                  <FormLabel>关键词</FormLabel>
                  <FormControl>
                    <TagInput
                      value={field.value}
                      onChange={field.onChange}
                      placeholder="输入后按回车添加标签"
                    />
                  </FormControl>
                  <FormDescription>按回车或逗号添加标签，用于内容生成</FormDescription>
                  <FormMessage />
                </FormItem>
              )} />

              {supportsVisualReference && isMontage ? (
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-foreground">人物参考</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">封面需要本人出镜时，系统会把这张人物参考图提供给 Agent。</p>
                  </div>
                  <ReferenceAssetUpload
                    value={referenceImage ?? null}
                    onChange={(value) => form.setValue('reference_image', value, { shouldDirty: true, shouldValidate: true })}
                    purpose="project_reference"
                    onUploadingChange={setReferenceUploading}
                  />
                </div>
              ) : null}

              {!isMontage ? (
                <FormField control={form.control} name="visual_style" render={({ field }) => (
                  <FormItem>
                    <FormLabel>视觉参考与风格</FormLabel>
                    <FormControl>
                      <AnalyzedImageField
                        asset={referenceImage ?? null}
                        onAssetChange={(value) => form.setValue('reference_image', value, { shouldDirty: true, shouldValidate: true })}
                        purpose="project_reference"
                        text={field.value ?? ''}
                        onTextChange={field.onChange}
                        analysis={editingAnalysis}
                        placeholder={isSeednote
                          ? '可手动描述风格；留空时在创建后后台识别色彩、质感与构图'
                          : isMoments
                            ? '可手动描述真实生活感、光线和画面调性；留空时后台识别'
                            : isEcommerce
                              ? '可手动描述品牌视觉基线；留空时后台识别'
                              : '可手动描述封面与配图风格；留空时后台识别'}
                        onUploadingChange={setReferenceUploading}
                        onBeforeAnalysisAction={() => editingProject
                          ? queryClient.cancelQueries({ queryKey: queryKeys.projects.detail(editingProject.id) })
                          : undefined}
                        onAnalysisAction={async () => {
                          if (!editingProject) return
                          const refreshed = await api.projects.get(editingProject.id)
                          queryClient.setQueryData(queryKeys.projects.detail(editingProject.id), refreshed)
                          editingAnalysisUpdatedAtRef.current = refreshed.project.image_analysis?.updated_at ?? ''
                          setEditingAnalysis(refreshed.project.image_analysis ?? null)
                          form.setValue('visual_style', refreshed.project.visual_style ?? '')
                          form.setValue('reference_image', refreshed.project.reference_image ?? null)
                          await queryClient.invalidateQueries({ queryKey: ['projects'] })
                        }}
                      />
                    </FormControl>
                    <FormDescription>图片和文本在识别期间保持锁定；停止识别后可手动填写。</FormDescription>
                    <FormMessage />
                  </FormItem>
                )} />
              ) : null}

              {isWechat && (
                <div className="space-y-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-foreground">默认人物参考</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">公众号封面需要人物出镜时，任务和计划可直接使用这张图。</p>
                    </div>
                    <ReferenceAssetUpload
                      value={portraitReferenceImage ?? null}
                      onChange={(value) => form.setValue('portrait_reference_image', value, { shouldDirty: true, shouldValidate: true })}
                      purpose="project_portrait_reference"
                      ariaLabel="人物参考图文件"
                      onUploadingChange={setReferenceUploading}
                    />
                  </div>
                </div>
              )}

			  <FormField control={form.control} name="image_ratio" render={({ field }) => (
				  <FormItem>
					<FormLabel>{isMontage ? '默认视频比例' : '默认图像比例'}</FormLabel>
                    <FormControl>
                      <ImageAspectRatioField
                        value={field.value || 'auto'}
                        defaultValue={currentPlatformConfig?.default_image_ratio}
                        ratios={currentPlatformConfig?.supported_image_ratios ?? []}
                        onChange={field.onChange}
                      />
                    </FormControl>
					<FormDescription>{isMontage ? '视频与封面使用同一个比例。' : '选择智能适配时，每个任务可再明确指定，或由创作流程按产物决定。'}</FormDescription>
					<FormMessage />
				  </FormItem>
			  )} />

              {isMontage && <MontageProjectDefaultsPanel form={form} onReadyChange={setMontageDefaultsReady} />}

              {isEcommerce && (
                <>
                  <div className="space-y-3 rounded-lg border border-border p-3">
                    <div>
                      <p className="text-sm font-medium text-foreground">电商默认配置</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">新建电商任务时会预填这些项目默认值，产品图和卖点仍在任务里填写。</p>
                    </div>
                    <div className="divide-y divide-border">
                      {ecommerceModuleCatalog.map((mod) => {
                        const qty = ecommerceModules?.[mod.key] ?? 0
                        const enabled = qty >= 1
                        return (
                          <div key={mod.key} className="flex items-center justify-between gap-3 py-2">
                            <div className="min-w-0 flex-1">
                              <p className="text-sm font-medium text-foreground">{mod.label}</p>
                              <p className="mt-0.5 text-xs text-muted-foreground">{mod.hint}</p>
                            </div>
                            <div className="flex items-center gap-2">
                              {enabled && (
                                <div className="flex items-center gap-1">
                                  <Button type="button" variant="outline" size="sm" className="h-7 w-7 p-0" onClick={() => setEcommerceModuleQty(mod.key, Math.max(mod.minQty, qty - mod.qtyStep))} aria-label="减少">
                                    <Minus className="h-3 w-3" />
                                  </Button>
                                  <span className="w-8 text-center text-sm tabular-nums">{qty}{mod.qtyLabel}</span>
                                  <Button type="button" variant="outline" size="sm" className="h-7 w-7 p-0" onClick={() => setEcommerceModuleQty(mod.key, Math.min(mod.maxQty, qty + mod.qtyStep))} aria-label="增加">
                                    <Plus className="h-3 w-3" />
                                  </Button>
                                </div>
                              )}
                              <Switch checked={enabled} onCheckedChange={(on) => setEcommerceModuleQty(mod.key, on ? mod.defaultQty : 0)} aria-label={`启用 ${mod.label}`} />
                            </div>
                          </div>
                        )
                      })}
                    </div>
                    <FormField control={form.control} name="ecommerce_target_platform" render={({ field }) => (
                      <FormItem>
                        <FormLabel>默认目标平台</FormLabel>
                        <Select value={field.value || undefined} onValueChange={field.onChange}>
                          <FormControl>
                            <SelectTrigger className="w-full"><SelectValue placeholder="选择投放平台" /></SelectTrigger>
                          </FormControl>
                          <SelectContent>
                            {ecommerceTargetPlatformOptions.map((opt) => (
                              <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )} />
                    <FormField control={form.control} name="ecommerce_image_capability_key" render={({ field }) => (
                      <FormItem>
                        <FormLabel>默认图像能力</FormLabel>
                        <FormControl>
                          {imageCapabilitiesLoading ? (
                            <Skeleton className="h-10 w-full rounded-xl" />
                          ) : (
                            <ImageCapabilitySelector options={imageCapabilityOptions} value={field.value || defaultImageCapability} onChange={field.onChange} />
                          )}
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )} />
                    <FormField control={form.control} name="ecommerce_brand_brief" render={({ field }) => (
                      <FormItem>
                        <FormLabel>品牌 brief</FormLabel>
                        <FormControl>
                          <Textarea {...field} placeholder="品牌定位、受众、调性、禁忌和固定视觉要求" className="min-h-[72px] resize-y" />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )} />
                  </div>
                </>
              )}

              {isWechat && (
                <>
                  {/* 写作风格（作者署名 + 写作风格模仿 + 可选头像）与排版：始终绑定到项目自身字段。 */}
                  <PersonaBlock
                    author={authorValue ?? ''}
                    onAuthor={(v) => form.setValue('author', v, { shouldDirty: true })}
                    writer={writerValue ?? ''}
                    onWriter={(v) => form.setValue('writer', v, { shouldDirty: true })}
                  />
                  <ThemePicker theme={themeValue ?? ''} onTheme={(v) => form.setValue('theme', v, { shouldDirty: true })} />
                </>
              )}

            </form>
          </Form>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button type="submit" form="project-form" loading={isSubmitting} disabled={referenceUploading || (isMontage && !montageDefaultsReady)}>
              {editingProject ? '更新' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
