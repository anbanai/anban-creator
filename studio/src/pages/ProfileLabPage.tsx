import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Check, CircleAlert, CircleCheck, Clock3, LoaderCircle, Plus, RotateCcw, Sparkles } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { ProfileContentFields } from '@/components/projects/ProfileContentFields'
import { ProfileAccountFields } from '@/components/projects/ProfileAccountFields'
import { ReferenceAssetUpload } from '@/components/projects/ReferenceAssetUpload'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { referenceSelectionFromValue } from '@/lib/reference-image'
import type { CreateProjectRequest, ProfileDimension, Project, ProjectProfile, ProjectProfileSetup, ReferenceImageValue } from '@/types'
import { parseCreationIntent, projectCreatedReturnHref } from '@/lib/command-center'
import { profileLabAgents as agents, projectAgentIDs, supportsProfileLab } from '@/lib/project-capabilities'
import { queryKeys } from '@/lib/query-keys'

const dimensions = [
  ['identity', '定位'], ['style', '风格'], ['audience', '受众'],
  ['platforms', '平台'], ['preferences', '偏好与红线'], ['memory', '经验记忆'],
] as const

type DimensionKey = typeof dimensions[number][0]
type Group = 'intent' | 'content' | 'boundaries'

const defaultSetup: ProjectProfileSetup = {
  account_status: 'new',
  platform_accounts: [
    { platform: 'wechat', profile_url: '', account_name: '', follower_range: '' },
    { platform: 'seednote', profile_url: '', account_name: '', follower_range: '' },
  ],
  answers: {
    intent: { goals: '', direction: '', differentiation: '', strengths: '', resources: '' },
    content: { preferences: '', formats: '', tone: '', audience: '', favorites: '', reference_accounts: '' },
    boundaries: { exclusions: '', collaboration: '', compliance: '' },
  },
  samples: [],
}

function isPublicURL(value: string) {
  try { return ['http:', 'https:'].includes(new URL(value).protocol) } catch { return false }
}

function applicationLabel(profile: ProjectProfile) {
  switch (profile.application_source) {
    case 'system_analysis': return '系统已应用'
    case 'user_confirmation': return '用户亲自确认'
    case 'user_edit': return '用户手动编辑'
    case 'user_restore': return '用户恢复历史版本'
    default: return profile.status === 'confirmed' ? '已应用' : '待补充'
  }
}

function profileStatus(profile?: ProjectProfile | null) {
  if (!profile) return { label: '创建后分析', icon: Clock3, tone: 'text-muted-foreground' }
  if (profile.initialization_status === 'queued' || profile.initialization_status === 'running') return { label: '分析中', icon: LoaderCircle, tone: 'text-sky-700' }
  if (profile.initialization_status === 'failed') return { label: '分析未完成', icon: CircleAlert, tone: 'text-destructive' }
  if (profile.initialization_status === 'ready') return { label: `已应用 · v${profile.version}`, icon: CircleCheck, tone: 'text-emerald-700' }
  return { label: '待开始', icon: Clock3, tone: 'text-muted-foreground' }
}

function primaryPlatform(agentIDs: string[]) {
  if (agentIDs.some((id) => id === 'wechat-article' || id === 'wechat-picture')) return 'wechat'
  if (agentIDs.includes('seednote')) return 'seednote'
  if (agentIDs.includes('montage')) return 'montage'
  if (agentIDs.includes('hypit')) return 'hypit'
  if (agentIDs.includes('whiteboard-animation')) return 'whiteboard-animation'
  return 'wechat'
}

function setupFromProject(project: Project): ProjectProfileSetup {
  const setup = project.profile_setup
  if (!setup) return defaultSetup
  return {
    ...defaultSetup,
    ...setup,
    platform_accounts: setup.platform_accounts ?? defaultSetup.platform_accounts,
    answers: {
      intent: { ...defaultSetup.answers.intent, ...setup.answers?.intent },
      content: { ...defaultSetup.answers.content, ...setup.answers?.content },
      boundaries: { ...defaultSetup.answers.boundaries, ...setup.answers?.boundaries },
    },
    samples: setup.samples ?? [],
  }
}

function sourcesForProfile(profile?: ProjectProfile | null): Set<string> {
  const result = new Set<string>()
  if (!profile) return result
  Object.values(profile.dimensions).forEach((dimension) => dimension.sources.forEach((source) => result.add(source)))
  return result
}

function ProfileLabPage() {
  const { projectId } = useParams()
  const [searchParams] = useSearchParams()
  const location = useLocation()
  const isEditing = Boolean(projectId)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const creationIntent = parseCreationIntent(searchParams)
  const returnTo = searchParams.get('return_to')
  const [name, setName] = useState('')
  const [keywords, setKeywords] = useState('')
  const [instructions, setInstructions] = useState('')
  const [visualStyle, setVisualStyle] = useState('')
  const [referenceImage, setReferenceImage] = useState<ReferenceImageValue | null>(null)
  const [referenceImageDirty, setReferenceImageDirty] = useState(false)
  const [referenceUploading, setReferenceUploading] = useState(false)
  const [selectedAgents, setSelectedAgents] = useState<string[]>([])
  const [setup, setSetup] = useState<ProjectProfileSetup>(defaultSetup)
  const [skipAnalysis, setSkipAnalysis] = useState(false)
  const [analyzeOnSave, setAnalyzeOnSave] = useState(true)
  const [drafts, setDrafts] = useState<Record<string, Record<string, unknown>>>({})
  const [dirtyDimensions, setDirtyDimensions] = useState<Set<string>>(new Set())
  const [draftVersions, setDraftVersions] = useState<Record<string, number>>({})
  const [newKeys, setNewKeys] = useState<Record<string, string>>({})
  const [newValues, setNewValues] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState('')
  const [currentStep, setCurrentStep] = useState(0)
  const [hydratedProjectID, setHydratedProjectID] = useState('')
  const lastProfileRevision = useRef<{ projectId: string; version: number } | null>(null)
  const activeProjectID = useRef(projectId)
  activeProjectID.current = projectId
  const mountedRef = useRef(true)
  const currentLocationKey = `${location.pathname}${location.search}`
  const currentLocationRef = useRef(currentLocationKey)
  currentLocationRef.current = currentLocationKey
  const creationViewKey = useRef(currentLocationKey)

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    setName('')
    setKeywords('')
    setInstructions('')
    setVisualStyle('')
    setReferenceImage(null)
    setReferenceImageDirty(false)
    setReferenceUploading(false)
    setSelectedAgents([])
    setSetup(defaultSetup)
    setSkipAnalysis(false)
    setAnalyzeOnSave(true)
    setHydratedProjectID('')
    setDirtyDimensions(new Set())
    setDrafts({})
    setDraftVersions({})
    setNewKeys({})
    setNewValues({})
    setFormError('')
    setCurrentStep(0)
  }, [projectId])

  const projectQuery = useQuery({
    queryKey: queryKeys.projects.detail(projectId ?? ''),
    queryFn: ({ signal }) => api.projects.get(projectId!, signal),
    enabled: isEditing,
  })
  const project = projectQuery.data?.project
  const profileQuery = useQuery({
    queryKey: ['project-profile', projectId],
    queryFn: () => api.projects.getAccountProfile(projectId!),
    enabled: Boolean(project && supportsProfileLab(project)),
    refetchInterval: (query) => {
      const status = query.state.data?.initialization_status
      return status === 'queued' || status === 'running' ? 2000 : false
    },
  })
  const profile = profileQuery.data ?? project?.profile ?? null
  const historyQuery = useQuery({
    queryKey: ['project-profile-revisions', projectId],
    queryFn: () => api.projects.profileRevisions(projectId!),
    enabled: Boolean(project && supportsProfileLab(project)),
  })
  const profileAnalysisQuery = useQuery({
    queryKey: ['project-profile-analysis', projectId, profile?.analysis_task_id],
    queryFn: () => api.projects.profileAnalysis(projectId!),
    enabled: Boolean(project && supportsProfileLab(project) && profile?.analysis_task_id),
    refetchInterval: (query) => query.state.data?.status === 'pending' || query.state.data?.status === 'running' ? 2000 : false,
  })

  useEffect(() => {
    if (!project || hydratedProjectID === project.id) return
    setName(project.name ?? '')
    setKeywords(project.keywords ?? '')
    setInstructions(project.instructions ?? project.positioning ?? '')
    setVisualStyle(project.visual_style ?? '')
    setReferenceImage(project.reference_image ?? null)
    setReferenceImageDirty(false)
    setSelectedAgents(projectAgentIDs(project))
    setSetup(setupFromProject(project))
    setHydratedProjectID(project.id)
  }, [hydratedProjectID, project])

  useEffect(() => {
    if (!profile) return
    setDrafts((current) => ({ ...current, ...Object.fromEntries(Object.entries(profile.dimensions).filter(([key]) => !dirtyDimensions.has(key)).map(([key, dimension]) => [key, dimension.content])) }))
    setDraftVersions((current) => ({ ...current, ...Object.fromEntries(dimensions.filter(([key]) => !dirtyDimensions.has(key)).map(([key]) => [key, profile.version])) }))
  }, [profile, dirtyDimensions])

  useEffect(() => {
    const analyzedProfile = profileAnalysisQuery.data?.profile
    const current = queryClient.getQueryData<ProjectProfile>(['project-profile', projectId])
    if (analyzedProfile && (!current || analyzedProfile.version >= current.version)) queryClient.setQueryData(['project-profile', projectId], analyzedProfile)
  }, [profileAnalysisQuery.data, projectId, queryClient])

  useEffect(() => {
    if (!projectId || profile?.version === undefined) return
    const previous = lastProfileRevision.current
    lastProfileRevision.current = { projectId, version: profile.version }
    if (!previous || previous.projectId !== projectId || previous.version === profile.version) return
    void queryClient.invalidateQueries({ queryKey: ['project-profile-revisions', projectId] })
    void queryClient.invalidateQueries({ queryKey: ['projects'] })
  }, [profile?.version, projectId, queryClient])

  const sourceTags = useMemo(() => sourcesForProfile(profile), [profile])
  const busy = isEditing && (projectQuery.isPending || (project && supportsProfileLab(project) && profileQuery.isPending))

  function cacheProfile(id: string, next: ProjectProfile) {
    queryClient.setQueryData<ProjectProfile>(['project-profile', id], (current) => current && current.version > next.version ? current : next)
  }

  const saveProfileDimension = useMutation({
    mutationFn: ({ id, key, dimension, revision }: { id: string; key: DimensionKey; dimension: ProfileDimension; revision: number }) => api.projects.updateProfileDimension(id, key, revision, dimension),
    onSuccess: (nextProfile, { id, key }) => {
      cacheProfile(id, nextProfile)
      queryClient.invalidateQueries({ queryKey: ['project-profile-revisions', id] })
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      if (activeProjectID.current !== id) return
      setDirtyDimensions((current) => new Set([...current].filter((item) => item !== key)))
      setNewKeys((current) => ({ ...current, [key]: '' }))
      setNewValues((current) => ({ ...current, [key]: '' }))
      toast.success('画像维度已保存')
    },
    onError: (error, { id }) => {
      if (activeProjectID.current !== id) return
      toast.error(getApiErrorMessage(error, '画像保存失败'))
      if (getApiErrorMessage(error, '').includes('profile_revision_conflict')) {
        toast.error('画像已更新，未保存内容已保留，请载入最新版本后再编辑')
        void queryClient.invalidateQueries({ queryKey: ['project-profile', id] })
      }
    },
  })

  const analysisMutation = useMutation({
    mutationFn: ({ id, retry, revision }: { id: string; retry: boolean; revision: number }) => retry
      ? api.projects.retryProfile(id, revision)
      : api.projects.refreshProfile(id, revision),
    onSuccess: ({ profile: nextProfile }, { id }) => {
      cacheProfile(id, nextProfile)
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      queryClient.invalidateQueries({ queryKey: ['project-profile-analysis', id] })
      if (activeProjectID.current !== id) return
      toast.success('免费画像分析已启动')
    },
    onError: (error, { id }) => { if (activeProjectID.current === id) toast.error(getApiErrorMessage(error, '画像分析未能启动')) },
  })

  const restoreMutation = useMutation({
    mutationFn: ({ id, revision, expectedRevision }: { id: string; revision: number; expectedRevision: number }) => api.projects.restoreProfileRevision(id, revision, expectedRevision),
    onSuccess: (restored, { id }) => {
      cacheProfile(id, restored)
      queryClient.invalidateQueries({ queryKey: ['project-profile-revisions', id] })
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      if (activeProjectID.current !== id) return
      toast.success(`已恢复到画像版本 v${restored.version}`)
    },
    onError: (error, { id }) => { if (activeProjectID.current === id) toast.error(getApiErrorMessage(error, '画像版本恢复失败')) },
  })

  const createMutation = useMutation({
    mutationFn: ({ payload }: { payload: CreateProjectRequest; viewKey: string }) => api.projects.create(payload),
    onSuccess: ({ project: created }, { payload, viewKey }) => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      toast.success(payload.profile_setup?.analyze === false ? '项目已创建' : '项目已创建，画像分析将在后台运行')
      if (!mountedRef.current || currentLocationRef.current !== viewKey || creationViewKey.current !== viewKey) return
      if (returnTo) {
        navigate(projectCreatedReturnHref({ returnTo, projectId: created.id, type: creationIntent.type, intent: creationIntent.intent }))
      } else navigate(`/projects/${created.id}/profile-lab`, { replace: true })
    },
    onError: (error) => setFormError(getApiErrorMessage(error, '创建项目失败，请重试')),
  })

  const saveProjectMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: CreateProjectRequest; analyze: boolean; priorProfile: ProjectProfile | null }) => api.projects.update(id, payload),
    onSuccess: async (_, { id, analyze, priorProfile }) => {
      await queryClient.invalidateQueries({ queryKey: ['projects'] })
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects.detail(id) })
      if (activeProjectID.current !== id) return
      toast.success('项目设置已保存')
      if (analyze && priorProfile && priorProfile.initialization_status !== 'queued' && priorProfile.initialization_status !== 'running') {
        analysisMutation.mutate({ id, retry: priorProfile.initialization_status === 'failed', revision: priorProfile.version })
      }
    },
    onError: (error, { id }) => { if (activeProjectID.current === id) setFormError(getApiErrorMessage(error, '项目保存失败，请重试')) },
  })

  function setAnswer(group: Group, key: string, value: string) {
    setSetup((current) => ({ ...current, answers: { ...current.answers, [group]: { ...current.answers[group], [key]: value } } }))
  }

  const steps = [
    { id: 'project-basics', label: '基础信息' },
    { id: 'accounts', label: '社媒链接' },
    { id: 'intent', label: '运营方向' },
    { id: 'boundaries', label: '偏好与红线' },
  ] as const

  function validateStep(step: number) {
    setFormError('')
    if (step === 0) {
      if (!name.trim()) {
        setFormError('请填写项目名称')
        document.getElementById('project-name')?.focus()
        return false
      }
      if (!isEditing && selectedAgents.length === 0) {
        setFormError('至少选择一种创作对象')
        document.getElementById('agent-selection')?.focus()
        return false
      }
    }
    if (step === 1) {
      const suppliedURLs = setup.platform_accounts.map((account) => account.profile_url.trim()).filter(Boolean)
      if (suppliedURLs.some((item) => !isPublicURL(item))) {
        setFormError('请填写有效的公开 HTTP 或 HTTPS 链接')
        return false
      }
    }
    return true
  }

  function focusStep(step: number) {
    window.requestAnimationFrame(() => document.getElementById(steps[step].id)?.focus())
  }

  function goToStep(next: number) {
    const bounded = Math.max(0, Math.min(steps.length - 1, next))
    if (next > currentStep) {
      for (let step = currentStep; step < bounded; step += 1) {
        if (validateStep(step)) continue
        setCurrentStep(step)
        focusStep(step)
        return
      }
    }
    setCurrentStep(bounded)
    setFormError('')
    focusStep(bounded)
  }

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (createMutation.isPending || saveProjectMutation.isPending || analysisMutation.isPending) return
    if (!validateStep(0)) {
      setCurrentStep(0)
      focusStep(0)
      return
    }
    if (!validateStep(1)) {
      setCurrentStep(1)
      focusStep(1)
      return
    }
    const profileSetup = {
      ...setup,
      platform_accounts: setup.platform_accounts.filter((account) => account.profile_url.trim() || account.account_name?.trim() || account.follower_range?.trim()).map((account) => ({ ...account, profile_url: account.profile_url.trim(), account_name: account.account_name?.trim(), follower_range: account.follower_range?.trim() })),
      samples: setup.samples.map((sample) => sample.trim()).filter(Boolean),
      ...(skipAnalysis && !isEditing ? { analyze: false } : {}),
    }
    const payload: CreateProjectRequest = {
      name: name.trim(),
      keywords: keywords.trim(),
      instructions: instructions.trim(),
      visual_style: visualStyle.trim(),
      profile_setup: profileSetup,
    }
    const selectedReferenceImage = referenceSelectionFromValue(referenceImage)
    if (!isEditing || referenceImageDirty) payload.reference_image = selectedReferenceImage
    const originalAgents = project ? projectAgentIDs(project) : []
    const capabilitiesChanged = !project || selectedAgents.length !== originalAgents.length || selectedAgents.some((id) => !originalAgents.includes(id))
    if (capabilitiesChanged) {
      payload.agent_ids = selectedAgents
      payload.platform = primaryPlatform(selectedAgents) as CreateProjectRequest['platform']
    }
    if (isEditing) saveProjectMutation.mutate({ id: projectId!, payload, analyze: analyzeOnSave, priorProfile: profile })
    else createMutation.mutate({ payload, viewKey: creationViewKey.current })
  }

  function saveDimension(key: DimensionKey) {
    if (!profile || !projectId || saveProfileDimension.isPending) return
    const original = profile.dimensions[key]
    if (draftVersions[key] !== profile.version) return
    const content = { ...(drafts[key] ?? {}) }
    const newKey = newKeys[key]?.trim()
    const newValue = newValues[key]?.trim()
    if (newKey && newValue) {
      if (newKey in content) { toast.error('字段已存在，请直接编辑已有内容'); return }
      content[newKey] = newValue
    }
    saveProfileDimension.mutate({ id: projectId, key, dimension: { ...original, content }, revision: draftVersions[key] })
  }

  function editDimension(key: DimensionKey, content: Record<string, unknown>) {
    setDirtyDimensions((current) => new Set([...current, key]))
    setDrafts((current) => ({ ...current, [key]: content }))
  }

  function reloadDimension(key: DimensionKey) {
    setDirtyDimensions((current) => new Set([...current].filter((item) => item !== key)))
    setNewKeys((current) => ({ ...current, [key]: '' }))
    setNewValues((current) => ({ ...current, [key]: '' }))
  }

  const projectLoadingError = projectQuery.isError ? getApiErrorMessage(projectQuery.error, '项目资料加载失败') : ''
  const status = profileStatus(profile)
  const StatusIcon = status.icon
  const analysisRunning = profile?.initialization_status === 'queued' || profile?.initialization_status === 'running'

  if (busy) return <div role="status" className="p-8 text-sm text-muted-foreground">正在加载 Profile Lab…</div>
  if (projectLoadingError) return <div role="alert" className="space-y-3 p-8 text-sm"><p>{projectLoadingError}</p><Button variant="outline" onClick={() => void projectQuery.refetch()}>重试</Button></div>
  if (project && !supportsProfileLab(project)) return <section className="space-y-3 p-8" aria-labelledby="profile-lab-title"><h1 id="profile-lab-title" className="text-xl font-semibold">该项目使用专属创作设置</h1><p className="text-sm text-muted-foreground">电商与朋友圈项目请在项目页编辑基本设置。</p><Link className="inline-flex min-h-11 items-center gap-2 rounded-md border px-3 text-sm hover:bg-muted" to="/projects"><ArrowLeft className="size-4" />返回项目</Link></section>

  return (
    <section aria-labelledby="profile-lab-title" className="mx-auto w-full max-w-[1720px] space-y-6 px-4 py-5 md:px-6 lg:px-8">
      <header className="flex flex-wrap items-start justify-between gap-4 border-b border-border pb-5">
        <div className="min-w-0">
          <Link to="/projects" className="mb-3 inline-flex min-h-11 items-center gap-2 text-sm text-muted-foreground hover:text-foreground"><ArrowLeft className="size-4" />项目</Link>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="font-mono text-xs uppercase text-muted-foreground">PROFILE LAB / {isEditing ? 'EDIT' : 'NEW'}</span>
            <span className="h-px w-8 bg-border" />
            <h1 id="profile-lab-title" className="text-2xl font-semibold">{isEditing ? name || project?.name : '新建项目'}</h1>
          </div>
          {isEditing && project?.updated_at && <p className="mt-1 text-xs text-muted-foreground">项目更新于 {new Date(project.updated_at).toLocaleDateString('zh-CN')}</p>}
        </div>
        {isEditing && (
          <div className={`inline-flex min-h-11 items-center gap-2 border-l-2 pl-3 text-sm ${status.tone}`} aria-live="polite">
            <StatusIcon className={`size-4 ${analysisRunning ? 'animate-spin' : ''}`} />{status.label}
          </div>
        )}
      </header>

      <div data-testid="profile-lab-layout" className="mx-auto max-w-3xl">
        <nav aria-label="创建进度" className="mb-8">
          <ol className="grid grid-cols-4 gap-2">
            {steps.map((step, index) => {
              const active = currentStep === index
              const complete = currentStep > index
              return <li key={step.id}>
                <button type="button" onClick={() => goToStep(index)} aria-current={active ? 'step' : undefined} className="group flex w-full flex-col gap-2 text-left">
                  <span className={`h-1 w-full transition-colors ${active || complete ? 'bg-foreground' : 'bg-border'}`} />
                  <span className={`text-xs ${active ? 'font-semibold text-foreground' : 'text-muted-foreground'}`}>{index + 1}. {step.label}</span>
                </button>
              </li>
            })}
          </ol>
        </nav>

        <form onSubmit={submit} className="min-w-0 space-y-5" noValidate>
          {currentStep === 0 && <section id="agent-selection" tabIndex={-1} className="scroll-mt-6 border-y border-foreground/80 py-4">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <h2 className="text-lg font-semibold">创作对象</h2>
              <span className="font-mono text-xs text-muted-foreground">{selectedAgents.length.toString().padStart(2, '0')} SELECTED</span>
            </div>
            <div className="mt-3 grid grid-cols-1 gap-x-5 sm:grid-cols-2">
              {agents.map((agent) => {
                const selected = selectedAgents.includes(agent.id)
                return (
                  <label key={agent.id} className={`flex min-h-[76px] cursor-pointer items-start gap-3 border-b py-3 transition-colors ${selected ? 'border-foreground/40 bg-muted/30' : 'border-border hover:bg-muted/20'}`}>
                    <input
                      aria-label={agent.label}
                      type="checkbox"
                      checked={selected}
                      onChange={() => setSelectedAgents((current) => selected ? current.filter((id) => id !== agent.id) : [...current, agent.id])}
                      className="mt-1 size-4 shrink-0 accent-primary"
                    />
                    <span className="min-w-0"><span className="block text-sm font-medium">{agent.label}</span><span className="mt-1 block text-xs text-muted-foreground">{agent.detail}</span></span>
                    {selected && <Check className="ml-auto mt-0.5 size-4 shrink-0 text-emerald-700" />}
                  </label>
                )
              })}
            </div>
          </section>}

          {currentStep === 0 && <section id="project-basics" tabIndex={-1} className="scroll-mt-6 border-b border-border pb-5">
            <div className="mb-4 flex items-baseline justify-between gap-3"><h2 className="text-lg font-semibold">基础信息与账号</h2><span className="text-xs text-muted-foreground">可稍后补充</span></div>
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="space-y-1.5 text-sm sm:col-span-2"><span>项目名称 <span className="text-destructive">*</span></span><Input id="project-name" aria-label="项目名称" value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：城市慢行观察" maxLength={80} /></label>
              <label className="space-y-1.5 text-sm"><span>关键词</span><Input aria-label="关键词" value={keywords} onChange={(event) => setKeywords(event.target.value)} placeholder="用逗号分隔" /></label>
              <label className="space-y-1.5 text-sm"><span>账号状态</span><select aria-label="账号状态" value={setup.account_status} onChange={(event) => setSetup((current) => ({ ...current, account_status: event.target.value as ProjectProfileSetup['account_status'] }))} className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm"><option value="new">筹备中 / 暂无账号</option><option value="existing">已有账号</option></select></label>
              <label className="space-y-1.5 text-sm sm:col-span-2"><span>项目定位</span><Textarea aria-label="项目定位" value={instructions} onChange={(event) => setInstructions(event.target.value)} placeholder="这项内容希望长期服务什么方向？" rows={2} /></label>
            </div>
          </section>}

          {currentStep === 1 && <section id="accounts" tabIndex={-1} className="scroll-mt-6 space-y-5 border-b border-border pb-5">
            <div><h2 className="text-lg font-semibold">你的公开账号</h2><p className="mt-1 text-sm text-muted-foreground">没有账号也可以跳过，之后随时补充。</p></div>
            <ProfileAccountFields accounts={setup.platform_accounts} onChange={(accounts) => setSetup((current) => ({ ...current, platform_accounts: accounts }))} />
            <details className="border-t border-border pt-4"><summary className="cursor-pointer text-sm font-medium">更多资料（可选）</summary><div className="mt-4 space-y-4">
              <label className="block space-y-1.5 text-sm"><span>内容样本</span><Textarea aria-label="内容样本" value={setup.samples.join('\n')} onChange={(event) => setSetup((current) => ({ ...current, samples: event.target.value.split('\n') }))} placeholder="每行一个公开链接、标题或内容片段" rows={4} /></label>
              <section aria-label="视觉参考与风格" className="space-y-2"><h3 className="text-sm font-medium">视觉参考与风格</h3><div className="grid gap-3 sm:grid-cols-[8rem_minmax(0,1fr)]"><ReferenceAssetUpload value={referenceImage} onChange={(value) => { setReferenceImage(value); setReferenceImageDirty(true) }} purpose="project_reference" ariaLabel="项目参考图文件" onUploadingChange={setReferenceUploading} /><label className="space-y-1.5 text-sm"><span>视觉风格描述</span><Textarea aria-label="视觉风格描述" value={visualStyle} onChange={(event) => setVisualStyle(event.target.value)} placeholder="例如：自然光、低饱和、留白充足" rows={4} maxLength={4000} /></label></div><p className="text-xs text-muted-foreground">这些资料会帮助创作任务保持一致。</p></section>
            </div></details>
          </section>}

          {currentStep === 2 && <section id="intent" tabIndex={-1} className="scroll-mt-6 border-b border-border pb-5">
            <h2 className="mb-4 text-lg font-semibold">运营意图</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="space-y-1.5 text-sm"><span>运营目标</span><Textarea aria-label="运营目标" value={setup.answers.intent.goals} onChange={(event) => setAnswer('intent', 'goals', event.target.value)} placeholder="希望账号长期带来什么价值？" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>内容方向</span><Textarea aria-label="内容方向" value={setup.answers.intent.direction} onChange={(event) => setAnswer('intent', 'direction', event.target.value)} placeholder="长期关注的话题与领域" rows={2} /></label>
              <label className="space-y-1.5 text-sm sm:col-span-2"><span>差异化</span><Textarea aria-label="差异化" value={setup.answers.intent.differentiation} onChange={(event) => setAnswer('intent', 'differentiation', event.target.value)} placeholder="经验、视角、资源或表达上的不同" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>优势与资源</span><Textarea aria-label="优势与资源" value={setup.answers.intent.strengths} onChange={(event) => setAnswer('intent', 'strengths', event.target.value)} placeholder="经验、专业能力、独特资源" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>可用素材资源</span><Textarea aria-label="可用素材资源" value={setup.answers.intent.resources} onChange={(event) => setAnswer('intent', 'resources', event.target.value)} placeholder="案例、数据、团队或拍摄条件" rows={2} /></label>
            </div>
          </section>}

          {currentStep === 3 && <section id="content" className="scroll-mt-6 border-b border-border pb-5">
            <h2 className="mb-4 text-lg font-semibold">内容偏好与受众</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="space-y-1.5 text-sm"><span>内容偏好</span><Textarea aria-label="内容偏好" value={setup.answers.content.preferences} onChange={(event) => setAnswer('content', 'preferences', event.target.value)} placeholder="希望多出现什么内容？" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>内容形式</span><Textarea aria-label="内容形式" value={setup.answers.content.formats} onChange={(event) => setAnswer('content', 'formats', event.target.value)} placeholder="文章、图解、清单、口播…" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>表达调性</span><Textarea aria-label="表达调性" value={setup.answers.content.tone} onChange={(event) => setAnswer('content', 'tone', event.target.value)} placeholder="例如：克制、幽默、专业" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>目标受众</span><Textarea aria-label="目标受众" value={setup.answers.content.audience} onChange={(event) => setAnswer('content', 'audience', event.target.value)} placeholder="具体描述希望影响的人" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>喜欢的内容</span><Textarea aria-label="喜欢的内容" value={setup.answers.content.favorites} onChange={(event) => setAnswer('content', 'favorites', event.target.value)} placeholder="喜欢的选题、形式或表达方式" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>参考账号</span><Textarea aria-label="参考账号" value={setup.answers.content.reference_accounts} onChange={(event) => setAnswer('content', 'reference_accounts', event.target.value)} placeholder="账号名称或公开主页链接" rows={2} /></label>
            </div>
          </section>}

          {currentStep === 3 && <section id="boundaries" tabIndex={-1} className="scroll-mt-6 border-b border-border pb-5">
            <h2 className="mb-4 text-lg font-semibold">不做内容与合作边界</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="space-y-1.5 text-sm"><span>内容红线</span><Textarea aria-label="内容红线" value={setup.answers.boundaries.exclusions} onChange={(event) => setAnswer('boundaries', 'exclusions', event.target.value)} placeholder="绝不出现或不希望生成的内容" rows={2} /></label>
              <label className="space-y-1.5 text-sm"><span>合作边界</span><Textarea aria-label="合作边界" value={setup.answers.boundaries.collaboration} onChange={(event) => setAnswer('boundaries', 'collaboration', event.target.value)} placeholder="合作主题、对象或表达上的限制" rows={2} /></label>
              <label className="space-y-1.5 text-sm sm:col-span-2"><span>合规要求</span><Textarea aria-label="合规要求" value={setup.answers.boundaries.compliance} onChange={(event) => setAnswer('boundaries', 'compliance', event.target.value)} placeholder="行业规范、事实核验或声明要求" rows={2} /></label>
            </div>
          </section>}

          <div className="space-y-3 border-b border-border pb-4">
            {formError && <p role="alert" className="text-sm text-destructive">{formError}</p>}
            <div className="flex flex-wrap items-center justify-between gap-3">
              <Button type="button" variant="ghost" nativeButton={false} render={<Link to="/projects" />}><ArrowLeft />返回项目</Button>
              <div className="flex items-center gap-2">
                {currentStep > 0 && <Button type="button" variant="outline" onClick={() => goToStep(currentStep - 1)}>上一步</Button>}
                {currentStep < steps.length - 1 ? <Button type="button" onClick={() => goToStep(currentStep + 1)}>下一步</Button> : <Button type="submit" disabled={createMutation.isPending || saveProjectMutation.isPending || analysisMutation.isPending || referenceUploading}>{(createMutation.isPending || saveProjectMutation.isPending) ? <LoaderCircle className="animate-spin" /> : <Check />}{isEditing ? '保存设置' : '创建项目'}</Button>}
              </div>
            </div>
            {currentStep === steps.length - 1 && <div className="space-y-3 pt-2">{!isEditing && <label className="flex min-h-11 items-center gap-3 text-sm"><input type="checkbox" checked={skipAnalysis} onChange={(event) => setSkipAnalysis(event.target.checked)} className="size-4 accent-primary" aria-label="跳过画像分析" />先创建，稍后分析画像</label>}{isEditing && <label className="flex min-h-11 items-center gap-3 text-sm"><input type="checkbox" checked={analyzeOnSave} disabled={analysisRunning} onChange={(event) => setAnalyzeOnSave(event.target.checked)} className="size-4 accent-primary" aria-label="保存后重新分析画像" />{analysisRunning ? '分析中；保存资料后可重新分析' : '保存后重新分析画像'}</label>}</div>}
          </div>
        </form>

        {isEditing && <aside className="space-y-4 xl:sticky xl:top-5">
          <section className="border-y border-foreground/80 py-4">
            <div className="flex items-center justify-between gap-2"><h2 className="text-sm font-semibold">六维画像</h2><span className="font-mono text-[10px] text-muted-foreground">PROFILE / 6</span></div>
            {profile && <p className="mt-3 text-xs font-medium">{applicationLabel(profile)}</p>}
            {profileQuery.isError && <div role="alert" className="mt-3 space-y-2 text-xs text-destructive"><p>{getApiErrorMessage(profileQuery.error, '画像资料加载失败，当前内容可能不是最新版本')}</p><Button variant="outline" size="sm" onClick={() => void profileQuery.refetch()} aria-label="重新加载画像">重试</Button></div>}
            {profile && profile.version > 0 && analysisRunning && <p className="mt-3 border-l-2 border-sky-600 pl-2 text-xs leading-5 text-sky-800">分析完成前，当前 v{profile.version} 继续用于创作。</p>}
            {!profile && <p className="mt-3 text-xs leading-5 text-muted-foreground">完成项目创建后，画像会按已填写资料自动分析。也可跳过，之后再启动。</p>}
            <ul className="mt-3 divide-y divide-border">
              {dimensions.map(([key, label]) => {
                const dimension = profile?.dimensions[key]
                const gap = dimension?.missing_fields.length ?? 0
                const filled = dimension ? Object.keys(dimension.content).length > 0 : false
                return <li key={key} className="flex min-h-11 items-center justify-between gap-2 py-2 text-sm"><span>{label}</span><span className="text-right text-xs text-muted-foreground">{!profile ? '待分析' : gap > 0 ? `缺 ${gap} 项` : filled ? '有内容' : '待补充'}</span></li>
              })}
            </ul>
            {profile && (
              <div className="mt-3 space-y-3 border-t border-border pt-3">
                {profile.analysis_limits.length > 0 && <div><p className="text-xs font-medium">分析范围</p><ul className="mt-1 list-inside list-disc text-xs leading-5 text-muted-foreground">{profile.analysis_limits.map((item) => <li key={item}>{item}</li>)}</ul></div>}
                {profile.follow_up_questions.length > 0 && <div><p className="text-xs font-medium">待补充问题</p><ul className="mt-1 space-y-1 text-xs leading-5 text-muted-foreground">{profile.follow_up_questions.map((item) => <li key={item}>• {item}</li>)}</ul></div>}
                {profile.initialization_status === 'failed' && <div role="alert" className="border-l-2 border-destructive pl-2 text-xs leading-5 text-destructive">{profile.last_error || '主页分析失败，当前画像未被替换。'}</div>}
                {(profile.initialization_status === 'failed' || profile.initialization_status === 'ready') && <Button variant="outline" size="sm" disabled={analysisMutation.isPending || profileQuery.isError} onClick={() => analysisMutation.mutate({ id: projectId!, retry: profile.initialization_status === 'failed', revision: profile.version })}><RotateCcw />重新分析</Button>}
                {profile.initialization_status === 'not_started' && <Button variant="outline" size="sm" disabled={analysisMutation.isPending || profileQuery.isError} onClick={() => analysisMutation.mutate({ id: projectId!, retry: false, revision: profile.version })}><Sparkles />开始免费分析</Button>}
                {analysisRunning && <p role="status" className="flex items-center gap-2 text-xs text-sky-700"><LoaderCircle className="size-3.5 animate-spin" />画像正在分析，完成后会自动应用。</p>}
              </div>
            )}
            {profile && <div className="mt-3 border-t border-border pt-3"><p className="text-xs font-medium">证据来源</p><div className="mt-2 flex flex-wrap gap-1.5">{Array.from(sourceTags).map((source) => <span key={source} className="border border-border px-1.5 py-1 font-mono text-[10px]">{source}</span>)}{sourceTags.size === 0 && <span className="text-xs text-muted-foreground">[待补充]</span>}</div></div>}
          </section>

          {profile && (
            <section aria-label="画像版本历史" className="border-b border-border pb-3">
              <div className="flex items-center justify-between gap-2"><h2 className="text-sm font-semibold">版本历史</h2><span className="font-mono text-xs text-muted-foreground">{historyQuery.data?.length ?? 0}</span></div>
              {historyQuery.isPending ? <p className="py-3 text-xs text-muted-foreground">正在读取版本…</p> : historyQuery.isError ? <div role="alert" className="space-y-2 py-3 text-xs text-destructive"><p>{getApiErrorMessage(historyQuery.error, '版本历史加载失败')}</p><Button variant="outline" size="sm" onClick={() => void historyQuery.refetch()} aria-label="重新加载版本历史">重试</Button></div> : historyQuery.data?.length ? (
                <ul className="mt-2 divide-y divide-border">
                  {[...historyQuery.data].sort((a, b) => b.revision - a.revision).map((revision) => (
                    <li key={revision.id || revision.revision} className="flex min-h-12 items-center justify-between gap-2 py-2">
                      <div><p className="font-mono text-xs">v{revision.revision}{revision.revision === profile.version ? ' · 当前' : ''}</p><p className="text-[10px] text-muted-foreground">{new Date(revision.created_at).toLocaleDateString('zh-CN')}</p></div>
                      {revision.revision !== profile.version && <Button variant="ghost" size="xs" disabled={restoreMutation.isPending || analysisRunning || profileQuery.isError} onClick={() => restoreMutation.mutate({ id: projectId!, revision: revision.revision, expectedRevision: profile.version })} aria-label={`恢复版本 ${revision.revision}`}><RotateCcw />恢复</Button>}
                    </li>
                  ))}
                </ul>
              ) : <p className="py-3 text-xs text-muted-foreground">暂无可恢复版本</p>}
            </section>
          )}
        </aside>}
      </div>

      {isEditing && profile && (
        <section className="border-t border-foreground/80 pt-5">
          <div className="mb-4 flex flex-wrap items-end justify-between gap-2"><div><p className="font-mono text-[10px] uppercase text-muted-foreground">Applied profile · v{profile.version}</p><h2 className="mt-1 text-xl font-semibold">画像内容</h2></div><p className="text-xs text-muted-foreground">来源和证据保留在每个维度下；推断始终标记为待确认。</p></div>
          <div className="grid gap-4 lg:grid-cols-2">
            {dimensions.map(([key, label]) => {
              const dimension = profile.dimensions[key]
              const contentDraft = drafts[key] ?? {}
              const contentKeys = Object.keys(contentDraft)
              const stale = dirtyDimensions.has(key) && draftVersions[key] !== profile.version
              return (
                <article key={key} role="region" aria-label={`${label} ${key}`} className={`min-w-0 border p-4 ${key === 'preferences' ? 'border-l-4 border-l-amber-600' : 'border-border'}`}>
                  <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-base font-semibold">{label}</h3><span className="font-mono text-[10px] text-muted-foreground">{key}</span></div>
                  <div className="mt-3 space-y-3">
                    {stale && <div role="alert" className="space-y-2 border-l-2 border-amber-600 pl-2 text-xs text-amber-800"><p>后台画像已更新，未保存内容已保留。请记录需要的修改，再载入最新版本。</p><Button variant="outline" size="sm" onClick={() => reloadDimension(key)} aria-label={`载入最新${label}`}>载入最新版本</Button></div>}
                    {contentKeys.length ? <ProfileContentFields content={contentDraft} onChange={(next) => editDimension(key, next)} /> : <p className="border-l-2 border-muted-foreground/30 pl-2 text-sm text-muted-foreground">[待补充] 当前没有已记录内容。</p>}
                    <div className="grid gap-2 sm:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)_auto]">
                      <Input aria-label={`${label}新字段名称`} value={newKeys[key] ?? ''} onChange={(event) => { setDirtyDimensions((current) => new Set([...current, key])); setNewKeys((current) => ({ ...current, [key]: event.target.value })) }} placeholder="新字段" />
                      <Input aria-label={`${label}新字段内容`} value={newValues[key] ?? ''} onChange={(event) => { setDirtyDimensions((current) => new Set([...current, key])); setNewValues((current) => ({ ...current, [key]: event.target.value })) }} placeholder="补充内容" />
                      <Button type="button" variant="outline" size="icon" aria-label={`添加${label}字段`} onClick={() => {
                        const field = newKeys[key]?.trim()
                        if (!field) return
                        if (field in contentDraft) { toast.error('字段已存在，请直接编辑已有内容'); return }
                        editDimension(key, { ...contentDraft, [field]: newValues[key] ?? '' })
                        setNewKeys((current) => ({ ...current, [key]: '' }))
                        setNewValues((current) => ({ ...current, [key]: '' }))
                      }}><Plus /></Button>
                    </div>
                    <Button type="button" size="sm" variant="secondary" disabled={saveProfileDimension.isPending || stale || profileQuery.isError} onClick={() => saveDimension(key)}><Check />保存{label}</Button>
                    <div className="border-t border-border pt-3 text-xs leading-5">
                      <p className="font-medium">来源</p><div className="mt-1 flex flex-wrap gap-1.5">{dimension.sources.length ? dimension.sources.map((source) => <span key={source} className="border border-border px-1.5 py-0.5 font-mono text-[10px]">{source}</span>) : <span className="text-muted-foreground">[待补充]</span>}</div>
                      <p className="mt-2 font-medium">证据</p>{dimension.evidence.length ? <ul className="mt-1 space-y-1 text-muted-foreground">{dimension.evidence.map((evidence) => <li key={evidence}>{evidence}</li>)}</ul> : <p className="text-muted-foreground">[待补充]</p>}
                      {dimension.missing_fields.length > 0 && <p className="mt-2 text-amber-800">待补充：{dimension.missing_fields.join('、')}</p>}
                      {key === 'preferences' && <p className="mt-2 border-l-2 border-amber-600 pl-2 font-medium text-amber-900">preferences 始终作为创作硬性约束。</p>}
                    </div>
                  </div>
                </article>
              )
            })}
          </div>
        </section>
      )}
    </section>
  )
}

export default ProfileLabPage
