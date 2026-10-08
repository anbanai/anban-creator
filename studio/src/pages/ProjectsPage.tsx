import { useState, useEffect, useMemo, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, Inbox } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { FieldHint } from '@/components/common/FieldHint'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/lib/api'
import type { ImageAnalysis, Project, ProjectPlatform, ProjectStats, CreateProjectRequest, ProjectProfile, ProfileAnalysisAnswers, ProfileAnalysisStatus } from '@/types'
import { isImageAnalysisActive, isImageAnalysisUpdateOlder } from '@/types'
import { getApiErrorMessage } from '@/lib/http-client'
import { queryKeys } from '@/lib/query-keys'
import { ProjectCard } from '@/components/ProjectCard'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { AnalyzedImageField } from '@/components/image-analysis/AnalyzedImageField'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { projectSchema, type ProjectFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { parseCreationIntent, projectCreatedReturnHref } from '@/lib/command-center'
import { referenceSelectionFromValue } from '@/lib/reference-image'
import { ProjectChannelConfigDialog } from '@/components/projects/ProjectChannelConfigDialog'
import { useAuth } from '@/contexts/AuthContext'
import { MetricStrip, WorkspaceSubnav } from '@/components/workspace'

const adminOnlyPlatforms = new Set<ProjectPlatform>(['moments', 'ecommerce', 'hypit'])
const profilePlatforms = [
  { id: 'xiaohongshu', label: '小红书' },
  { id: 'douyin', label: '抖音' },
  { id: 'bilibili', label: 'B站' },
  { id: 'video_account', label: '视频号' },
  { id: 'wechat_official_account', label: '公众号' },
  { id: 'weibo', label: '微博' },
  { id: 'zhihu', label: '知乎' },
] as const

function emptyProfileAnswers(projectName = ''): ProfileAnalysisAnswers {
  return {
    basic: { project_name: projectName, account_status: '' },
    platform_accounts: [],
    intent: { goals: '', direction: '', differentiation: '' },
    content: { preferences: '', formats: '', tone: '', audience: '' },
    boundaries: { exclusions: '', collaboration: '', compliance: '' },
  }
}

function canViewPlatform(platform: ProjectPlatform | '', isAdmin: boolean) {
  return isAdmin || platform === '' || !adminOnlyPlatforms.has(platform)
}

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '活跃', value: 'active' },
  { label: '已归档', value: 'archived' },
]

const PROJECT_FORM_DEFAULTS: ProjectFormValues = {
  name: '',
  keywords: '',
  instructions: '',
  visual_style: '',
  reference_image: null,
  ecommerce_default_selected_modules: {},
}

function projectToForm(project: Project): ProjectFormValues {
  return {
    ...PROJECT_FORM_DEFAULTS,
    name: project.name || '',
    keywords: project.keywords || '',
    instructions: project.instructions || project.positioning || '',
    visual_style: project.visual_style || '',
    reference_image: project.reference_image ?? null,
  }
}

export default function ProjectsPage() {
  const { user } = useAuth()
  const isAdmin = user?.is_admin === true
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const createIntent = parseCreationIntent(searchParams)
  const returnTo = searchParams.get('return_to')
  const [statusFilter, setStatusFilter] = useState('all')
  const [searchFilter, setSearchFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const formSessionRef = useRef(0)
  const createdProjectReturnHrefRef = useRef<string | null>(null)
  const pendingProfileGuideRef = useRef<Project | null>(null)
  const profileGuideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => () => {
    formSessionRef.current += 1
    if (profileGuideTimerRef.current) clearTimeout(profileGuideTimerRef.current)
  }, [])
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [editingAnalysis, setEditingAnalysis] = useState<ImageAnalysis | null>(null)
  const editingAnalysisUpdatedAtRef = useRef('')
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [channelProject, setChannelProject] = useState<Project | null>(null)
  const [profileProject, setProfileProject] = useState<Project | null>(null)
  const [profileDraft, setProfileDraft] = useState<ProjectProfile | null>(null)
  const [profileDraftText, setProfileDraftText] = useState<Record<string, string>>({})
  const [profileJsonErrors, setProfileJsonErrors] = useState<Record<string, string>>({})
  const [profileModalOpen, setProfileModalOpen] = useState(false)
  const [profileWizardMode, setProfileWizardMode] = useState(false)
  const [profileWizardStep, setProfileWizardStep] = useState(0)
  const [profileGuideAnalysisTaskID, setProfileGuideAnalysisTaskID] = useState<string | null>(null)
  const [profileGuideIsRefresh, setProfileGuideIsRefresh] = useState(false)
  const [profileAnswers, setProfileAnswers] = useState<ProfileAnalysisAnswers>(() => emptyProfileAnswers())
  const { submit } = useSubmitLock()

  const form = useForm<ProjectFormValues>({
    resolver: zodResolver(projectSchema) as Resolver<ProjectFormValues>,
    defaultValues: PROJECT_FORM_DEFAULTS,
  })

  const referenceImage = useWatch({ control: form.control, name: 'reference_image' })

  // Warn before closing with unsaved changes
  useFormDirtyCheck(form, modalOpen)

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

  const projectOverview = useMemo(() => {
    const stats = visibleProjects.map((project) => projectStats[project.id]).filter(Boolean)
    const tasks = stats.reduce((sum, item) => sum + item.total_tasks, 0)
    const completed = stats.reduce((sum, item) => sum + item.completed_tasks, 0)
    const running = stats.reduce((sum, item) => sum + item.running_tasks, 0)
    return {
      projects: visibleProjects.length,
      tasks,
      completed,
      running,
      completion: tasks > 0 ? `${Math.round((completed / tasks) * 100)}%` : '—',
    }
  }, [projectStats, visibleProjects])

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
    mutationFn: async ({ data, sessionID, returnContext }: { data: CreateProjectRequest; sessionID: number; returnContext: Omit<Parameters<typeof projectCreatedReturnHref>[0], 'projectId'> | null }) => {
      const created = await api.projects.create(data)
      if (sessionID === formSessionRef.current) {
        createdProjectReturnHrefRef.current = returnContext ? projectCreatedReturnHref({ ...returnContext, projectId: created.project.id }) : null
      }
      return created
    },
    onSuccess: (created, { sessionID }) => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
      if (sessionID !== formSessionRef.current) return
      toast.success(created.project.image_analysis ? '项目已创建，正在后台识别视觉风格' : '项目创建成功')
      pendingProfileGuideRef.current = created.project
      resetModal(true)
      profileGuideTimerRef.current = setTimeout(() => {
        profileGuideTimerRef.current = null
        const project = pendingProfileGuideRef.current
        pendingProfileGuideRef.current = null
        if (project) beginProfileGuide(project)
      }, 120)
    },
    onError: (err, { sessionID }) => {
      if (sessionID === formSessionRef.current) toast.error(getApiErrorMessage(err, '创建项目失败，请重试'))
    },
  })

  const profileQuery = useQuery({
    queryKey: ['project-profile', profileProject?.id],
    queryFn: () => api.projects.getAccountProfile(profileProject!.id),
    enabled: profileModalOpen && Boolean(profileProject),
  })
  useEffect(() => {
    if (!profileQuery.data) return
    setProfileDraft(profileQuery.data)
    setProfileDraftText(Object.fromEntries(Object.entries(profileQuery.data.dimensions).map(([key, dimension]) => [key, JSON.stringify(dimension.content, null, 2)])))
    setProfileJsonErrors({})
  }, [profileQuery.data])
  const profileAnalysisQuery = useQuery({
    queryKey: ['project-profile-analysis', profileProject?.id, profileDraft?.analysis_task_id],
    queryFn: () => api.projects.profileAnalysis(profileProject!.id),
    enabled: profileModalOpen && Boolean(profileProject?.id && profileDraft?.analysis_task_id),
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'pending' || status === 'running' ? 2000 : false
    },
  })
  useEffect(() => {
    const result = profileAnalysisQuery.data
    if (!result) return
    setProfileDraft(result.profile)
    setProfileDraftText(Object.fromEntries(Object.entries(result.profile.dimensions).map(([key, dimension]) => [key, JSON.stringify(dimension.content, null, 2)])))
    setProfileJsonErrors({})
  }, [profileAnalysisQuery.data])
  const profileAnalysisMutation = useMutation({
    mutationFn: ({ retry, answers }: { retry: boolean; answers?: ProfileAnalysisAnswers }) => {
      const version = profileDraft?.version ?? profileQuery.data?.version ?? 0
      return retry
        ? api.projects.retryProfile(profileProject!.id, version, answers)
        : api.projects.refreshProfile(profileProject!.id, version, answers)
    },
    onSuccess: (result) => {
      setProfileDraft(result.profile)
      setProfileGuideAnalysisTaskID(result.task.id)
      queryClient.setQueryData(['project-profile', profileProject?.id], result.profile)
      queryClient.invalidateQueries({ queryKey: ['project-profile-analysis', profileProject?.id] })
      toast.success('画像任务已加入队列')
    },
    onError: (err) => {
      if (getApiErrorMessage(err, '').includes('profile_revision_conflict')) {
        toast.error('画像版本已变化，请刷新后再试')
        queryClient.invalidateQueries({ queryKey: ['project-profile', profileProject?.id] })
      } else toast.error(getApiErrorMessage(err, '画像更新失败'))
    },
  })

  function resetProfileFlow() {
    setProfileDraft(null)
    setProfileDraftText({})
    setProfileJsonErrors({})
    setProfileWizardStep(0)
    setProfileGuideAnalysisTaskID(null)
    setProfileGuideIsRefresh(false)
  }

  function openProfile(project: Project) {
    resetProfileFlow()
    setProfileWizardMode(false)
    setProfileAnswers(emptyProfileAnswers(project.name))
    setProfileProject(project)
    setProfileModalOpen(true)
    void queryClient.invalidateQueries({ queryKey: ['project-profile', project.id] })
  }

  function closeProfile() {
    const returnHref = createdProjectReturnHrefRef.current
    createdProjectReturnHrefRef.current = null
    setProfileModalOpen(false)
    setProfileWizardMode(false)
    setProfileProject(null)
    resetProfileFlow()
    if (returnHref) navigate(returnHref)
  }

  function beginProfileGuide(project: Project, isRefresh = false) {
    resetProfileFlow()
    setProfileWizardMode(true)
    setProfileGuideIsRefresh(isRefresh)
    setProfileAnswers(emptyProfileAnswers(project.name))
    setProfileProject(project)
    setProfileModalOpen(true)
    void queryClient.invalidateQueries({ queryKey: ['project-profile', project.id] })
  }

  function toggleProfilePlatform(platform: string, checked: boolean) {
    setProfileAnswers((current) => {
      const existing = current.platform_accounts.find((item) => item.platform === platform)
      if (checked && !existing) return { ...current, platform_accounts: [...current.platform_accounts, { platform, account_name: '', profile_url: '' }] }
      if (!checked) return { ...current, platform_accounts: current.platform_accounts.filter((item) => item.platform !== platform) }
      return current
    })
  }

  function updatePlatformAccount(platform: string, field: 'account_name' | 'profile_url', value: string) {
    setProfileAnswers((current) => ({
      ...current,
      platform_accounts: current.platform_accounts.map((item) => item.platform === platform ? { ...item, [field]: value } : item),
    }))
  }

  function startProfileAnalysis(retry = false) {
    profileAnalysisMutation.mutate({ retry, answers: profileAnswers })
    setProfileWizardStep(4)
  }

  const profileDimensionMutation = useMutation({
    mutationFn: ({ name, dimension }: { name: string; dimension: ProjectProfile['dimensions'][keyof ProjectProfile['dimensions']] }) => api.projects.updateProfileDimension(profileProject!.id, name, profileDraft!.version, dimension),
    onSuccess: (profile) => {
      setProfileDraft(profile)
      queryClient.setQueryData(['project-profile', profileProject?.id], profile)
      queryClient.invalidateQueries({ queryKey: ['project-profile-analysis', profileProject?.id] })
      toast.success('画像文件已保存')
    },
    onError: (err) => {
      if (getApiErrorMessage(err, '').includes('profile_revision_conflict')) {
        toast.error('画像版本冲突，正在重新加载当前内容')
        queryClient.invalidateQueries({ queryKey: ['project-profile', profileProject?.id] })
      } else toast.error(getApiErrorMessage(err, '画像保存失败'))
    },
  })

  const profileConfirmMutation = useMutation({
    mutationFn: () => api.projects.confirmAccountProfile(profileProject!.id, profileDraft!),
    onSuccess: (profile) => {
      setProfileDraft(profile)
      queryClient.setQueryData(['project-profile', profileProject?.id], profile)
      queryClient.invalidateQueries({ queryKey: ['project-profile-analysis', profileProject?.id] })
      toast.success('项目画像已确认，可用于后续创作')
    },
    onError: (error) => toast.error(getApiErrorMessage(error, '画像确认失败，请重试')),
  })

  const updateMutation = useMutation({
    mutationFn: async ({ id, data }: { id: string; data: Partial<CreateProjectRequest>; sessionID: number }) => {
      const updated = await api.projects.update(id, data)
      return updated
    },
    onSuccess: (_, { sessionID }) => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
      if (sessionID !== formSessionRef.current) return
      toast.success('项目更新成功')
      finishProjectSave()
    },
    onError: (err, { sessionID }) => {
      if (sessionID === formSessionRef.current) toast.error(getApiErrorMessage(err, '更新项目失败，请重试'))
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
    formSessionRef.current += 1
    createdProjectReturnHrefRef.current = null
    setEditingProject(null)
    form.reset(PROJECT_FORM_DEFAULTS)
    setEditingAnalysis(null)
    editingAnalysisUpdatedAtRef.current = ''
    setReferenceUploading(false)
    setModalOpen(true)
  }

  useEffect(() => {
    if (!createIntent.shouldCreate || modalOpen || pendingProfileGuideRef.current) return
    openCreate()
  }, [createIntent.shouldCreate, modalOpen])

  function openEdit(project: Project) {
    formSessionRef.current += 1
    createdProjectReturnHrefRef.current = null
    setEditingProject(project)
    setEditingAnalysis(project.image_analysis ?? null)
    editingAnalysisUpdatedAtRef.current = project.image_analysis?.updated_at ?? ''
    setReferenceUploading(false)
    form.reset(projectToForm(project))
    setModalOpen(true)
  }

  function finishProjectSave() {
    const returnHref = createdProjectReturnHrefRef.current
    resetModal()
    if (returnHref) navigate(returnHref)
  }

  function closeModal() {
    if (form.formState.isDirty) {
      setShowDirtyDialog(true)
      return
    }
    resetModal()
  }

  function resetModal(preserveReturnHref = false) {
    formSessionRef.current += 1
    if (!preserveReturnHref) createdProjectReturnHrefRef.current = null
    setModalOpen(false)
    setShowDirtyDialog(false)
    setEditingProject(null)
    setEditingAnalysis(null)
    editingAnalysisUpdatedAtRef.current = ''
    setReferenceUploading(false)
    form.reset(PROJECT_FORM_DEFAULTS)
    if (createIntent.shouldCreate) {
      setSearchParams({}, { replace: true })
    } else if (searchParams.has('edit')) {
      const next = new URLSearchParams(searchParams)
      next.delete('edit')
      setSearchParams(next, { replace: true })
    }
  }

  async function onSubmit(values: ProjectFormValues) {
    const payload: CreateProjectRequest = {
      name: values.name?.trim() || undefined,
      keywords: values.keywords?.trim() || '',
      instructions: values.instructions?.trim() || '',
    }
    if (!editingProject || form.formState.dirtyFields.visual_style) {
      payload.visual_style = values.visual_style?.trim() ?? ''
    }
    const referenceImage = referenceSelectionFromValue(values.reference_image)
    if (editingProject) {
      const updatePayload = {
        ...payload,
        ...(form.formState.dirtyFields.reference_image ? { reference_image: referenceImage } : {}),
      }
      await submit(async () => updateMutation.mutateAsync({ id: editingProject.id, data: updatePayload, sessionID: formSessionRef.current })).catch(() => {})
    } else {
      const createPayload = {
        ...payload,
        ...(referenceImage ? { reference_image: referenceImage } : {}),
      }
      await submit(async () => createMutation.mutateAsync({ data: createPayload, sessionID: formSessionRef.current, returnContext: returnTo ? { returnTo, type: createIntent.type, intent: createIntent.intent } : null })).catch(() => {})
    }
  }

  function handleProjectSubmit(event?: React.BaseSyntheticEvent) {
    if (referenceUploading) {
      event?.preventDefault()
      return
    }
    return form.handleSubmit(onSubmit)(event)
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending
  const profileDimensionLabels: Record<string, string> = { identity: '定位', style: '风格', audience: '受众', platforms: '平台', preferences: '偏好与红线', memory: '记忆' }
  const profileInitializationStatus = profileDraft?.initialization_status
  const profileAnalysisStatus: ProfileAnalysisStatus = profileAnalysisQuery.data?.status
    ?? (profileInitializationStatus === 'queued' ? 'pending' : profileInitializationStatus === 'ready' ? 'completed' : profileInitializationStatus ?? 'not_started')
  const profileAnalysisRunning = profileAnalysisStatus === 'queued' || profileAnalysisStatus === 'pending' || profileAnalysisStatus === 'running' || profileAnalysisMutation.isPending
  const profileAnalysisFailed = profileAnalysisStatus === 'failed' || profileAnalysisStatus === 'cancelled' || profileAnalysisMutation.isError
  const profileResultReady = profileDraft?.status === 'confirmed'
    || profileDraft?.initialization_status === 'ready'
    || profileAnalysisStatus === 'completed'
  const profileNeedsConfirmation = profileResultReady && profileDraft?.status === 'draft'
  const profileGuideRefreshApplied = profileWizardMode
    && profileGuideIsRefresh
    && profileGuideAnalysisTaskID !== null
    && profileDraft?.analysis_task_id === profileGuideAnalysisTaskID
    && profileDraft?.status === 'confirmed'
    && profileDraft?.initialization_status === 'ready'
    && profileAnalysisQuery.data?.status === 'completed'

  return (
    <div className="space-y-6">
      <PageHeader title="项目" description="管理品牌与创作方向、素材和风格，可用于不同类型的任务。">
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建项目
        </Button>
      </PageHeader>

      <WorkspaceSubnav
        items={[
          { label: '项目', href: '/projects' },
          { label: '计划', href: '/plans' },
          { label: '任务', href: '/tasks' },
          { label: '时间线', href: '/timeline' },
        ]}
      />

      <MetricStrip
        label="项目概览"
        metrics={[
          { label: '项目', value: projectOverview.projects },
          { label: '任务', value: projectOverview.tasks, detail: projectOverview.running ? `${projectOverview.running} 个运行中` : undefined },
          { label: '完成率', value: projectOverview.completion, detail: projectOverview.completed ? `${projectOverview.completed} 个已完成` : '暂无任务' },
        ]}
      />

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
          action={!visibleProjects.length && statusFilter === 'all' && !searchFilter ? { label: '新建项目', onClick: openCreate } : { label: '清空筛选', onClick: () => { setSearchFilter(''); setStatusFilter('all') } }}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filteredProjects.map((project) => (
            <ProjectCard
              key={project.id}
              project={project}
              stats={projectStats[project.id]}
              onEdit={openEdit}
              onProfile={openProfile}
              onChannelConfig={setChannelProject}
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
              <FormField control={form.control} name="name" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">项目名称</FormLabel>
                  <FormControl>
                    <Input className="flex-1 min-w-0" placeholder="例如 我的科技博客" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )} />

              <FormField control={form.control} name="instructions" render={({ field }) => (
                <FormItem>
                  <div className="flex items-center gap-1">
                    <FormLabel className="w-auto">项目定位</FormLabel>
                    <FieldHint>每次创作都会参考这些要求，保持项目定位与表达风格一致。</FieldHint>
                  </div>
                  <FormControl>
                    <Textarea
                      placeholder="例如 面向开发者的实用 AI 教程"
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>写清受众、领域和语气；定位会作为长期约束注入每一次创作，任务里可再补充单次要求。</FormDescription>
                  <FormMessage />
                </FormItem>
              )} />

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
                      placeholder="可手动描述封面与配图风格；留空时后台识别"
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

              <div className="flex items-center gap-3 rounded-md border border-border p-3">
                {editingProject?.portrait_reference_image ? <img src={editingProject.portrait_reference_image.download_url} alt="项目默认人物参考" className="h-12 w-12 shrink-0 rounded-md border object-cover" /> : null}
                <div className="min-w-0">
                  <p className="text-sm font-medium text-foreground">人物参考</p>
                  <p className="mt-0.5 text-xs text-muted-foreground">人物参考图保留为任务和计划的默认值，可在任务或计划中单独更换。</p>
                  {editingProject?.portrait_reference_image ? <p className="mt-1 truncate text-xs text-muted-foreground">当前默认：{editingProject.portrait_reference_image.file_name}</p> : null}
                </div>
              </div>
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

      <Dialog open={profileModalOpen} onOpenChange={(open) => { if (!open) closeProfile() }}>
        <DialogContent className="flex max-h-[90dvh] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
          <DialogHeader className="shrink-0 border-b border-border px-4 py-3">
            <DialogTitle>{profileWizardMode ? '项目画像引导' : '项目画像'}</DialogTitle>
          </DialogHeader>
          {profileWizardMode && profileWizardStep < 4 ? (
            <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-4 py-4">
              <div className="flex items-center justify-between gap-3">
                <h3 className="text-sm font-semibold">{['基础信息', '社媒链接', '运营意图', '偏好与红线'][profileWizardStep]}</h3>
                <span className="text-xs text-muted-foreground">第 {profileWizardStep + 1} / 4 步</span>
              </div>
              {profileWizardStep === 0 ? (
                <div className="space-y-5">
                  <label className="block space-y-1.5 text-sm font-medium">项目名称
                    <Input aria-label="画像项目名称" value={profileAnswers.basic.project_name} onChange={(event) => setProfileAnswers((current) => ({ ...current, basic: { ...current.basic, project_name: event.target.value } }))} />
                  </label>
                  <fieldset className="space-y-2">
                    <legend className="text-sm font-medium">运营状态</legend>
                    <div className="flex flex-wrap gap-2">
                      {([{ value: '', label: '暂不确定' }, { value: 'new', label: '全新起号' }, { value: 'existing', label: '已有账号' }] as const).map((option) => (
                        <label key={option.value} className="flex min-h-10 cursor-pointer items-center gap-2 rounded-md border border-border px-3 text-sm">
                          <input aria-label={option.label} type="radio" name="profile-account-status" value={option.value} checked={profileAnswers.basic.account_status === option.value} onChange={() => setProfileAnswers((current) => ({ ...current, basic: { ...current.basic, account_status: option.value } }))} />
                          {option.label}
                        </label>
                      ))}
                    </div>
                  </fieldset>
                  <fieldset className="space-y-2">
                    <legend className="text-sm font-medium">经营平台</legend>
                    <div className="grid gap-2 sm:grid-cols-2">
                      {profilePlatforms.map((platform) => (
                        <label key={platform.id} className="flex min-h-10 cursor-pointer items-center gap-2 rounded-md border border-border px-3 text-sm">
                          <input type="checkbox" name="profile-platform" value={platform.id} checked={profileAnswers.platform_accounts.some((item) => item.platform === platform.id)} onChange={(event) => toggleProfilePlatform(platform.id, event.target.checked)} />
                          {platform.label}
                        </label>
                      ))}
                    </div>
                  </fieldset>
                </div>
              ) : null}
              {profileWizardStep === 1 ? (
                <div className="space-y-4">
                  {profileAnswers.platform_accounts.length === 0 ? <p className="text-sm text-muted-foreground">未选择平台。可返回基础信息选择，也可以留空继续。</p> : profileAnswers.platform_accounts.map((account) => {
                    const platform = profilePlatforms.find((item) => item.id === account.platform)
                    const label = platform?.label ?? account.platform
                    return <section key={account.platform} className="grid gap-3 rounded-md border border-border p-3 sm:grid-cols-2">
                      <h4 className="text-sm font-medium sm:col-span-2">{label}</h4>
                      <label className="space-y-1.5 text-sm">账号名
                        <Input aria-label={`${label}账号名`} value={account.account_name} onChange={(event) => updatePlatformAccount(account.platform, 'account_name', event.target.value)} />
                      </label>
                      <label className="space-y-1.5 text-sm">主页链接
                        <Input aria-label={`${label}主页链接`} type="url" value={account.profile_url} onChange={(event) => updatePlatformAccount(account.platform, 'profile_url', event.target.value)} />
                      </label>
                    </section>
                  })}
                </div>
              ) : null}
              {profileWizardStep === 2 ? (
                <div className="grid gap-4">
                  <label className="space-y-1.5 text-sm">运营目标<Textarea aria-label="运营目标" value={profileAnswers.intent.goals} onChange={(event) => setProfileAnswers((current) => ({ ...current, intent: { ...current.intent, goals: event.target.value } }))} /></label>
                  <label className="space-y-1.5 text-sm">内容方向<Textarea aria-label="内容方向" value={profileAnswers.intent.direction} onChange={(event) => setProfileAnswers((current) => ({ ...current, intent: { ...current.intent, direction: event.target.value } }))} /></label>
                  <label className="space-y-1.5 text-sm">差异化优势<Textarea aria-label="差异化优势" value={profileAnswers.intent.differentiation} onChange={(event) => setProfileAnswers((current) => ({ ...current, intent: { ...current.intent, differentiation: event.target.value } }))} /></label>
                </div>
              ) : null}
              {profileWizardStep === 3 ? (
                <div className="grid gap-4 sm:grid-cols-2">
                  <label className="space-y-1.5 text-sm">内容偏好<Textarea aria-label="内容偏好" value={profileAnswers.content.preferences} onChange={(event) => setProfileAnswers((current) => ({ ...current, content: { ...current.content, preferences: event.target.value } }))} /></label>
                  <label className="space-y-1.5 text-sm">内容形式<Textarea aria-label="内容形式" value={profileAnswers.content.formats} onChange={(event) => setProfileAnswers((current) => ({ ...current, content: { ...current.content, formats: event.target.value } }))} /></label>
                  <label className="space-y-1.5 text-sm">表达语气<Textarea aria-label="表达语气" value={profileAnswers.content.tone} onChange={(event) => setProfileAnswers((current) => ({ ...current, content: { ...current.content, tone: event.target.value } }))} /></label>
                  <label className="space-y-1.5 text-sm">目标受众<Textarea aria-label="目标受众" value={profileAnswers.content.audience} onChange={(event) => setProfileAnswers((current) => ({ ...current, content: { ...current.content, audience: event.target.value } }))} /></label>
                  <label className="space-y-1.5 text-sm">不做什么<Textarea aria-label="不做什么" value={profileAnswers.boundaries.exclusions} onChange={(event) => setProfileAnswers((current) => ({ ...current, boundaries: { ...current.boundaries, exclusions: event.target.value } }))} /></label>
                  <label className="space-y-1.5 text-sm">合作边界<Textarea aria-label="合作边界" value={profileAnswers.boundaries.collaboration} onChange={(event) => setProfileAnswers((current) => ({ ...current, boundaries: { ...current.boundaries, collaboration: event.target.value } }))} /></label>
                  <label className="space-y-1.5 text-sm sm:col-span-2">合规红线<Textarea aria-label="合规红线" value={profileAnswers.boundaries.compliance} onChange={(event) => setProfileAnswers((current) => ({ ...current, boundaries: { ...current.boundaries, compliance: event.target.value } }))} /></label>
                  <p className="text-xs text-muted-foreground sm:col-span-2">信息可以留空，画像会根据已有内容生成。</p>
                </div>
              ) : null}
            </div>
          ) : (
            <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 py-4">
              {!profileWizardMode && profileQuery.isPending ? <p className="text-sm text-muted-foreground" role="status">正在加载项目画像…</p> : null}
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-muted/20 p-3">
                <div>
                  <p className="text-sm font-medium">初始化状态：{profileDraft?.initialization_status ?? 'not_started'}</p>
                  {profileDraft?.last_error && <p className="text-xs text-destructive" role="alert">{profileDraft.last_error}</p>}
                </div>
                {!profileWizardMode && profileDraft?.status === 'confirmed' ? <Button variant="outline" size="sm" loading={profileAnalysisMutation.isPending} disabled={profileAnalysisRunning} onClick={() => profileAnalysisMutation.mutate({ retry: profileAnalysisFailed })}>更新画像</Button> : null}
              </div>
              {profileAnalysisRunning && <p className="text-sm text-blue-600" role="status">画像任务已加入队列，完成后会显示可确认的结果。</p>}
              {profileAnalysisFailed && <p className="text-sm text-destructive" role="alert">画像任务失败，已确认的画像保持不变，可以重试。</p>}
              {!profileWizardMode && !profileDraft?.analysis_task_id && profileDraft?.status !== 'confirmed' ? <p className="text-sm text-muted-foreground">尚未初始化项目画像。可以稍后从这里开始，也可以直接创建任务或计划。</p> : null}
              {profileWizardMode && profileAnalysisFailed ? <Button variant="outline" disabled={profileAnalysisMutation.isPending} loading={profileAnalysisMutation.isPending} onClick={() => startProfileAnalysis(true)}>重试画像分析</Button> : null}
              {profileResultReady && profileDraft ? (
                <>
                  <table className="w-full text-left text-xs"><thead><tr className="border-b"><th className="py-2 pr-3">画像维度</th><th className="py-2">用途</th></tr></thead><tbody>
                    {Object.entries(profileDimensionLabels).map(([key, label]) => <tr key={key} className="border-b"><td className="py-2 pr-3 font-mono">profile/{key}.md</td><td className="py-2">{label}</td></tr>)}
                  </tbody></table>
                  <div className="space-y-3">
                    {Object.entries(profileDraft.dimensions).map(([key, dimension]) => (
                      <div key={key} className="space-y-2 rounded-md border p-3">
                        <div className="mb-2 flex items-center justify-between gap-2">
                          <span className="text-sm font-medium">{profileDimensionLabels[key] || key}</span>
                          <span className="text-xs text-muted-foreground">{dimension.sources.join('、') || '[待补充]'}</span>
                        </div>
                        <Textarea
                          aria-label={`${profileDimensionLabels[key] || key}画像内容`}
                          value={profileDraftText[key] ?? JSON.stringify(dimension.content, null, 2)}
                          onChange={(event) => {
                            const text = event.target.value
                            setProfileDraftText((current) => ({ ...current, [key]: text }))
                            try {
                              const content = JSON.parse(text) as Record<string, unknown>
                              if (!content || Array.isArray(content) || typeof content !== 'object') throw new Error('object required')
                              setProfileJsonErrors((current) => {
                                const next = { ...current }
                                delete next[key]
                                return next
                              })
                              setProfileDraft((current) => current ? { ...current, dimensions: { ...current.dimensions, [key]: { ...dimension, content } } } : current)
                            } catch { setProfileJsonErrors((current) => ({ ...current, [key]: `${profileDimensionLabels[key] || key} 的内容必须是有效 JSON 对象` })) }
                          }}
                          className="min-h-[76px] font-mono text-xs"
                        />
                        {profileJsonErrors[key] ? <p className="text-sm text-destructive" role="alert">{profileJsonErrors[key]}</p> : null}
                        {profileDraft.status === 'confirmed' ? <Button size="sm" variant="secondary" disabled={Boolean(profileJsonErrors[key]) || profileDimensionMutation.isPending || profileAnalysisRunning} onClick={() => profileDimensionMutation.mutate({ name: key, dimension })}>保存此维度</Button> : null}
                        {dimension.missing_fields.length > 0 && <p className="mt-1 text-xs text-amber-600">待补充：{dimension.missing_fields.join('、')}</p>}
                      </div>
                    ))}
                    {profileDraft.follow_up_questions.length > 0 && <div className="text-sm text-muted-foreground">待确认问题：{profileDraft.follow_up_questions.join('；')}</div>}
                    {profileNeedsConfirmation ? <p className="text-sm text-amber-700">这是画像草稿，确认后才会用于后续任务。</p> : null}
                    {profileGuideRefreshApplied ? <p className="text-sm text-green-700" role="status">画像已更新并自动应用。</p> : null}
                  </div>
                </>
              ) : null}
              {!profileWizardMode && profileDraft?.status === 'confirmed' ? <Button variant="outline" onClick={() => beginProfileGuide(profileProject!, true)}>编辑引导并更新画像</Button> : null}
            </div>
          )}
          <DialogFooter className="shrink-0 border-t border-border px-4 py-3">
            {profileWizardMode && profileWizardStep < 4 ? <>
              <Button variant="outline" onClick={closeProfile}>暂时跳过</Button>
              {profileWizardStep > 0 ? <Button variant="secondary" onClick={() => setProfileWizardStep((step) => step - 1)}>上一步</Button> : null}
              {profileWizardStep < 3 ? <Button onClick={() => setProfileWizardStep((step) => step + 1)}>下一步</Button> : <Button loading={profileAnalysisMutation.isPending} disabled={profileAnalysisMutation.isPending || profileQuery.isPending} onClick={() => startProfileAnalysis()}>开始生成画像</Button>}
            </> : <>
              <Button variant="secondary" onClick={() => closeProfile()}>关闭</Button>
              {profileNeedsConfirmation ? <Button loading={profileConfirmMutation.isPending} disabled={Object.keys(profileJsonErrors).length > 0 || profileConfirmMutation.isPending} onClick={() => profileConfirmMutation.mutate()}>确认并应用画像</Button> : null}
              {!profileWizardMode && (!profileDraft || profileDraft.status !== 'confirmed') ? <Button onClick={() => { setProfileWizardMode(true); setProfileWizardStep(0) }}>开始画像引导</Button> : null}
            </>}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {channelProject && (
        <ProjectChannelConfigDialog
          key={channelProject.id}
          project={channelProject}
          onClose={() => setChannelProject(null)}
        />
      )}

      {/* Dirty form confirmation */}
      <AlertDialog open={showDirtyDialog} onOpenChange={setShowDirtyDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃编辑？</AlertDialogTitle>
            <AlertDialogDescription>你有未保存的更改，确定要关闭吗？</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction onClick={() => resetModal()}>放弃</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

    </div>
  )
}
