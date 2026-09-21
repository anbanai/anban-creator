import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Brain, FileText, ImageOff, Loader2, RefreshCw } from 'lucide-react'

import { projectsApi } from '@/lib/api/projects'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'

interface ProjectMemorySheetProps {
  projectId: string
  projectName: string
  open: boolean
  onOpenChange: (open: boolean) => void
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

export function ProjectMemorySheet({ projectId, projectName, open, onOpenChange }: ProjectMemorySheetProps) {
  const [selectedPath, setSelectedPath] = useState('')
  const query = useQuery({
    queryKey: ['project-memory', projectId],
    queryFn: () => projectsApi.memory(projectId),
    enabled: open,
    staleTime: 0,
  })
  const files = query.data?.files ?? []

  useEffect(() => {
    if (!open || files.length === 0) return
    if (!files.some((file) => file.path === selectedPath)) {
      setSelectedPath(files.find((file) => file.path === 'MEMORY.md')?.path ?? files[0].path)
    }
  }, [files, open, selectedPath])

  const selected = files.find((file) => file.path === selectedPath) ?? files[0]

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="w-full gap-0 p-0 sm:w-[min(92vw,68rem)] sm:max-w-[min(92vw,68rem)]"
      >
        <SheetHeader className="border-b bg-background/95 pr-14 backdrop-blur">
          <div className="flex min-w-0 items-center gap-2">
            <Brain className="size-4 shrink-0 text-primary" />
            <SheetTitle className="truncate text-lg">项目记忆</SheetTitle>
          </div>
          <SheetDescription className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
            <span className="font-medium text-foreground/80">{projectName}</span>
            <span aria-hidden="true">·</span>
            <span>最近更新 {formatUpdatedAt(query.data?.updated_at ?? null)}</span>
          </SheetDescription>
          <p className="text-xs text-muted-foreground">项目记忆文件</p>
          <Button
            variant="outline"
            size="icon-sm"
            className="absolute right-12 top-3"
            aria-label="刷新记忆"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <RefreshCw className={query.isFetching ? 'animate-spin' : ''} />
          </Button>
        </SheetHeader>

        {query.isLoading ? (
          <div className="flex min-h-0 flex-1 items-center justify-center text-muted-foreground"><Loader2 className="mr-2 size-4 animate-spin" />正在读取记忆</div>
        ) : query.isError ? (
          <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
            <p className="text-sm font-medium">无法读取项目记忆</p>
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}><RefreshCw />重试</Button>
          </div>
        ) : query.data?.status === 'empty' || files.length === 0 ? (
          <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-6 text-center text-muted-foreground">
            <Brain className="size-8" />
            <p className="text-sm font-medium text-foreground">还没有项目记忆</p>
            <p className="max-w-sm text-xs">完成一次创作后，项目会在这里积累可复用的上下文。</p>
          </div>
        ) : (
          <div className="grid min-h-0 min-w-0 flex-1 grid-rows-[auto_minmax(0,1fr)] sm:grid-cols-[15rem_minmax(0,1fr)] sm:grid-rows-1">
            <aside className="min-w-0 overflow-x-auto overflow-y-hidden border-b bg-muted/20 p-3 sm:overflow-x-hidden sm:overflow-y-auto sm:border-b-0 sm:border-r">
              <p className="mb-2 px-2 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">文件</p>
              <div className="flex min-w-max gap-2 sm:block sm:min-w-0">
              {files.map((file) => (
                <button
                  type="button"
                  key={file.path}
                  aria-label={file.path}
                  aria-current={selected?.path === file.path ? 'page' : undefined}
                  onClick={() => setSelectedPath(file.path)}
                  className={`flex min-w-[12rem] items-start gap-2 rounded-lg border px-3 py-2.5 text-left transition-colors hover:bg-background sm:mb-1 sm:min-w-0 ${selected?.path === file.path ? 'border-border bg-background text-foreground shadow-sm' : 'border-transparent text-muted-foreground'}`}
                >
                  <FileText className="mt-0.5 size-4 shrink-0" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-xs font-medium">{fileName(file.path)}</span>
                    {fileDirectory(file.path) && <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">{fileDirectory(file.path)}</span>}
                  </span>
                  <span className="shrink-0 pt-0.5 text-[11px] tabular-nums text-muted-foreground">{formatBytes(file.size_bytes)}</span>
                </button>
              ))}
              </div>
            </aside>
            <main className="min-h-0 min-w-0 overflow-y-auto bg-muted/10 p-4 sm:p-8">
              <div className="mx-auto w-full max-w-3xl">
                <div className="mb-6 flex min-w-0 items-start justify-between gap-4 border-b border-border/70 pb-4">
                  <div className="min-w-0">
                    <p className="mb-1 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">当前文件</p>
                    <h2 className="break-all text-base font-semibold text-foreground">{selected?.path}</h2>
                    {selected?.modified_at && <p className="mt-1 text-xs text-muted-foreground">修改于 {formatUpdatedAt(selected.modified_at)}</p>}
                  </div>
                  <span className="shrink-0 rounded-full bg-background px-2.5 py-1 text-xs tabular-nums text-muted-foreground shadow-sm">{formatBytes(selected?.size_bytes ?? 0)}</span>
                </div>
                {query.data?.partial && <p className="mb-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"><span className="font-medium">内容已按安全限制截断</span><span className="ml-1">仅展示可读取部分。</span></p>}
                {selected?.truncated && <p className="mb-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"><span className="font-medium">此文件已截断</span><span className="ml-1">仅展示可读取部分。</span></p>}
              <article className="prose prose-sm max-w-3xl break-words leading-7 dark:prose-invert prose-headings:scroll-mt-6 prose-headings:font-semibold prose-a:break-all prose-code:break-words prose-pre:max-w-full prose-pre:overflow-x-auto prose-table:block prose-table:overflow-x-auto">
                <Markdown
                  remarkPlugins={[remarkGfm]}
                  skipHtml
                  components={{
                    img: ({ alt }) => <span className="inline-flex items-center gap-1 text-muted-foreground"><ImageOff className="size-4" />{alt || '远程图片已禁用'}</span>,
                  }}
                >
                  {selected?.content ?? ''}
                </Markdown>
              </article>
              </div>
            </main>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
