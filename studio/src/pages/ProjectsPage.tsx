import { useState, useEffect, useMemo, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, Inbox, Loader2, Minus } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/lib/api'
import type { Project, ProjectPlatform, ProjectStats, CreateProjectRequest, PlatformConfig, Template, TemplateType } from '@/types'
import { getApiErrorMessage } from '@/lib/http-client'
import { ProjectCard } from '@/components/ProjectCard'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { ReferenceAssetUpload } from '@/components/projects/ReferenceAssetUpload'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { TemplatePicker } from '@/components/templates/TemplatePicker'
import { PersonaBlock } from '@/components/templates/PersonaBlock'
import { ThemePicker } from '@/components/templates/ThemePicker'
import { ImageCapabilitySelector } from '@/components/ImageCapabilitySelector'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { projectSchema, type ProjectFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { renderPlatformIcon } from '@/lib/PlatformIcon'
import { ecommerceModuleCatalog, ecommerceTargetPlatformOptions } from '@/lib/labels'
import { useImageCapabilities } from '@/hooks/useImageModels'
import { createTaskHref, parseCreationIntent, projectCreatedReturnHref } from '@/lib/command-center'
import { MontageProjectDefaultsPanel } from '@/components/montage/MontageProjectDefaultsPanel'
import { AgentPackSchemaFields } from '@/components/agent-pack/AgentPackSchemaFields'
import { useAgentPacks } from '@/hooks/useAgentPacks'
import { referenceSelectionFromValue } from '@/lib/reference-image'

const platformOptions: { value: ProjectPlatform; label: string }[] = [
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号' },
  { value: 'moments', label: '朋友圈' },
  { value: 'ecommerce', label: '电商出图' },
  { value: 'montage', label: 'Montage' },
]

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
  template_id: '',
  ecommerce_default_selected_modules: {},
  ecommerce_target_platform: '',
  ecommerce_brand_brief: '',
  ecommerce_image_model_key: '',
  montage_defaults: {
    default_pipeline: '',
    preferences: {
      aspect_ratio: '9:16',
      duration_seconds: 30,
      style: '',
      music_prompt: '',
      subtitle_mode: '',
      voiceover_mode: '',
    },
    asset_guidance: '',
    delivery_targets: [],
  },
  reference_image: null,
  image_ratio: '',
  enable_publishing: false,
  require_publish_approval: false,
}

function projectPlatformFromIntent(type?: string): ProjectPlatform {
  switch (type) {
    case 'article':
    case 'seednote':
    case 'moments':
    case 'ecommerce':
    case 'montage':
      return type
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
    template_id: ch.template_id || '',
    ecommerce_default_selected_modules: ch.ecommerce_defaults?.default_selected_modules || {},
    ecommerce_target_platform: ch.ecommerce_defaults?.target_platform || '',
    ecommerce_brand_brief: ch.ecommerce_defaults?.brand_brief || '',
    ecommerce_image_model_key: ch.ecommerce_defaults?.image_model_key || '',
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
    image_ratio: (ch.image_ratio as '' | '3:4' | '1:1' | '4:3' | '16:9') || '',
    enable_publishing: ch.config?.enable_publishing ?? false,
    require_publish_approval: ch.config?.require_publish_approval ?? false,
  }
}

export default function ProjectsPage() {
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
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [fetchingProfile, setFetchingProfile] = useState(false)
  const [profileFetchHint, setProfileFetchHint] = useState<string | null>(null)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [analyzingStyle, setAnalyzingStyle] = useState(false)
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [referenceAnalysisUrl, setReferenceAnalysisUrl] = useState('')
  // 导入模型：项目 Owns 自己的人设（视觉/写作风格/排版/作者）。selectedTemplate 仅
  // 用于 TemplatePicker 的高亮，标记"当前按哪个模板导入"——不写入表单，提交时也不发送
  // template_id（后端 Update 无条件清空，存量绑定项目保存即迁移为自有值）。
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null)
  const styleManuallyEditedRef = useRef(false)
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
  const visualTemplateType: TemplateType | null = isWechat ? 'article' : isSeednote ? 'seednote' : isEcommerce ? 'ecommerce' : null
  const supportsVisualReference = !!visualTemplateType || isMoments
  const profileUrl = useWatch({ control: form.control, name: 'profile_url' })
  const enablePublishing = useWatch({ control: form.control, name: 'enable_publishing' })
  const referenceImage = useWatch({ control: form.control, name: 'reference_image' })
  const authorValue = useWatch({ control: form.control, name: 'author' })
  const writerValue = useWatch({ control: form.control, name: 'writer' })
  const themeValue = useWatch({ control: form.control, name: 'theme' })
  const ecommerceModules = useWatch({ control: form.control, name: 'ecommerce_default_selected_modules' })
  const { items: imageModelOptions, isLoading: imageModelsLoading } = useImageCapabilities()

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
  // 递增令牌：openEdit 触发的模板异步回填在 .then 中比对，若对话框已切到别的项目则丢弃，
  // 避免陈旧回填串改其他项目的表单（与上方 cancelled 取消防护同一思路）。
  const projectEditTokenRef = useRef(0)

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

  // Auto-analyze reference image to fill visual style for image-based project types.
  useEffect(() => {
    if (!modalOpen || !referenceAnalysisUrl || !supportsVisualReference) return
    if (styleManuallyEditedRef.current) return

    let cancelled = false
    const timer = setTimeout(async () => {
      setAnalyzingStyle(true)
      try {
        const result = await api.projects.analyzeImage(referenceAnalysisUrl)
        if (!cancelled && result.visual_style && !styleManuallyEditedRef.current) {
          form.setValue('visual_style', result.visual_style)
        }
      } catch {
        toast.error('风格识别失败，请手动填写或重试')
      } finally {
        if (!cancelled) setAnalyzingStyle(false)
      }
    }, 1000)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [modalOpen, referenceAnalysisUrl, supportsVisualReference, form])

  // Reset manual-edit flag when modal reopens
  useEffect(() => {
    styleManuallyEditedRef.current = false
  }, [modalOpen])

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
  })

  const filteredProjects = useMemo(() => {
    if (!projects) return []
    if (!searchFilter.trim()) return projects
    const q = searchFilter.toLowerCase()
    return projects.filter((ch) => ch.name.toLowerCase().includes(q))
  }, [projects, searchFilter])

  const { data: projectStats = {} } = useQuery({
    queryKey: ['project-stats', statusFilter, projects?.map((project) => project.id).join(',')],
    queryFn: async () => {
      if (!projects || projects.length === 0) return {} as Record<string, ProjectStats>
      return api.projects.stats(projects.map((project) => project.id))
    },
    enabled: Boolean(projects && projects.length > 0),
  })

  const createMutation = useMutation({
    mutationFn: (data: CreateProjectRequest) => api.projects.create(data),
    onSuccess: (created) => {
      toast.success('项目创建成功')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
      const createdProject = created.project
      if (returnTo && createdProject?.id) {
        resetModal()
        navigate(projectCreatedReturnHref({
          returnTo,
          type: createIntent.type ?? createdProject.platform,
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

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.projects.delete(id),
    onSuccess: () => {
      toast.success('项目已删除')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
      setDeleteTarget(null)
    },
    onError: (err) => {
      toast.error(getApiErrorMessage(err, '删除项目失败，请重试'))
    },
  })

  function openCreate() {
    setEditingProject(null)
    setProfileFetchHint(null)
    form.reset({
      ...CHANNEL_FORM_DEFAULTS,
      platform: projectPlatformFromIntent(createIntent.type),
    })
    setSelectedTemplate(null)
    setReferenceAnalysisUrl('')
    setReferenceUploading(false)
    setModalOpen(true)
  }

  useEffect(() => {
    if (!createIntent.shouldCreate || modalOpen) return
    openCreate()
  }, [createIntent.shouldCreate, modalOpen])

  function openEdit(project: Project) {
    setEditingProject(project)
    setProfileFetchHint(null)
    setReferenceAnalysisUrl('')
    setReferenceUploading(false)
    form.reset(projectToForm(project))
    skipAutoFetchRef.current = true
    setSelectedTemplate(null)
    setModalOpen(true)

    // 存量兼容：旧项目可能仍带 template_id。打开编辑时只把模板视觉提示回填到空的
    // visual_style；author/writer/theme 已由项目自身维护，不再从模板导入。
    if (project.template_id) {
      const token = ++projectEditTokenRef.current
      api.templates
        .get(project.template_id)
        .then((t) => {
          // 对话框已切到别的项目（或重开）则丢弃这条陈旧回填，避免串改。
          if (projectEditTokenRef.current !== token) return
          setSelectedTemplate(t)
          if (!form.getValues('visual_style')) form.setValue('visual_style', t.style_prompt || '', { shouldDirty: false })
        })
        .catch(() => {
          /* 模板不存在/已删：保持空选，用户可手动选其他模板 */
        })
    }
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
    setSelectedTemplate(null)
    setReferenceAnalysisUrl('')
    setReferenceUploading(false)
    form.reset(CHANNEL_FORM_DEFAULTS)
    if (createIntent.shouldCreate) {
      setSearchParams({}, { replace: true })
    }
  }

  // 选模板=一次性把模板视觉提示导入表单（可编辑）。再次点击同一
  // 卡片为 no-op（保留用户对风格文本框的改动），切换到别的模板则覆盖。
  function handleProjectTemplateImport(template: Template) {
    if (selectedTemplate?.id === template.id) return
    setSelectedTemplate(template)
    form.setValue('visual_style', template.style_prompt || '', { shouldDirty: true })
  }

  async function onSubmit(values: ProjectFormValues) {
    const payload: CreateProjectRequest = {
      platform: values.platform,
      agent_config: values.agent_config,
      name: values.name?.trim() || undefined,
      profile_url: values.profile_url?.trim() || undefined,
      avatar_url: values.avatar_url?.trim() || undefined,
      keywords: values.keywords?.trim() || undefined,
      instructions: values.instructions?.trim() || undefined,
      visual_style: values.platform === 'montage' ? undefined : values.visual_style?.trim() || undefined,
      writer: values.writer?.trim() || undefined,
      theme: values.theme?.trim() || undefined,
      author: values.author?.trim() || undefined,
      // 导入模型：模板只用于填充视觉提示，不发送 template_id，不参与运行时解析。
      image_ratio: values.image_ratio || undefined,
      wechat_app_id: values.wechat_app_id?.trim() || undefined,
      wechat_secret: values.wechat_secret?.trim() || undefined,
      enable_publishing: values.enable_publishing || undefined,
      require_publish_approval: values.require_publish_approval || undefined,
    }
    if (values.platform === 'ecommerce') {
      payload.ecommerce_defaults = {
        default_selected_modules: values.ecommerce_default_selected_modules || {},
        target_platform: values.ecommerce_target_platform || undefined,
        brand_brief: values.ecommerce_brand_brief?.trim() || undefined,
        image_model_key: values.ecommerce_image_model_key || undefined,
      }
    }
    if (values.platform === 'montage') {
      payload.montage_defaults = {
        default_pipeline: values.montage_defaults?.default_pipeline?.trim() || undefined,
        preferences: {
          aspect_ratio: values.montage_defaults?.preferences?.aspect_ratio?.trim() || undefined,
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
    // Auto-set image_ratio based on platform if not specified
    if (!payload.image_ratio) {
      if (payload.platform === 'article') {
        payload.image_ratio = '16:9'
      } else if (payload.platform === 'seednote' || payload.platform === 'moments') {
        payload.image_ratio = '3:4'
      }
    }

    // Disable publishing flag when unchecked (credentials preserved)
    if (!values.enable_publishing) {
      payload.enable_publishing = false
      // Approval gate is moot when publishing is off; reset it so the stored
      // config stays consistent (avoids a lingering require flag with no publishing).
      payload.require_publish_approval = false
    }
    const referenceImage = referenceSelectionFromValue(values.reference_image)
    if (editingProject) {
      const updatePayload = form.formState.dirtyFields.reference_image
        ? { ...payload, reference_image: referenceImage }
        : payload
      await submit(async () => updateMutation.mutateAsync({ id: editingProject.id, data: updatePayload })).catch(() => {})
    } else {
      const createPayload = referenceImage ? { ...payload, reference_image: referenceImage } : payload
      await submit(async () => createMutation.mutateAsync(createPayload)).catch(() => {})
    }
  }

  function handleProjectSubmit(event?: React.BaseSyntheticEvent) {
    if (referenceUploading) {
      event?.preventDefault()
      return
    }
    return form.handleSubmit(onSubmit)(event)
  }

  function handleDelete(id: string) {
    setDeleteTarget(id)
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-6">
      <PageHeader title="项目" description="管理你的内容项目和发布配置。">
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建项目
        </Button>
      </PageHeader>

      {/* Status filter tabs */}
      <ToggleGroup
        value={[statusFilter]}
        onValueChange={(val) => setStatusFilter(val[0] || 'all')}
        variant="outline"
        size="sm"
        spacing={2}
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
        placeholder="搜索项目名称..."
        className="w-full max-w-xs"
      />

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
          title={!projects?.length ? (statusFilter === 'all' ? '还没有项目' : statusFilter === 'active' ? '没有活跃的项目' : '没有已归档的项目') : '未找到匹配的项目'}
          description={!projects?.length ? '创建你的第一个内容项目开始创作。' : '尝试其他搜索关键词'}
          action={!projects?.length ? { label: '新建项目', onClick: openCreate } : undefined}
          note={!projects?.length ? '配置好项目后，任务和计划都会自动继承对应的平台参数。' : undefined}
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
              onDelete={handleDelete}
              onCreateTask={(project) => navigate(createTaskHref({
                type: project.platform,
                projectId: project.id,
                intent: 'new',
              }))}
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
                        field.onChange(v)
                        form.setValue('agent_config', {}, { shouldDirty: true })
                        form.setValue('wechat_app_id', '')
                        form.setValue('wechat_secret', '')
                        form.setValue('enable_publishing', false)
                      }}
                      disabled={!!editingProject}
                    >
                      <SelectTrigger className="w-full">
                        {selectedPlatform ? (
                          <span className="flex items-center gap-1.5">
                            {renderPlatformIcon(selectedPlatform)}
                            {platformOptions.find(o => o.value === selectedPlatform)?.label || selectedPlatform}
                          </span>
                        ) : (
                          <SelectValue placeholder="选择平台" />
                        )}
                      </SelectTrigger>
                      <SelectContent>
                        {platformOptions.map((opt) => (
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

                  <FormField
                    control={form.control}
                    name="enable_publishing"
                    render={({ field }) => (
                      <FormItem>
                        <div className="flex items-center gap-2">
                          <FormControl>
                            <Switch
                              checked={field.value}
                              onCheckedChange={field.onChange}
                            />
                          </FormControl>
                          <FormLabel className="!mt-0 font-normal cursor-pointer" onClick={() => field.onChange(!field.value)}>
                            启用自动发布
                          </FormLabel>
                        </div>
                        <FormDescription>
                          {field.value
                            ? '开启后，任务完成后将自动发布到公众号'
                            : '未配置微信凭证，将无法使用自动发布到公众号功能'}
                          {field.value && (
                            <span className="mt-1 block">
                              请前往<a href="https://developers.weixin.qq.com/" target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">微信开发者</a>添加 API IP 白名单：47.108.177.204
                            </span>
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  {enablePublishing && (
                    <>
                      <FormField
                        control={form.control}
                        name="require_publish_approval"
                        render={({ field }) => (
                          <FormItem>
                            <div className="flex items-center gap-2">
                              <FormControl>
                                <Switch
                                  checked={field.value}
                                  onCheckedChange={field.onChange}
                                />
                              </FormControl>
                              <FormLabel className="!mt-0 cursor-pointer font-normal" onClick={() => field.onChange(!field.value)}>
                                发布前需人工审核
                              </FormLabel>
                            </div>
                            <FormDescription>
                              {field.value
                                ? '任务完成后暂停自动发布，进入「待审核发布」状态，需在任务详情手动放行后才发布到草稿箱'
                                : '任务完成后直接自动发布到公众号草稿箱'}
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />

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

              {supportsVisualReference && (
                <div className="space-y-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-foreground">视觉参考</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        上传一张参考图，系统会尝试识别色彩、质感和构图。
                      </p>
                    </div>
                    <ReferenceAssetUpload
                      value={referenceImage ?? null}
                      onChange={(value) => {
                        form.setValue('reference_image', value, { shouldDirty: true, shouldValidate: true })
                        if (!value) setReferenceAnalysisUrl('')
                      }}
                      purpose="project_reference"
                      onUploadingChange={setReferenceUploading}
                      onUploadedPreview={setReferenceAnalysisUrl}
                    />
                  </div>

                  {visualTemplateType && (
                    <TemplatePicker type={visualTemplateType} selected={selectedTemplate} onSelect={handleProjectTemplateImport} />
                  )}
                </div>
              )}

              {!isMontage ? (
                <FormField control={form.control} name="visual_style" render={({ field }) => (
                  <FormItem>
                    <FormLabel>视觉风格</FormLabel>
                    <FormControl>
                      <div className="relative">
                        <Textarea
                          placeholder={isSeednote
                            ? '描述图片视觉风格，如：手绘感、暖色调、小清新、治愈系水彩插画风格'
                            : isMoments
                              ? '描述图片视觉风格，如：真实生活感、轻杂志排版、暖色自然光、不过度营销'
                            : isEcommerce
                              ? '描述品牌视觉风格基线，如：高端极简白底、国潮暖橙插画、电商爆款高饱和促销感。作为主图/详情/封面跨图一致的视觉锚点'
                              : '描述文章封面与配图的视觉风格，如：温暖自然的生活摄影、柔光大地色系、写实治愈。留空则由项目定位与内容主题三维分析自动确定'}
                          className={analyzingStyle ? 'pr-10' : ''}
                          {...field}
                          onChange={(e) => {
                            styleManuallyEditedRef.current = true
                            field.onChange(e)
                          }}
                        />
                        {analyzingStyle && (
                          <div className="absolute right-2 top-2">
                            <div className="h-4 w-4 animate-spin rounded-full border-2 border-primary border-t-transparent" />
                          </div>
                        )}
                      </div>
                    </FormControl>
                    {analyzingStyle && (
                      <p className="text-xs text-muted-foreground">正在分析参考图...</p>
                    )}
                    <FormDescription>
                      {isSeednote
                        ? '用于封面和内容图的风格提示。'
                        : isMoments
                          ? '用于保持朋友圈素材的画面调性；留空则由内容自动判断。'
                        : isEcommerce
                          ? '作为电商素材跨图一致的视觉基线，产品图仍在任务里上传。'
                          : '仅决定封面与配图的视觉，与写作风格、排版样式相互独立。'}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )} />
              ) : null}

              {isMontage && <MontageProjectDefaultsPanel form={form} />}

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
                    <FormField control={form.control} name="ecommerce_image_model_key" render={({ field }) => (
                      <FormItem>
                        <FormLabel>默认图像能力</FormLabel>
                        <FormControl>
                          {imageModelsLoading ? (
                            <Skeleton className="h-10 w-full rounded-xl" />
                          ) : (
                            <ImageCapabilitySelector options={imageModelOptions} value={field.value || ''} onChange={field.onChange} />
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

              {/* 作者名（署名）：seednote 在此编辑；article 由上方公众号写作配置统一维护。 */}
              {isSeednote && <FormField control={form.control} name="author" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">作者名</FormLabel>
                  <FormControl>
                    <Input className="flex-1 min-w-0" placeholder="例如 张三" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />}
            </form>
          </Form>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button type="submit" form="project-form" loading={isSubmitting} disabled={referenceUploading}>
              {editingProject ? '更新' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(v) => { if (!v) setDeleteTarget(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确定要删除此项目吗？</AlertDialogTitle>
            <AlertDialogDescription>此操作不可撤销。删除后项目及其所有配置将永久移除。</AlertDialogDescription>
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
