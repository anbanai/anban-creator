import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Brain, FileText, ImageOff, Loader2, LockKeyhole, MessageSquareText, Pencil, RefreshCw, RotateCcw, Save, Sparkles, X } from 'lucide-react'

import { projectsApi } from '@/lib/api/projects'
import { normalizeProjectProfile } from '@/lib/project-profile'
import { getApiErrorMessage } from '@/lib/http-client'
import type { ProfileDimension, Project, ProjectProfile } from '@/types'
import { supportsProfileLab } from '@/lib/project-capabilities'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'
import { ProfileDimensionCard } from './ProfileDimensionCard'

interface ProjectMemoryDialogProps {
  projectId: string
  projectName: string
  project?: Project
  open: boolean
  onOpenChange: (open: boolean) => void
  onUpdateProfile?: () => void
}
const dimensionLabels: Record<string, string> = {
  identity: '定位', style: '风格', audience: '受众', platforms: '平台', preferences: '偏好与红线', memory: '经验记忆',
}

function formatBytes(bytes: number) {
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KiB`
}

function formatUpdatedAt(value: string | null) {
  if (!value) return '尚未更新'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function fileName(path: string) {
  return path.split('/').pop() || path
}

function fileDirectory(path: string) {
  const parts = path.split('/')
  return parts.length > 1 ? parts.slice(0, -1).join('/') : ''
}

function isManagedMemoryPath(path: string) {
  return path === 'MEMORY.md' || path === 'AGENTS.md' || path === 'CLAUDE.md' || path.startsWith('.claude/')
}

function profileDimensionForPath(path: string): keyof ProjectProfile['dimensions'] | null {
  const match = /^profile\/(identity|style|audience|platforms|preferences|memory)\.md$/.exec(path)
  return match ? match[1] as keyof ProjectProfile['dimensions'] : null
}

export function ProjectMemoryDialog({ projectId, projectName, project, open, onOpenChange, onUpdateProfile }: ProjectMemoryDialogProps) {
  const [selectedPath, setSelectedPath] = useState('')
  const [editing, setEditing] = useState(false)
  const [draftContent, setDraftContent] = useState('')
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [profileTexts, setProfileTexts] = useState<Record<string, string>>({})
  const [dirtyDimensions, setDirtyDimensions] = useState<Record<string, boolean>>({})
  const [profileErrors, setProfileErrors] = useState<Record<string, string>>({})
  const [profileActionError, setProfileActionError] = useState('')
  const readingPaneRef = useRef<HTMLElement>(null)
  const queryClient = useQueryClient()
  const profileSupported = Boolean(project && supportsProfileLab(project))
  const query = useQuery({
    queryKey: ['project-memory', projectId],
    queryFn: () => projectsApi.memory(projectId),
    enabled: open,
    staleTime: 0,
  })
  const files = query.data?.files ?? []
  const profileQuery = useQuery({
    queryKey: ['project-profile', projectId],
    queryFn: () => projectsApi.getAccountProfile(projectId),
    enabled: open && profileSupported,
    refetchInterval: (profileQuery) => {
      const status = profileQuery.state.data?.initialization_status
      return status === 'queued' || status === 'running' ? 2000 : false
    },
  })
  const rawProfile = profileQuery.data ?? project?.profile ?? null
  const profile = useMemo(() => normalizeProjectProfile(rawProfile), [rawProfile])
  const profileAnalysisQuery = useQuery({
    queryKey: ['project-profile-analysis', projectId, profile?.analysis_task_id],
    queryFn: () => projectsApi.profileAnalysis(projectId),
    enabled: open && Boolean(profile?.analysis_task_id) && profileSupported,
    refetchInterval: (analysisQuery) => {
      const status = analysisQuery.state.data?.status
      return status === 'pending' || status === 'running' ? 2000 : false
    },
  })
  const analysisStatus = profileAnalysisQuery.data?.status ?? profile?.initialization_status ?? 'not_started'
  const analysisRunning = analysisStatus === 'pending' || analysisStatus === 'queued' || analysisStatus === 'running'
  const analysisFailed = analysisStatus === 'failed' || analysisStatus === 'cancelled'

  useEffect(() => {
    if (!files.length) return
    if (!files.some((file) => file.path === selectedPath)) {
      setSelectedPath(files.find((file) => file.path === 'MEMORY.md')?.path ?? files[0].path)
    }
  }, [files, selectedPath])

  useEffect(() => {
    setDraftContent(files.find((file) => file.path === selectedPath)?.content ?? '')
    setSaveError(null)
  }, [files, selectedPath])

  useEffect(() => {
    if (readingPaneRef.current) readingPaneRef.current.scrollTop = 0
  }, [selectedPath, open])

  useEffect(() => {
    const analyzedProfile = profileAnalysisQuery.data?.profile
    if (!analyzedProfile) return
    queryClient.setQueryData<ProjectProfile>(['project-profile', projectId], (current) => (
      !current || analyzedProfile.version >= current.version ? analyzedProfile : current
    ))
  }, [profileAnalysisQuery.data, projectId, queryClient])

  useEffect(() => {
    const loadedProfile = normalizeProjectProfile(profileQuery.data)
    if (!loadedProfile) return
    setProfileTexts((current) => {
      const next = { ...current }
      Object.entries(loadedProfile.dimensions).forEach(([name, dimension]) => {
        if (!dirtyDimensions[name] || next[name] === undefined) next[name] = JSON.stringify(dimension.content, null, 2)
      })
      return next
    })
  }, [dirtyDimensions, profileQuery.data])

  useEffect(() => {
    if (profile?.version) void queryClient.invalidateQueries({ queryKey: ['project-memory', projectId] })
  }, [profile?.version, projectId, queryClient])

  const analysisMutation = useMutation({
    mutationFn: ({ retry, revision }: { retry: boolean; revision: number }) => retry
      ? projectsApi.retryProfile(projectId, revision)
      : projectsApi.refreshProfile(projectId, revision),
    onSuccess: ({ profile: nextProfile }) => {
      setProfileActionError('')
      queryClient.setQueryData(['project-profile', projectId], nextProfile)
      void queryClient.invalidateQueries({ queryKey: ['project-profile-analysis', projectId] })
      void queryClient.invalidateQueries({ queryKey: ['projects'] })
    },
    onError: (error) => {
      const message = getApiErrorMessage(error, '画像分析未能启动')
      setProfileActionError(message.includes('profile_revision_conflict') ? '画像版本已变化，正在重新加载当前内容。' : message)
      void queryClient.invalidateQueries({ queryKey: ['project-profile', projectId] })
    },
  })

  const dimensionMutation = useMutation({
    mutationFn: ({ name, dimension, revision }: { name: string; dimension: ProfileDimension; revision: number }) => projectsApi.updateProfileDimension(projectId, name, revision, dimension),
    onSuccess: (nextProfile, { name }) => {
      queryClient.setQueryData(['project-profile', projectId], nextProfile)
      setProfileTexts((current) => ({ ...current, [name]: JSON.stringify(nextProfile.dimensions[name as keyof ProjectProfile['dimensions']].content, null, 2) }))
      setDirtyDimensions((current) => ({ ...current, [name]: false }))
      setProfileErrors((current) => ({ ...current, [name]: '' }))
      void queryClient.invalidateQueries({ queryKey: ['project-profile-analysis', projectId] })
      void queryClient.invalidateQueries({ queryKey: ['project-memory', projectId] })
    },
    onError: (error, { name }) => {
      const message = getApiErrorMessage(error, '画像保存失败')
      const conflict = message.includes('profile_revision_conflict')
      setProfileErrors((current) => ({ ...current, [name]: conflict ? '画像版本已更新，未保存的修改已保留。载入最新版本后再保存。' : message }))
      if (conflict) void queryClient.invalidateQueries({ queryKey: ['project-profile', projectId] })
    },
  })

  const selected = files.find((file) => file.path === selectedPath) ?? files[0]
  const selectedProfileDimension = selected ? profileDimensionForPath(selected.path) : null
  const hasProfileFiles = files.some((file) => Boolean(profileDimensionForPath(file.path)))
  const selectedDimension = selectedProfileDimension && profile
    ? profile.dimensions[selectedProfileDimension]
    : null
  const canEdit = Boolean(selected && !isManagedMemoryPath(selected.path) && !selectedProfileDimension)
  const memoryRefreshing = query.isFetching
  const profileRefreshing = profileQuery.isFetching || profileAnalysisQuery.isFetching
  const refreshLabel = '刷新项目记忆'

  const save = async () => {
    if (!selected || !canEdit) return
    setSaving(true)
    setSaveError(null)
    try {
      await projectsApi.updateMemory(projectId, { path: selected.path, expected_content: selected.content, content: draftContent })
      setEditing(false)
      await query.refetch()
    } catch (error) {
      setSaveError(getApiErrorMessage(error, '保存失败，请刷新后重试'))
    } finally {
      setSaving(false)
    }
  }

  function saveDimension(name: string) {
    if (!profile || analysisRunning || dimensionMutation.isPending) return
    let content: Record<string, unknown>
    try {
      const parsed: unknown = JSON.parse(profileTexts[name] ?? '{}')
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('object required')
      content = parsed as Record<string, unknown>
    } catch {
      setProfileErrors((current) => ({ ...current, [name]: `${dimensionLabels[name] ?? name} 的内容必须是有效 JSON 对象` }))
      return
    }
    setProfileErrors((current) => ({ ...current, [name]: '' }))
    dimensionMutation.mutate({ name, dimension: { ...profile.dimensions[name as keyof ProjectProfile['dimensions']], content }, revision: profile.version })
  }

  function loadLatestDimension(name: string) {
    const latest = profileQuery.data?.dimensions[name as keyof ProjectProfile['dimensions']]
    if (!latest) return
    setProfileTexts((current) => ({ ...current, [name]: JSON.stringify(latest.content, null, 2) }))
    setDirtyDimensions((current) => ({ ...current, [name]: false }))
    setProfileErrors((current) => ({ ...current, [name]: '' }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[calc(100dvh-1rem)] max-h-[820px] w-[calc(100vw-1rem)] max-w-none flex-col gap-0 overflow-hidden p-0 sm:h-[88dvh] sm:w-[92vw] sm:max-w-[1080px]"
      >
        <DialogHeader className="shrink-0 gap-1 border-b bg-background px-5 py-4 pr-24 sm:px-6 sm:pr-24">
          <div className="flex min-w-0 items-center gap-2">
            <Brain className="size-4 shrink-0 text-primary" />
            <DialogTitle className="truncate text-lg">项目记忆</DialogTitle>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <DialogDescription className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
              {project ? <ProjectIdentity project={project} compact /> : <span className="break-words font-medium text-foreground/80">{projectName}</span>}
              <span aria-hidden="true">·</span>
              <span>{query.data?.updated_at ? `最近更新 ${formatUpdatedAt(query.data.updated_at)}` : '创作过程中积累的项目上下文'}</span>
            </DialogDescription>
            {profileSupported && onUpdateProfile && project?.status !== 'archived' && <Button variant="outline" size="sm" className="shrink-0" onClick={onUpdateProfile}><MessageSquareText />对话更新画像</Button>}
          </div>
          <Tooltip>
            <TooltipTrigger render={<Button variant="outline" size="icon-md" className="absolute right-14 top-2" aria-label={refreshLabel} disabled={memoryRefreshing || profileRefreshing} onClick={() => {
              void query.refetch()
              if (profileSupported) void profileQuery.refetch()
              if (profile?.analysis_task_id) void profileAnalysisQuery.refetch()
            }} />}>
              <RefreshCw className={(memoryRefreshing || profileRefreshing) ? 'size-4 animate-spin' : 'size-4'} />
            </TooltipTrigger>
            <TooltipContent>{refreshLabel}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger render={<DialogClose render={<Button variant="ghost" size="icon-md" className="absolute right-2 top-2" aria-label="关闭项目记忆" />} />}>
              <X className="size-4" />
            </TooltipTrigger>
            <TooltipContent>关闭</TooltipContent>
          </Tooltip>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto">
          <section className="min-h-0 h-full">
            {query.isLoading ? (
              <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="mr-2 size-4 animate-spin" />正在读取记忆</div>
            ) : query.isError ? (
              <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
                <p className="text-sm font-medium">无法读取项目记忆</p>
                <Button variant="outline" size="sm" onClick={() => void query.refetch()}><RefreshCw />重试</Button>
              </div>
            ) : query.data?.status === 'empty' || files.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center text-muted-foreground">
                <Brain className="size-8" />
                <p className="text-sm font-medium text-foreground">还没有项目记忆</p>
                <p className="max-w-sm text-xs">完成一次创作后，项目会在这里积累可复用的上下文。</p>
              </div>
            ) : (
              <div className="grid h-full min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)] md:grid-cols-[220px_minmax(0,1fr)] md:grid-rows-1">
                <div className="min-w-0 border-b bg-muted/20 p-3 md:hidden">
                  <select
                    aria-label="选择记忆文件"
                    value={selected?.path ?? ''}
                    onChange={(event) => { setEditing(false); setSelectedPath(event.target.value) }}
                    className="h-10 w-full min-w-0 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    {files.map((file) => <option key={file.path} value={file.path}>{file.path}</option>)}
                  </select>
                </div>
                <nav aria-label="记忆文件" className="hidden min-h-0 min-w-0 overflow-y-auto border-r bg-muted/20 p-3 md:block">
                  <p className="mb-2 px-2 text-xs font-medium text-muted-foreground">文件 <span className="ml-1 tabular-nums">{files.length}</span></p>
                  <div className="space-y-1">
                    {files.map((file) => (
                      <button
                        type="button"
                        key={file.path}
                        aria-label={file.path}
                        aria-current={selected?.path === file.path ? 'true' : undefined}
                        title={file.path}
                        onClick={() => { setEditing(false); setSelectedPath(file.path) }}
                        className={`flex w-full min-w-0 items-start gap-2 rounded-lg border px-2 py-2.5 text-left outline-none transition-colors hover:bg-background focus-visible:ring-2 focus-visible:ring-ring ${selected?.path === file.path ? 'border-border bg-background text-foreground shadow-sm' : 'border-transparent text-muted-foreground'}`}
                      >
                        <FileText className="mt-0.5 size-4 shrink-0" />
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-xs font-medium">{fileName(file.path)}</span>
                          {fileDirectory(file.path) && <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">{fileDirectory(file.path)}</span>}
                        </span>
                        <span className="shrink-0 pt-0.5 text-[11px] tabular-nums text-muted-foreground">{formatBytes(file.size_bytes)}</span>
                        {isManagedMemoryPath(file.path) && <span className="flex shrink-0 items-center gap-1 pt-0.5 text-[10px] text-muted-foreground"><LockKeyhole className="size-3" />只读</span>}
                      </button>
                    ))}
                  </div>
                </nav>
                <section ref={readingPaneRef} aria-label="记忆正文" tabIndex={0} className="min-h-0 min-w-0 overflow-y-auto overscroll-contain bg-background p-4 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring sm:p-6 lg:p-8">
                  <div className="mx-auto w-full max-w-3xl">
                    <div className="mb-6 flex min-w-0 flex-wrap items-start justify-between gap-4 border-b border-border/70 pb-4">
                      <div className="min-w-0 flex-1 basis-48">
                        <p className="mb-1 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">当前文件</p>
                        <h2 className="break-all text-base font-semibold leading-6 text-foreground">{selected?.path}</h2>
                        {selected?.modified_at && <p className="mt-1 text-xs text-muted-foreground">修改于 {formatUpdatedAt(selected.modified_at)}</p>}
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        <span className="rounded-full bg-background px-2.5 py-1 text-xs tabular-nums text-muted-foreground shadow-sm">{formatBytes(selected?.size_bytes ?? 0)}</span>
                        {selectedProfileDimension && <span className="rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground">画像资料</span>}
                        {selected && isManagedMemoryPath(selected.path) && <span className="flex items-center gap-1 rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground"><LockKeyhole className="size-3" />只读</span>}
                        {canEdit && !editing && <Button variant="outline" size="sm" onClick={() => { setDraftContent(selected?.content ?? ''); setEditing(true) }}><Pencil />编辑</Button>}
                        {canEdit && editing && <Button size="sm" disabled={saving} onClick={() => void save()}>{saving ? <Loader2 className="animate-spin" /> : <Save />}保存</Button>}
                      </div>
                    </div>
                    {saveError && <p role="alert" className="mb-4 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">{saveError}</p>}
                    {query.data?.partial && <p className="mb-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"><span className="font-medium">内容已按安全限制截断</span><span className="ml-1">仅展示可读取部分。</span></p>}
                    {selected?.truncated && <p className="mb-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"><span className="font-medium">此文件已截断</span><span className="ml-1">仅展示可读取部分。</span></p>}
                    {selectedProfileDimension && selectedDimension && profile ? <ProfileDimensionCard
                      label={dimensionLabels[selectedProfileDimension]}
                      dimension={selectedDimension}
                      text={profileTexts[selectedProfileDimension] ?? JSON.stringify(selectedDimension.content, null, 2)}
                      error={profileErrors[selectedProfileDimension]}
                      disabled={analysisRunning || dimensionMutation.isPending || profileQuery.isError}
                      onTextChange={(text) => {
                        setProfileTexts((current) => ({ ...current, [selectedProfileDimension]: text }))
                        setDirtyDimensions((current) => ({ ...current, [selectedProfileDimension]: true }))
                        setProfileErrors((current) => ({ ...current, [selectedProfileDimension]: '' }))
                      }}
                      onSave={() => saveDimension(selectedProfileDimension)}
                    /> : editing ? <textarea aria-label="编辑记忆文件" value={draftContent} onChange={(event) => setDraftContent(event.target.value)} className="min-h-[50vh] w-full resize-y rounded-md border bg-background p-4 font-mono text-sm leading-6 outline-none focus-visible:ring-2 focus-visible:ring-ring" /> : <article className="prose prose-sm max-w-3xl break-words leading-7 dark:prose-invert prose-headings:scroll-mt-6 prose-headings:font-semibold prose-a:break-all prose-code:break-words prose-pre:max-w-full prose-pre:overflow-x-auto prose-table:block prose-table:overflow-x-auto">
                      <Markdown
                        remarkPlugins={[remarkGfm]}
                        skipHtml
                        components={{
                          img: ({ alt }) => <span className="inline-flex items-center gap-1 text-muted-foreground"><ImageOff className="size-4" />{alt || '远程图片已禁用'}</span>,
                        }}
                      >
                        {selected?.content ?? ''}
                      </Markdown>
                    </article>}
                  </div>
                </section>
              </div>
            )}
          </section>
          {profileSupported && <section aria-label="项目画像" className="border-t bg-muted/10 p-4 sm:p-6">
            <div className="mx-auto w-full max-w-3xl space-y-0">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border/70 pb-4">
                <div className="min-w-0">
            <p className={`text-sm font-medium ${analysisFailed ? 'text-destructive' : analysisRunning ? 'text-sky-700' : 'text-foreground'}`} role={analysisRunning ? 'status' : undefined}>
                    {analysisRunning ? '画像分析中' : analysisFailed ? '画像分析失败' : profile?.initialization_status === 'ready' ? `画像已应用 · v${profile.version}` : '画像待配置'}
                  </p>
                  {profile?.last_error && analysisFailed && <p className="mt-1 text-xs text-destructive">{profile.last_error}</p>}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {profile && <Button
                    variant="outline"
                    size="sm"
                    disabled={analysisRunning || analysisMutation.isPending || profileQuery.isError}
                    onClick={() => { setProfileActionError(''); analysisMutation.mutate({ retry: analysisFailed, revision: profile.version }) }}
                  >
                    {analysisMutation.isPending ? <Loader2 className="animate-spin" /> : analysisFailed ? <RotateCcw /> : <Sparkles />}
                    {analysisFailed ? '重试分析' : profile.initialization_status === 'ready' ? '重新分析' : '开始分析'}
                  </Button>}
                </div>
              </div>

              {profileActionError && <p role="alert" className="text-sm text-destructive">{profileActionError}</p>}
              {profileQuery.isPending && !profile && <p role="status" className="flex items-center gap-2 py-8 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在读取画像</p>}
              {profileQuery.isError && <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"><span>{getApiErrorMessage(profileQuery.error, '画像资料加载失败')}</span><Button variant="outline" size="sm" onClick={() => void profileQuery.refetch()}>重试</Button></div>}
              {analysisRunning && <p className="text-xs text-sky-700">画像分析完成后会自动刷新，当前画像继续用于创作。</p>}
              {analysisFailed && <p role="alert" className="text-xs text-destructive">这次分析未完成，当前画像保持不变，可以重试。</p>}

              {profile && selectedProfileDimension && profileErrors[selectedProfileDimension]?.includes('未保存的修改已保留') && <Button className="mt-3" variant="outline" size="sm" disabled={profileQuery.isFetching} onClick={() => loadLatestDimension(selectedProfileDimension)}>载入最新版本</Button>}
              {profile && !hasProfileFiles && <div className="mt-4 space-y-0">
                <p className="mb-3 text-xs text-muted-foreground">画像文件尚未投影到项目记忆，先在这里补充；同步后可直接从左侧 `profile/*.md` 文件查看。</p>
                {Object.entries(profile.dimensions).map(([name, dimension]) => (
                  <div key={name} className="min-w-0">
                    <ProfileDimensionCard
                      label={dimensionLabels[name] ?? name}
                      dimension={dimension}
                      text={profileTexts[name] ?? JSON.stringify(dimension.content, null, 2)}
                      error={profileErrors[name]}
                      disabled={analysisRunning || dimensionMutation.isPending || profileQuery.isError}
                      onTextChange={(text) => {
                        setProfileTexts((current) => ({ ...current, [name]: text }))
                        setDirtyDimensions((current) => ({ ...current, [name]: true }))
                        setProfileErrors((current) => ({ ...current, [name]: '' }))
                      }}
                      onSave={() => saveDimension(name)}
                    />
                    {profileErrors[name]?.includes('未保存的修改已保留') && <Button className="mt-2" variant="outline" size="sm" disabled={profileQuery.isFetching} onClick={() => loadLatestDimension(name)}>载入最新版本</Button>}
                  </div>
                ))}
              </div>}

              {profile?.follow_up_questions.length ? <details className="border-t border-border pt-3 text-sm">
                <summary className="cursor-pointer font-medium">待确认问题 <span className="text-muted-foreground">{profile.follow_up_questions.length}</span></summary>
                <ul className="mt-2 space-y-2 text-muted-foreground">{profile.follow_up_questions.map((question) => <li key={question}>{question}</li>)}</ul>
              </details> : null}
            </div>
          </section>}
        </div>
      </DialogContent>
    </Dialog>
  )
}
