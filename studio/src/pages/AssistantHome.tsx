import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { ChevronDown, Loader2, Send, Sparkles } from 'lucide-react'
import { api } from '@/lib/api'
import type { AssistantModeID } from '@/lib/api/assistant'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { assistantMessageHandoffKey, saveAssistantMessageHandoff } from '@/lib/assistant-message-handoff'

const MODE_ORDER: AssistantModeID[] = ['profile', 'creation', 'review']

function isAssistantModeID(value: string | null): value is AssistantModeID {
  return value === 'profile' || value === 'creation' || value === 'review'
}

function personaLabel(projectName?: string) {
  const name = projectName?.trim()
  if (!name) return '新人设'
  return name.endsWith('人设') ? name : `${name}人设`
}

function titleForMode(modeId: AssistantModeID, personaName: string, contentTitle: string) {
  if (modeId === 'profile') return `开始为「${personaName}」建立画像`
  if (modeId === 'creation') return `为「${personaName}」制定新的创作计划`
  return `复盘「${personaName}」的作品${contentTitle ? `：${contentTitle}` : ''}`
}

function placeholderForMode(modeId: AssistantModeID) {
  if (modeId === 'profile') return '告诉我你想建立怎样的画像…'
  if (modeId === 'creation') return '描述你想创作的内容，或告诉我你的目标…'
  return '告诉我你想从这件作品中复盘什么…'
}

export default function AssistantHome() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const { user } = useAuth()
  const bootstrapQuery = useQuery({ queryKey: ['assistant-bootstrap'], queryFn: api.assistant.bootstrap, staleTime: 30_000 })
  const bootstrap = bootstrapQuery.data
  const [userSelectedMode, setUserSelectedMode] = useState<AssistantModeID | null>(null)
  const [selectedProjectId, setSelectedProjectId] = useState(params.get('project_id') ?? '')
  const [selectedContentId, setSelectedContentId] = useState(params.get('content_id') ?? '')
  const [composer, setComposer] = useState('')
  const [modeNotice, setModeNotice] = useState<string | null>(null)
  const requestedMode = params.get('mode')
  const modeId = userSelectedMode ?? (isAssistantModeID(requestedMode) ? requestedMode : bootstrap?.defaultModeId ?? 'profile')
  const mode = bootstrap?.modes.find((candidate) => candidate.id === modeId)
  const projects = bootstrap?.projects ?? []
  const selectableProjects = modeId === 'profile' ? projects : projects.filter((project) => project.profileReady)
  const hiddenUnconfirmedProjectCount = modeId === 'profile' ? 0 : projects.length - selectableProjects.length
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
  const heading = titleForMode(modeId, personaLabel(selectedProject?.name), content?.title ?? '')
  const canSend = Boolean(mode?.available) && (modeId === 'profile' || Boolean(selectedProject)) && (modeId !== 'review' || Boolean(content)) && Boolean(composer.trim())
  const checkpointQuery = useQuery({
    queryKey: ['assistant-checkpoint', modeId, selectedProject?.id ?? '', modeId === 'review' ? content?.id ?? '' : ''],
    queryFn: () => api.assistant.latestCheckpoint({ mode_id: modeId, ...(selectedProject ? { project_id: selectedProject.id } : {}), ...(modeId === 'review' && content ? { content_id: content.id } : {}) }),
    enabled: Boolean(mode?.available && (modeId === 'profile' || selectedProject) && (modeId !== 'review' || content)),
    staleTime: 30_000,
  })
  const checkpoint = checkpointQuery.data

  const chooseMode = (id: AssistantModeID) => {
    const nextMode = bootstrap?.modes.find((candidate) => candidate.id === id)
    setModeNotice(nextMode?.available ? null : nextMode?.disabledReason ?? '该模式当前不可用。')
    setUserSelectedMode(id)
    setSelectedContentId('')
  }

  const send = () => {
    const message = composer.trim()
    if (!canSend || !message) return
    const key = assistantMessageHandoffKey(user?.id ?? 'signed-out', modeId, selectedProject?.id, modeId === 'review' ? content?.id : undefined)
    saveAssistantMessageHandoff(key, message)
    const query = new URLSearchParams({ assistant_session: '1', mode: modeId, handoff: '1' })
    if (selectedProject) query.set('project_id', selectedProject.id)
    if (modeId === 'review' && content) query.set('content_id', content.id)
    navigate(`/chat?${query.toString()}`)
  }

  if (bootstrapQuery.isError) return <main className="mx-auto flex min-h-[calc(100dvh-4rem)] max-w-5xl items-center px-5"><div role="alert" className="max-w-lg"><h1 className="text-lg font-semibold">AI 助手暂时无法加载</h1><p className="mt-2 text-sm text-muted-foreground">{bootstrapQuery.error instanceof Error ? bootstrapQuery.error.message : '请稍后重试。'}</p><Button className="mt-4" variant="outline" onClick={() => void bootstrapQuery.refetch()}>重新加载</Button></div></main>

  return <main className="mx-auto flex min-h-[calc(100dvh-4rem)] w-full max-w-5xl flex-col px-4 pb-8 sm:px-6">
    <header className="flex h-14 items-center gap-2 border-b border-border/70"><span className="flex size-7 items-center justify-center rounded-lg bg-primary/10 text-primary"><Sparkles className="size-4" /></span><h1 className="text-sm font-semibold">AI 助手</h1></header>
    {bootstrapQuery.isLoading ? <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground"><Loader2 className="mr-2 size-4 animate-spin" />正在加载</div> : <div className="flex flex-1 flex-col items-center justify-center py-10 sm:py-16"><div className="w-full max-w-3xl">
      <nav className="mb-12 flex items-center justify-center gap-1" aria-label="助手模式">{MODE_ORDER.map((id) => { const item = bootstrap?.modes.find((candidate) => candidate.id === id); if (!item) return null; const selected = modeId === id; return <button key={id} type="button" aria-pressed={selected} aria-disabled={!item.available} title={item.available ? item.description : item.disabledReason ?? item.description} onClick={() => chooseMode(id)} className={`rounded-full px-4 py-2 text-sm transition-colors ${selected ? 'bg-foreground text-background' : 'text-muted-foreground hover:bg-muted hover:text-foreground'} ${!item.available ? 'opacity-50' : ''}`}><span>{item.displayName}</span>{!item.available && <span className="sr-only">{item.disabledReason ?? '当前不可用'}</span>}</button> })}</nav>
      <section className="text-center"><p className="text-sm font-medium text-primary">{mode?.displayName}</p><h2 className="mt-3 text-2xl font-semibold tracking-tight sm:text-3xl">{heading}</h2><p className="mx-auto mt-3 max-w-xl text-sm text-muted-foreground">{mode?.description}</p></section>
      <section className="mt-8 overflow-hidden rounded-2xl border border-border bg-background shadow-sm focus-within:border-ring focus-within:ring-4 focus-within:ring-ring/10"><Textarea value={composer} onChange={(event) => setComposer(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); send() } }} placeholder={placeholderForMode(modeId)} aria-label="发送消息" className="min-h-32 resize-none border-0 bg-transparent px-5 py-4 text-base shadow-none focus-visible:ring-0" /><div className="flex flex-wrap items-center gap-2 border-t border-border/70 px-3 py-2.5">
        <label className="sr-only" htmlFor="assistant-project">选择画像</label><NativeSelect id="assistant-project" aria-label="选择画像" value={selectedProject?.id ?? (modeId === 'profile' ? selectedProjectId : '')} onChange={(event) => { setSelectedProjectId(event.target.value); setSelectedContentId('') }}>{modeId === 'profile' ? <NativeSelectOption value="">新建画像</NativeSelectOption> : <NativeSelectOption value="" disabled>选择画像</NativeSelectOption>}{selectableProjects.map((item) => <NativeSelectOption key={item.id} value={item.id}>{item.name}</NativeSelectOption>)}</NativeSelect>
        {modeId === 'review' && <><span className="text-muted-foreground">/</span><label className="sr-only" htmlFor="assistant-content">选择作品</label><NativeSelect id="assistant-content" aria-label="选择作品" value={content?.id ?? ''} onChange={(event) => setSelectedContentId(event.target.value)} disabled={worksQuery.isLoading || works.length === 0}><NativeSelectOption value="" disabled>选择作品</NativeSelectOption>{works.map((work) => <NativeSelectOption key={work.id} value={work.id}>{work.title}</NativeSelectOption>)}</NativeSelect>{worksQuery.hasNextPage && <Button type="button" variant="ghost" size="icon-sm" aria-label="加载更多作品" title="加载更多作品" disabled={worksQuery.isFetchingNextPage} onClick={() => void worksQuery.fetchNextPage()}><ChevronDown className="size-4" /></Button>}</>}
        <div className="ml-auto flex items-center gap-2"><span className="hidden text-xs text-muted-foreground sm:inline">Enter 发送 · Shift + Enter 换行</span><Button size="icon-md" onClick={send} disabled={!canSend} aria-label="发送消息" title="发送消息"><Send className="size-4" /></Button></div>
      </div></section>
      {modeNotice && <p role="status" className="mt-3 text-center text-xs text-muted-foreground">{modeNotice}</p>}{hiddenUnconfirmedProjectCount > 0 && mode?.available && <p className="mt-3 text-center text-xs text-muted-foreground">创作和复盘模式只显示已确认的人设；未显示的人设请先在画像模式完成确认。</p>}{modeId === 'review' && selectedProject && works.length === 0 && mode?.available && !worksQuery.isLoading && !worksQuery.isError && <p className="mt-3 text-center text-xs text-muted-foreground">这个画像下还没有可复盘的已完成作品。</p>}
      {checkpoint && mode?.available && <button type="button" aria-label="继续草稿" className="mx-auto mt-5 block text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline" onClick={() => { const query = new URLSearchParams({ assistant_session: '1', mode: modeId, checkpoint_id: checkpoint.id }); if (selectedProject) query.set('project_id', selectedProject.id); if (modeId === 'review' && content) query.set('content_id', content.id); navigate(`/chat?${query.toString()}`) }}>继续上次草稿 · {checkpoint.summary}</button>}
    </div></div>}
  </main>
}
