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
      <SheetContent side="right" className="w-full gap-0 p-0 sm:max-w-4xl">
        <SheetHeader className="border-b pr-14">
          <div className="flex items-center gap-2">
            <Brain className="size-4" />
            <SheetTitle>项目记忆</SheetTitle>
          </div>
          <SheetDescription>{projectName} · {formatUpdatedAt(query.data?.updated_at ?? null)}</SheetDescription>
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
          <div className="flex flex-1 items-center justify-center text-muted-foreground"><Loader2 className="mr-2 size-4 animate-spin" />正在读取记忆</div>
        ) : query.isError ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
            <p className="text-sm font-medium">无法读取项目记忆</p>
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}><RefreshCw />重试</Button>
          </div>
        ) : query.data?.status === 'empty' || files.length === 0 ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center text-muted-foreground">
            <Brain className="size-8" />
            <p className="text-sm font-medium text-foreground">还没有项目记忆</p>
          </div>
        ) : (
          <div className="grid min-h-0 flex-1 grid-cols-1 sm:grid-cols-[14rem_minmax(0,1fr)]">
            <aside className="max-h-44 overflow-y-auto border-b p-2 sm:max-h-none sm:border-b-0 sm:border-r">
              {files.map((file) => (
                <button
                  type="button"
                  key={file.path}
                  aria-label={file.path}
                  onClick={() => setSelectedPath(file.path)}
                  className={`mb-1 flex w-full items-start gap-2 rounded-md px-2.5 py-2 text-left text-xs hover:bg-muted ${selected?.path === file.path ? 'bg-muted text-foreground' : 'text-muted-foreground'}`}
                >
                  <FileText className="mt-0.5 size-3.5 shrink-0" />
                  <span className="min-w-0 flex-1 break-all">{file.path}</span>
                  <span className="shrink-0 tabular-nums">{formatBytes(file.size_bytes)}</span>
                </button>
              ))}
            </aside>
            <main className="min-h-0 overflow-y-auto p-4 sm:p-6">
              {query.data?.partial && <p className="mb-3 text-xs text-amber-700 dark:text-amber-400">内容已按安全限制截断</p>}
              {selected?.truncated && <p className="mb-3 text-xs text-amber-700 dark:text-amber-400">此文件已截断</p>}
              <article className="prose prose-sm max-w-none break-words dark:prose-invert">
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
            </main>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
