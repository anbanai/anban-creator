import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { ArrowRight, ChevronDown, Loader2, MessageSquareText, RotateCcw, Sparkles } from 'lucide-react'
import { api } from '@/lib/api'
import type { AssistantModeID } from '@/lib/api/assistant'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

const MODE_ORDER: AssistantModeID[] = ['profile', 'creation', 'review']

function isAssistantModeID(value: string | null): value is AssistantModeID {
  return value === 'profile' || value === 'creation' || value === 'review'
}

function titleForMode(modeId: AssistantModeID, projectName: string, contentTitle: string) {
  if (modeId === 'profile') return `开始为「${projectName}」建立创作画像`
  if (modeId === 'creation') return `为「${projectName}」制定新的创作计划`
  return `复盘「${projectName}」的作品${contentTitle ? `：${contentTitle}` : ''}`
}

export default function AssistantHome() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const bootstrapQuery = useQuery({
    queryKey: ['assistant-bootstrap'],
    queryFn: api.assistant.bootstrap,
    staleTime: 30_000,
  })
  const bootstrap = bootstrapQuery.data
  const [userSelectedMode, setUserSelectedMode] = useState<AssistantModeID | null>(null)
  const [selectedProjectId, setSelectedProjectId] = useState(params.get('project_id') ?? '')
  const [selectedContentId, setSelectedContentId] = useState(params.get('content_id') ?? '')
  const requestedMode = params.get('mode')
  const modeId = userSelectedMode ?? (isAssistantModeID(requestedMode) ? requestedMode : bootstrap?.defaultModeId ?? 'profile')
  const mode = bootstrap?.modes.find((candidate) => candidate.id === modeId)
  const projects = bootstrap?.projects ?? []
  const selectableProjects = modeId === 'profile' ? projects : projects.filter((project) => project.profileReady)
  const selectedProject = selectableProjects.find((project) => project.id === selectedProjectId)
    ?? (modeId !== 'profile' && selectableProjects.length === 1 ? selectableProjects[0] : undefined)
  const worksQuery = useInfiniteQuery({
    queryKey: ['assistant-works', selectedProject?.id],
    initialPageParam: undefined as { created_at: string; id: string } | undefined,
    queryFn: ({ pageParam }) => api.assistant.works(selectedProject!.id, pageParam, 25),
    getNextPageParam: (lastPage) => lastPage.has_more ? lastPage.next_cursor : undefined,
    enabled: modeId === 'review' && Boolean(mode?.available && selectedProject),
    staleTime: 30_000,
  })
  const works = worksQuery.data?.pages.flatMap((page) => page.items) ?? []
  const content = works.find((work) => work.id === selectedContentId) ?? (modeId === 'review' && works.length === 1 ? works[0] : undefined)
  const heading = titleForMode(modeId, selectedProject?.name ?? (modeId === 'profile' ? '新画像' : '你的画像'), content?.title ?? '')
  const canStart = Boolean(mode?.available)
    && (modeId === 'profile' || Boolean(selectedProject))
    && (modeId !== 'review' || Boolean(content))
  const checkpointQuery = useQuery({
    queryKey: ['assistant-checkpoint', modeId, selectedProject?.id ?? '', modeId === 'review' ? content?.id ?? '' : ''],
    queryFn: () => api.assistant.latestCheckpoint({
      mode_id: modeId,
      ...(selectedProject ? { project_id: selectedProject.id } : {}),
      ...(modeId === 'review' && content ? { content_id: content.id } : {}),
    }),
    enabled: Boolean(mode?.available && (modeId === 'profile' || selectedProject) && (modeId !== 'review' || content)),
    staleTime: 30_000,
  })
  const checkpoint = checkpointQuery.data
  const loading = bootstrapQuery.isLoading

  const start = (checkpointId?: string) => {
    if (!canStart) return
    const query = new URLSearchParams({ assistant_session: '1', mode: modeId })
    if (selectedProject) query.set('project_id', selectedProject.id)
    if (modeId === 'review' && content) query.set('content_id', content.id)
    if (checkpointId) query.set('checkpoint_id', checkpointId)
    navigate(`/chat?${query.toString()}`)
  }

  if (bootstrapQuery.isError) {
    return <main className="mx-auto flex min-h-[calc(100dvh-4rem)] max-w-5xl items-center px-5"><div role="alert" className="max-w-lg"><h1 className="text-lg font-semibold">AI 助手暂时无法加载</h1><p className="mt-2 text-sm text-muted-foreground">{bootstrapQuery.error instanceof Error ? bootstrapQuery.error.message : '请稍后重试。'}</p><Button className="mt-4" variant="outline" onClick={() => void bootstrapQuery.refetch()}>重新加载</Button></div></main>
  }

  return (
    <main className="mx-auto flex min-h-[calc(100dvh-4rem)] w-full max-w-6xl flex-col px-4 pb-8 pt-4 sm:px-6">
      <header className="flex min-h-12 items-center gap-3 border-b border-border pb-3">
        <span className="flex size-8 items-center justify-center rounded-lg bg-primary/10 text-primary"><Sparkles className="size-4" /></span>
        <h1 className="text-base font-semibold">AI 助手</h1>
      </header>

      {loading ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground"><Loader2 className="mr-2 size-4 animate-spin" />正在加载</div>
      ) : (
        <div className="mx-auto flex w-full max-w-4xl flex-1 flex-col">
          <nav className="grid grid-cols-3 gap-2 border-b border-border py-4" aria-label="模式选择">
            {MODE_ORDER.map((id) => {
              const item = bootstrap?.modes.find((candidate) => candidate.id === id)
              if (!item) return null
              const selected = modeId === id
              return (
                <button
                  key={id}
                  type="button"
                  aria-pressed={selected}
                  aria-disabled={!item.available}
                  onClick={() => setUserSelectedMode(id)}
                  className={`min-h-16 rounded-lg border px-3 py-2 text-left transition-colors ${!item.available ? 'opacity-60 ' : ''}${selected ? 'border-primary bg-primary/[0.06] text-foreground' : 'border-border bg-background hover:bg-muted/60'}`}
                >
                  <span className="block text-sm font-medium">{item.displayName}</span>
                  <span className="mt-1 block text-xs leading-4 text-muted-foreground">{item.available ? item.description : item.disabledReason ?? item.description}</span>
                </button>
              )
            })}
          </nav>

          <section className="flex flex-1 flex-col justify-center py-8 sm:py-12">
            <div className="max-w-2xl">
              <p className="text-sm font-medium text-primary">{mode?.displayName}</p>
              <h2 className="mt-3 text-2xl font-semibold leading-tight sm:text-3xl">{heading}</h2>
            </div>

            <div className="mt-8 grid gap-4 sm:grid-cols-2">
              <label className="grid gap-2 text-sm font-medium">
                画像
                <NativeSelect
                  className="w-full"
                  aria-label="选择画像"
                  value={selectedProject?.id ?? (modeId === 'profile' ? selectedProjectId : '')}
                  onChange={(event) => {
                    setSelectedProjectId(event.target.value)
                    setSelectedContentId('')
                  }}
                >
                  {modeId === 'profile' ? <NativeSelectOption value="">新建画像</NativeSelectOption> : <NativeSelectOption value="" disabled>选择一个画像</NativeSelectOption>}
                  {selectableProjects.map((item) => <NativeSelectOption key={item.id} value={item.id}>{item.name}</NativeSelectOption>)}
                </NativeSelect>
              </label>
              {modeId === 'review' && (
                <label className="grid gap-2 text-sm font-medium">
                  作品
                  <NativeSelect
                    className="w-full"
                    aria-label="选择作品"
                    value={content?.id ?? ''}
                    onChange={(event) => setSelectedContentId(event.target.value)}
                    disabled={worksQuery.isLoading || works.length === 0}
                  >
                    <NativeSelectOption value="" disabled>选择一件已完成作品</NativeSelectOption>
                    {works.map((work) => <NativeSelectOption key={work.id} value={work.id}>{work.title}</NativeSelectOption>)}
                  </NativeSelect>
                  {worksQuery.hasNextPage && (
                    <Button type="button" variant="ghost" size="sm" className="w-fit" disabled={worksQuery.isFetchingNextPage} onClick={() => void worksQuery.fetchNextPage()}>
                      {worksQuery.isFetchingNextPage ? <Loader2 className="size-4 animate-spin" /> : <ChevronDown className="size-4" />}
                      {worksQuery.isFetchingNextPage ? '正在加载' : '加载更多作品'}
                    </Button>
                  )}
                  {worksQuery.isError && <p role="alert" className="text-xs text-destructive">作品列表暂时无法加载，请重试。</p>}
                </label>
              )}
            </div>

            {!mode?.available && mode?.disabledReason && <p className="mt-4 max-w-xl text-sm text-muted-foreground">{mode.disabledReason}</p>}
            {modeId !== 'profile' && !selectedProject && mode?.available && <p className="mt-4 text-sm text-muted-foreground">先选择一个画像，再开始当前模式。</p>}
            {modeId === 'review' && selectedProject && works.length === 0 && mode?.available && !worksQuery.isLoading && !worksQuery.isError && <p className="mt-4 text-sm text-muted-foreground">这个画像下还没有可复盘的已完成作品。</p>}

            <div className="mt-8 flex flex-wrap items-center gap-3">
              <Button size="lg" disabled={!canStart} onClick={() => start()}>
                <MessageSquareText className="size-4" />开始对话<ArrowRight className="size-4" />
              </Button>
              {checkpoint && mode?.available && (
                <Button variant="outline" size="lg" onClick={() => start(checkpoint.id)}>
                  <RotateCcw className="size-4" />
                  继续草稿
                </Button>
              )}
            </div>
            {checkpoint && <p className="mt-3 max-w-xl text-xs text-muted-foreground">上次保存：{checkpoint.summary}</p>}
          </section>
        </div>
      )}
    </main>
  )
}
