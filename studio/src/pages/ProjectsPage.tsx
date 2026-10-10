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
import type { ImageAnalysis, Project, ProjectPlatform, ProjectStats, CreateProjectRequest } from '@/types'
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
import { ReferenceAssetUpload } from '@/components/projects/ReferenceAssetUpload'
import { AnalyzedImageField } from '@/components/image-analysis/AnalyzedImageField'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { projectSchema, type ProjectFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { parseCreationIntent } from '@/lib/command-center'
import { referenceSelectionFromValue } from '@/lib/reference-image'
import { ProjectChannelConfigDialog } from '@/components/projects/ProjectChannelConfigDialog'
import { useAuth } from '@/contexts/AuthContext'
import { MetricStrip } from '@/components/workspace'

const adminOnlyPlatforms = new Set<ProjectPlatform>(['moments', 'ecommerce'])

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
  portrait_reference_image: null,
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
    portrait_reference_image: project.portrait_reference_image ?? null,
  }
}

export default function ProjectsPage() {
  const { user } = useAuth()
  const isAdmin = user?.is_admin === true
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const createIntent = parseCreationIntent(searchParams)
  const [statusFilter, setStatusFilter] = useState('all')
  const [searchFilter, setSearchFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const formSessionRef = useRef(0)
  useEffect(() => () => { formSessionRef.current += 1 }, [])
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [editingAnalysis, setEditingAnalysis] = useState<ImageAnalysis | null>(null)
  const editingAnalysisUpdatedAtRef = useRef('')
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [channelProject, setChannelProject] = useState<Project | null>(null)
  const { submit } = useSubmitLock()

  const form = useForm<ProjectFormValues>({
    resolver: zodResolver(projectSchema) as Resolver<ProjectFormValues>,
    defaultValues: PROJECT_FORM_DEFAULTS,
  })

  const referenceImage = useWatch({ control: form.control, name: 'reference_image' })
  const portraitReferenceImage = useWatch({ control: form.control, name: 'portrait_reference_image' })

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
    const query = searchParams.toString()
    navigate(`/projects/new${query ? `?${query}` : ''}`)
  }

  useEffect(() => {
    if (!createIntent.shouldCreate || modalOpen) return
    openCreate()
  }, [createIntent.shouldCreate, modalOpen])

  function openEdit(project: Project) {
    formSessionRef.current += 1
    setEditingProject(project)
    setEditingAnalysis(project.image_analysis ?? null)
    editingAnalysisUpdatedAtRef.current = project.image_analysis?.updated_at ?? ''
    setReferenceUploading(false)
    form.reset(projectToForm(project))
    setModalOpen(true)
  }

  function finishProjectSave() {
    resetModal()
  }

  function closeModal() {
    if (form.formState.isDirty) {
      setShowDirtyDialog(true)
      return
    }
    resetModal()
  }

  function resetModal() {
    formSessionRef.current += 1
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
    if (form.formState.dirtyFields.visual_style) {
      payload.visual_style = values.visual_style?.trim() ?? ''
    }
    const referenceImage = referenceSelectionFromValue(values.reference_image)
    const portraitReferenceImage = referenceSelectionFromValue(values.portrait_reference_image)
    if (!editingProject) return
    const updatePayload = {
      ...payload,
      ...(form.formState.dirtyFields.reference_image ? { reference_image: referenceImage } : {}),
      ...(form.formState.dirtyFields.portrait_reference_image ? { portrait_reference_image: portraitReferenceImage } : {}),
    }
    await submit(async () => updateMutation.mutateAsync({ id: editingProject.id, data: updatePayload, sessionID: formSessionRef.current })).catch(() => {})
  }

  function handleProjectSubmit(event?: React.BaseSyntheticEvent) {
    if (referenceUploading) {
      event?.preventDefault()
      return
    }
    return form.handleSubmit(onSubmit)(event)
  }

  const isSubmitting = updateMutation.isPending
  return (
    <div className="space-y-6">
      <PageHeader title="项目" description="管理品牌与创作方向、素材和风格，可用于不同类型的任务。">
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" />
          新建项目
        </Button>
      </PageHeader>

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
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-2 xl:grid-cols-3">
          {filteredProjects.map((project) => (
            <ProjectCard
              key={project.id}
              project={project}
              stats={projectStats[project.id]}
              onEdit={openEdit}
              onProfile={(selectedProject) => navigate(`/projects/${selectedProject.id}/profile-lab`)}
              onChannelConfig={setChannelProject}
              archiving={archiveMutation.isPending}
              restoring={restoreMutation.isPending}
              onArchive={(id) => { void submit(async () => archiveMutation.mutateAsync(id)).catch(() => {}) }}
              onRestore={(id) => { void submit(async () => restoreMutation.mutateAsync(id)).catch(() => {}) }}
            />
          ))}
        </div>
      )}

      {/* Basic settings retain existing asset editing. Profile settings live in Profile Lab. */}
      <Dialog open={modalOpen} onOpenChange={(v) => { if (!v) closeModal() }}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>编辑项目</DialogTitle>
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

              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-foreground">默认人物参考</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">用于任务和计划中勾选的人物封面；未勾选时生成普通封面。</p>
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
            </form>
          </Form>
          <DialogFooter>
            <Button variant="secondary" onClick={closeModal}>取消</Button>
            <Button type="submit" form="project-form" loading={isSubmitting} disabled={referenceUploading}>
              更新
            </Button>
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
            <AlertDialogAction onClick={resetModal}>放弃</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

    </div>
  )
}
