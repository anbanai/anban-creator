import { useState, useEffect, useMemo, useRef } from 'react'
import { useForm, useWatch, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Plus, Inbox, Loader2 } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/lib/api'
import type { Project, ProjectStats, CreateProjectRequest, PlatformConfig, Template } from '@/types'
import { getApiErrorMessage } from '@/lib/http-client'
import { ProjectCard } from '@/components/ProjectCard'
import { SearchInput } from '@/components/ui/SearchInput'
import { Button } from '@/components/common/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { TagInput } from '@/components/ui/TagInput'
import { ReferenceImageUpload } from '@/components/projects/ReferenceImageUpload'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { TemplatePicker } from '@/components/templates/TemplatePicker'
import { PersonaBlock } from '@/components/templates/PersonaBlock'
import { ThemePicker } from '@/components/templates/ThemePicker'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage, FormDescription } from '@/components/ui/form'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { projectSchema, type ProjectFormValues } from '@/lib/schemas'
import { useFormDirtyCheck } from '@/hooks/useFormDirtyCheck'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import PageHeader from '@/components/layout/PageHeader'
import EmptyState from '@/components/EmptyState'
import { renderPlatformIcon } from '@/lib/PlatformIcon'

const platformOptions = [
  { value: 'seednote', label: '种草笔记' },
  { value: 'article', label: '公众号' },
  { value: 'ecommerce', label: '电商出图' },
]

const statusTabs: { label: string; value: string }[] = [
  { label: '全部', value: 'all' },
  { label: '活跃', value: 'active' },
  { label: '已归档', value: 'archived' },
]

const CHANNEL_FORM_DEFAULTS: ProjectFormValues = {
  platform: 'article',
  name: '',
  profile_url: '',
  avatar_url: '',
  wechat_app_id: '',
  wechat_secret: '',
  keywords: '',
  positioning: '',
  style: '',
  writing_style: '',
  theme: '',
  author: '',
  author_style_intro: '',
  author_avatar_url: '',
  template_id: '',
  reference_image_url: '',
  image_ratio: '',
  enable_publishing: false,
  require_publish_approval: false,
}

function projectToForm(ch: Project): ProjectFormValues {
  return {
    platform: ch.platform,
    name: ch.name || '',
    profile_url: ch.profile_url || '',
    avatar_url: ch.avatar_url || '',
    wechat_app_id: ch.config?.wechat_app_id || '',
    wechat_secret: '',
    keywords: ch.keywords || '',
    positioning: ch.positioning || '',
    style: ch.style || '',
    writing_style: ch.writing_style || '',
    theme: ch.theme || '',
    author: ch.author || '',
    author_style_intro: ch.author_style_intro || '',
    author_avatar_url: ch.author_avatar_url || '',
    template_id: ch.template_id || '',
    reference_image_url: ch.reference_image_url || '',
    image_ratio: (ch.image_ratio as '' | '3:4' | '1:1' | '4:3' | '16:9') || '',
    enable_publishing: ch.config?.enable_publishing ?? false,
    require_publish_approval: ch.config?.require_publish_approval ?? false,
  }
}

export default function ProjectsPage() {
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState('all')
  const [searchFilter, setSearchFilter] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [fetchingProfile, setFetchingProfile] = useState(false)
  const [profileFetchHint, setProfileFetchHint] = useState<string | null>(null)
  const [showDirtyDialog, setShowDirtyDialog] = useState(false)
  const [analyzingStyle, setAnalyzingStyle] = useState(false)
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
  const profileUrl = useWatch({ control: form.control, name: 'profile_url' })
  const enablePublishing = useWatch({ control: form.control, name: 'enable_publishing' })
  const referenceImageUrl = useWatch({ control: form.control, name: 'reference_image_url' })
  const authorName = useWatch({ control: form.control, name: 'author' })
  const authorStyleIntro = useWatch({ control: form.control, name: 'author_style_intro' })
  const authorAvatarUrl = useWatch({ control: form.control, name: 'author_avatar_url' })
  const themeValue = useWatch({ control: form.control, name: 'theme' })

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

  // Auto-analyze reference image to fill visual style (seednote only)
  useEffect(() => {
    if (!modalOpen || !referenceImageUrl || selectedPlatform !== 'seednote') return
    if (styleManuallyEditedRef.current) return
    // Skip if URL matches the project's saved value — don't clobber existing style on edit
    if (editingProject && referenceImageUrl === editingProject.reference_image_url) return

    let cancelled = false
    const timer = setTimeout(async () => {
      setAnalyzingStyle(true)
      try {
        const result = await api.projects.analyzeImage(referenceImageUrl)
        if (!cancelled && result.style && !styleManuallyEditedRef.current) {
          form.setValue('style', result.style)
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
  }, [modalOpen, referenceImageUrl, selectedPlatform, editingProject, form])

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
      if (profile.positioning) form.setValue('positioning', profile.positioning)
      if (profile.keywords) form.setValue('keywords', profile.keywords)
      if (profile.style) form.setValue('style', profile.style)
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
    onSuccess: () => {
      toast.success('项目创建成功')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
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
    onError: () => {
      toast.error('更新项目失败，请重试')
    },
  })

  const archiveMutation = useMutation({
    mutationFn: (id: string) => api.projects.archive(id),
    onSuccess: () => {
      toast.success('项目已归档')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
    },
  })

  const restoreMutation = useMutation({
    mutationFn: (id: string) => api.projects.restore(id),
    onSuccess: () => {
      toast.success('项目已恢复')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-stats'] })
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
    onError: () => {
      toast.error('删除项目失败，请重试')
    },
  })

  function openCreate() {
    setEditingProject(null)
    setProfileFetchHint(null)
    form.reset(CHANNEL_FORM_DEFAULTS)
    setSelectedTemplate(null)
    setModalOpen(true)
  }

  function openEdit(project: Project) {
    setEditingProject(project)
    setProfileFetchHint(null)
    form.reset(projectToForm(project))
    skipAutoFetchRef.current = true
    setSelectedTemplate(null)
    setModalOpen(true)

    // 导入模型存量兼容：旧"绑定模板"项目的人设字段历史上为空（运行时由模板下发）。
    // 打开编辑时把模板人设作为默认值回填到当前为空的字段，让用户看到生效中的人设并可
    // 编辑。保存后 template_id 由后端清空（迁移为项目自有值）。shouldDirty:false 避免
    // 未改动时触发脏检查弹窗。模板已删则静默留空，用户可手动选其他模板。
    if (project.template_id) {
      const token = ++projectEditTokenRef.current
      api.templates
        .get(project.template_id)
        .then((t) => {
          // 对话框已切到别的项目（或重开）则丢弃这条陈旧回填，避免串改。
          if (projectEditTokenRef.current !== token) return
          setSelectedTemplate(t)
          if (!form.getValues('style')) form.setValue('style', t.style_prompt || '', { shouldDirty: false })
          if (!form.getValues('author')) form.setValue('author', t.author_name || '', { shouldDirty: false })
          if (!form.getValues('author_style_intro')) form.setValue('author_style_intro', t.author_style_intro || '', { shouldDirty: false })
          if (!form.getValues('author_avatar_url')) form.setValue('author_avatar_url', t.author_avatar_url || '', { shouldDirty: false })
          if (!form.getValues('theme')) form.setValue('theme', t.theme || '', { shouldDirty: false })
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
    form.reset(CHANNEL_FORM_DEFAULTS)
  }

  // 选模板=一次性把模板风格导入表单（视觉/作者/写作风格/排版，可编辑）。再次点击同一
  // 卡片为 no-op（保留用户对风格文本框的改动），切换到别的模板则覆盖。
  function handleProjectTemplateImport(template: Template) {
    if (selectedTemplate?.id === template.id) return
    setSelectedTemplate(template)
    form.setValue('style', template.style_prompt || '', { shouldDirty: true })
    form.setValue('author', template.author_name || '', { shouldDirty: true })
    form.setValue('author_style_intro', template.author_style_intro || '', { shouldDirty: true })
    form.setValue('author_avatar_url', template.author_avatar_url || '', { shouldDirty: true })
    form.setValue('theme', template.theme || '', { shouldDirty: true })
  }

  async function onSubmit(values: ProjectFormValues) {
    const payload: CreateProjectRequest = {
      platform: values.platform,
      name: values.name?.trim() || undefined,
      profile_url: values.profile_url?.trim() || undefined,
      avatar_url: values.avatar_url?.trim() || undefined,
      positioning: values.positioning?.trim() || undefined,
      keywords: values.keywords?.trim() || undefined,
      style: values.style?.trim() || undefined,
      writing_style: values.writing_style?.trim() || undefined,
      theme: values.theme?.trim() || undefined,
      author: values.author?.trim() || undefined,
      author_style_intro: values.author_style_intro?.trim() || undefined,
      author_avatar_url: values.author_avatar_url?.trim() || undefined,
      // 导入模型：项目 Owns 自己的人设。不发送 template_id——后端 Update 无条件清空，
      // 存量"绑定模板"项目保存后即迁移为自有值（运行时 task>template>project 解析）。
      reference_image_url: values.reference_image_url?.trim() || undefined,
      image_ratio: values.image_ratio || undefined,
      wechat_app_id: values.wechat_app_id?.trim() || undefined,
      wechat_secret: values.wechat_secret?.trim() || undefined,
      enable_publishing: values.enable_publishing || undefined,
      require_publish_approval: values.require_publish_approval || undefined,
    }

    // Auto-set image_ratio based on platform if not specified
    if (!payload.image_ratio) {
      if (payload.platform === 'article') {
        payload.image_ratio = '16:9'
      } else if (payload.platform === 'seednote') {
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
    if (editingProject) {
      await submit(async () => updateMutation.mutateAsync({ id: editingProject.id, data: payload }))
    } else {
      await submit(async () => createMutation.mutateAsync(payload))
    }
  }

  function handleDelete(id: string) {
    setDeleteTarget(id)
  }

  const isSubmitting = createMutation.isPending || updateMutation.isPending
  const isWechat = selectedPlatform === 'article'
  const isSeednote = selectedPlatform === 'seednote'
  const isEcommerce = selectedPlatform === 'ecommerce'

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
              onArchive={(id) => submit(async () => archiveMutation.mutateAsync(id))}
              onRestore={(id) => submit(async () => restoreMutation.mutateAsync(id))}
              onDelete={handleDelete}
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
            <form id="project-form" onSubmit={form.handleSubmit(onSubmit)} className="max-h-[60vh] space-y-4 overflow-y-auto p-1">
              <FormField control={form.control} name="platform" render={({ field }) => (
                <FormItem className="flex items-center gap-3 space-y-0">
                  <FormLabel className="shrink-0 w-20 text-right">平台</FormLabel>
                  <FormControl>
                    <Select
                      value={field.value}
                      onValueChange={(v) => {
                        field.onChange(v)
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

              <FormField control={form.control} name="positioning" render={({ field }) => (
                <FormItem>
                  <FormLabel>项目定位</FormLabel>
                  <FormControl>
                    <Textarea placeholder="例如 面向开发者的实用 AI 教程" {...field} />
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

              <FormField control={form.control} name="style" render={({ field }) => (
                <FormItem>
                  <div className="flex items-center justify-between gap-2">
                    <FormLabel>视觉风格</FormLabel>
                    {(isSeednote || isEcommerce) && (
                      <ReferenceImageUpload
                        value={referenceImageUrl}
                        onChange={(url) => form.setValue('reference_image_url', url, { shouldDirty: true })}
                        purpose="project"
                        compact
                      />
                    )}
                  </div>
                  <FormControl>
                    {isSeednote ? (
                      <div className="relative">
                        <Textarea
                          placeholder="描述你想要的图片视觉风格，如：手绘感，暖色调，小清新，治愈系水彩插画风格"
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
                        {analyzingStyle && (
                          <p className="text-xs text-muted-foreground">正在分析参考图...</p>
                        )}
                      </div>
                    ) : isEcommerce ? (
                      <Textarea
                        placeholder="描述品牌视觉风格基线，如：高端极简白底、国潮暖橙插画、电商爆款高饱和促销感。作为主图/详情/封面跨图一致的视觉锚点"
                        {...field}
                      />
                    ) : (
                      <Textarea
                        placeholder="描述文章封面与配图的视觉风格，如：温暖自然的生活摄影、柔光大地色系、写实治愈。留空则由项目定位与内容主题三维分析自动确定"
                        {...field}
                      />
                    )}
                  </FormControl>
                  <FormDescription>
                    {isSeednote
                      ? '描述 AI 生成图片的视觉风格，将用于封面和内容图的风格提示'
                      : isEcommerce
                        ? '品牌视觉维度——作为电商素材跨图一致的视觉基线（产品图在任务级上传）'
                        : '图片视觉维度——仅决定封面与配图的视觉，与写作风格、排版样式相互独立'}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )} />

              {/* 电商模板：选模板一次性导入视觉风格基线（三维风格架构——电商只用 Style 维度；
                  模块的默认模块/品牌/模型属任务级配置，项目只承载视觉风格）。 */}
              {isEcommerce && (
                <TemplatePicker type="ecommerce" selected={selectedTemplate} onSelect={handleProjectTemplateImport} />
              )}

              {isWechat && (
                <>
                  {/* 公众号模板：像小红书一样左右滑动选模板；选中后一次性把视觉/作者/写作
                      风格/排版导入表单，可继续自行调整（导入模型，项目 Owns 自己的人设）。
                      运行时解析优先级：task > task-template > plan > project 自身。 */}
                  <TemplatePicker type="article" selected={selectedTemplate} onSelect={handleProjectTemplateImport} />

                  {/* 写作风格（作者署名 + 写作风格模仿 + 可选头像）与排版：始终可编辑，
                      绑定到项目自身字段。选模板后自动填入，用户可覆盖。 */}
                  <PersonaBlock
                    authorName={authorName ?? ''}
                    onAuthorName={(v) => form.setValue('author', v, { shouldDirty: true })}
                    authorStyleIntro={authorStyleIntro ?? ''}
                    onAuthorStyleIntro={(v) => form.setValue('author_style_intro', v, { shouldDirty: true })}
                    authorAvatarUrl={authorAvatarUrl ?? ''}
                    onAuthorAvatarUrl={(v) => form.setValue('author_avatar_url', v, { shouldDirty: true })}
                  />
                  <ThemePicker theme={themeValue ?? ''} onTheme={(v) => form.setValue('theme', v, { shouldDirty: true })} />
                </>
              )}

              {/* 作者名（署名）：seednote 在此编辑；article 改由上方「公众号模板/写作风格」区块统一维护。 */}
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
            <Button type="submit" form="project-form" loading={isSubmitting}>
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
            <AlertDialogAction variant="destructive" disabled={deleteMutation.isPending} onClick={() => { if (deleteTarget) submit(async () => deleteMutation.mutateAsync(deleteTarget)) }}>
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
