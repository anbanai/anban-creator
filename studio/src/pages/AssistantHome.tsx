import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { ChevronDown, Folder, Loader2, Send, ShieldCheck } from 'lucide-react'
import { api } from '@/lib/api'
import type { AssistantModeID } from '@/lib/api/assistant'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { assistantMessageHandoffKey, saveAssistantMessageHandoff } from '@/lib/assistant-message-handoff'

const MODE_ORDER: AssistantModeID[] = ['profile', 'creation', 'review']

function isAssistantModeID(value: string | null): value is AssistantModeID {
  return value === 'profile' || value === 'creation' || value === 'review'
}

function ModePicker({
  modeId,
  modes,
  open,
  onOpenChange,
  onChoose,
}: {
  modeId: AssistantModeID
  modes: Array<{ id: AssistantModeID; displayName: string; description: string; available: boolean; disabledReason?: string }>
  open: boolean
  onOpenChange: (open: boolean) => void
  onChoose: (id: AssistantModeID) => void
}) {
  const mode = modes.find((candidate) => candidate.id === modeId)
  return <DropdownMenu open={open} onOpenChange={onOpenChange}>
    <DropdownMenuTrigger render={<button type="button" aria-label="选择模式" className="inline-flex h-9 items-center gap-2 rounded-lg px-2 text-[15px] font-medium text-foreground transition-colors hover:bg-muted focus-visible:bg-muted" />}>
      <ShieldCheck className="size-[18px] text-foreground/80" aria-hidden="true" />
      <span>{mode?.displayName ?? '画像模式'}</span>
      <ChevronDown className="size-4 text-muted-foreground" aria-hidden="true" />
    </DropdownMenuTrigger>
    <DropdownMenuContent align="start" className="w-64">
      {MODE_ORDER.map((id) => {
        const item = modes.find((candidate) => candidate.id === id)
        if (!item) return null
        const selected = item.id === modeId
        return <DropdownMenuItem key={item.id} aria-disabled={!item.available} onClick={() => onChoose(item.id)} className="items-start gap-3 py-2.5">
          <span className="min-w-0 flex-1"><span className="block font-medium">{item.displayName}</span><span className="mt-0.5 block whitespace-normal text-xs text-muted-foreground">{item.available ? item.description : item.disabledReason ?? item.description}</span></span>
          {selected && <span aria-hidden="true" className="pt-0.5 text-xs text-muted-foreground">当前</span>}
          {!item.available && <span className="shrink-0 pt-0.5 text-xs text-muted-foreground">未开放</span>}
        </DropdownMenuItem>
      })}
    </DropdownMenuContent>
  </DropdownMenu>
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
  const [modeMenuOpen, setModeMenuOpen] = useState(false)
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
    if (!nextMode?.available) {
      setModeNotice(nextMode?.disabledReason ?? '该模式当前不可用。')
      setModeMenuOpen(false)
      return
    }
    setModeNotice(nextMode?.available ? null : nextMode?.disabledReason ?? '该模式当前不可用。')
    setUserSelectedMode(id)
    setSelectedContentId('')
    setModeMenuOpen(false)
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

  if (bootstrapQuery.isError) return <main aria-label="AI 助手" className="flex min-h-full items-center justify-center px-5"><div role="alert" className="max-w-lg"><h1 className="text-lg font-semibold">AI 助手暂时无法加载</h1><p className="mt-2 text-sm text-muted-foreground">{bootstrapQuery.error instanceof Error ? bootstrapQuery.error.message : '请稍后重试。'}</p><Button className="mt-4" variant="outline" onClick={() => void bootstrapQuery.refetch()}>重新加载</Button></div></main>

  return <main aria-label="AI 助手" className="flex min-h-full w-full items-center justify-center px-4 py-12 sm:px-8">
    {bootstrapQuery.isLoading ? <div className="flex items-center justify-center text-sm text-muted-foreground"><Loader2 className="mr-2 size-4 animate-spin" />正在加载</div> : <div className="w-full max-w-[850px]">
      <header className="mb-9 flex items-center justify-center gap-3">
        <img src="/dsh-whale-mark.svg" alt="" aria-hidden="true" className="size-10 grayscale brightness-0" />
        <h1 className="text-[2.25rem] font-semibold tracking-tight text-foreground sm:text-[2.5rem]">探索未至之境</h1>
        <span className="rounded-full bg-[#e7edfc] px-2.5 py-1 text-sm font-medium text-[#38558d]">预览版</span>
      </header>

      <div className="mb-4 flex flex-wrap items-center gap-5 px-2">
        <div className="flex items-center gap-1.5">
          <Folder className="size-[19px] text-foreground/80" aria-hidden="true" />
          <label className="sr-only" htmlFor="assistant-project">选择画像</label>
          <NativeSelect id="assistant-project" aria-label="选择画像" size="sm" value={selectedProject?.id ?? (modeId === 'profile' ? selectedProjectId : '')} onChange={(event) => { setSelectedProjectId(event.target.value); setSelectedContentId('') }} className="[&_select]:h-9 [&_select]:rounded-lg [&_select]:border-0 [&_select]:bg-transparent [&_select]:px-1 [&_select]:pr-7 [&_select]:text-[15px] [&_select]:font-medium [&_select]:shadow-none [&_svg]:right-1">
            {modeId === 'profile' ? <NativeSelectOption value="">新建画像</NativeSelectOption> : <NativeSelectOption value="" disabled>选择画像</NativeSelectOption>}
            {selectableProjects.map((item) => <NativeSelectOption key={item.id} value={item.id}>{item.name}</NativeSelectOption>)}
          </NativeSelect>
        </div>
        <ModePicker modeId={modeId} modes={bootstrap?.modes ?? []} open={modeMenuOpen} onOpenChange={setModeMenuOpen} onChoose={chooseMode} />
        {modeId === 'review' && <div className="flex items-center gap-1.5">
          <label className="sr-only" htmlFor="assistant-content">选择作品</label>
          <NativeSelect id="assistant-content" aria-label="选择作品" size="sm" value={content?.id ?? ''} onChange={(event) => setSelectedContentId(event.target.value)} disabled={worksQuery.isLoading || works.length === 0} className="[&_select]:h-9 [&_select]:rounded-lg [&_select]:border-0 [&_select]:bg-transparent [&_select]:px-1 [&_select]:pr-7 [&_select]:text-[15px] [&_select]:font-medium [&_select]:shadow-none">
            <NativeSelectOption value="" disabled>选择作品</NativeSelectOption>
            {works.map((work) => <NativeSelectOption key={work.id} value={work.id}>{work.title}</NativeSelectOption>)}
          </NativeSelect>
          {worksQuery.hasNextPage && <Button type="button" variant="ghost" size="icon-sm" aria-label="加载更多作品" title="加载更多作品" disabled={worksQuery.isFetchingNextPage} onClick={() => void worksQuery.fetchNextPage()}><ChevronDown className="size-4" /></Button>}
        </div>}
      </div>

      <section className="overflow-hidden rounded-[28px] border border-border/80 bg-background shadow-[0_8px_30px_rgba(15,23,42,0.06)] transition-colors focus-within:border-ring/60 focus-within:ring-4 focus-within:ring-ring/10">
        <Textarea value={composer} onChange={(event) => setComposer(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); send() } }} placeholder="描述你想要构建的内容，/ 调用指令，@ 文件或对话" aria-label="发送消息" className="min-h-[128px] resize-none rounded-none border-0 bg-transparent px-6 py-5 text-base shadow-none placeholder:text-muted-foreground/50 focus-visible:ring-0 sm:text-[17px]" />
        <div className="flex min-h-16 items-center justify-end px-3 pb-3 pt-1 sm:px-4">
          <Button type="button" size="icon-md" onClick={send} disabled={!canSend} aria-label="发送消息" title="发送消息" className="rounded-full bg-[#b6c8f5] text-white hover:bg-[#9eb5ee] disabled:bg-[#d7e0f8] disabled:text-white"><Send className="size-5" /></Button>
        </div>
      </section>

      {modeNotice && <p role="status" className="mt-3 text-center text-xs text-muted-foreground">{modeNotice}</p>}
      {hiddenUnconfirmedProjectCount > 0 && mode?.available && <p className="mt-3 text-center text-xs text-muted-foreground">创作和复盘模式只显示已确认的人设；未显示的人设请先在画像模式完成确认。</p>}
      {modeId === 'review' && selectedProject && works.length === 0 && mode?.available && !worksQuery.isLoading && !worksQuery.isError && <p className="mt-3 text-center text-xs text-muted-foreground">这个画像下还没有可复盘的已完成作品。</p>}
      {checkpoint && mode?.available && <button type="button" aria-label="继续草稿" className="mx-auto mt-5 block text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline" onClick={() => { const query = new URLSearchParams({ assistant_session: '1', mode: modeId, checkpoint_id: checkpoint.id }); if (selectedProject) query.set('project_id', selectedProject.id); if (modeId === 'review' && content) query.set('content_id', content.id); navigate(`/chat?${query.toString()}`) }}>继续上次草稿 · {checkpoint.summary}</button>}
    </div>}
  </main>
}
