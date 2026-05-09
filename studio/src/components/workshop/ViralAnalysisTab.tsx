import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Loader2, Search, FlaskConical, Clock } from 'lucide-react'
import { api } from '@/lib/api'
import type { CreateViralAnalysisRequest } from '@/types'
import { formatDateTimeCN } from '@/lib/labels'
import AnalysisReport from '@/components/workshop/AnalysisReport'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { ScrollArea } from '@/components/ui/scroll-area'
import Badge from '@/components/ui/Badge'
import EmptyState from '@/components/EmptyState'

const statusVariantMap: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  pending: 'outline',
  analyzing: 'default',
  completed: 'success' as 'default',
  failed: 'destructive',
}

const statusLabelMap: Record<string, string> = {
  pending: '等待中',
  analyzing: '分析中',
  completed: '已完成',
  failed: '失败',
}

export default function ViralAnalysisTab() {
  const queryClient = useQueryClient()
  const [url, setUrl] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [])

  const { data: analysesData, isLoading: listLoading } = useQuery({
    queryKey: ['viral-analyses'],
    queryFn: () => api.viralAnalyses.list({ limit: 50 }),
  })

  const analyses = analysesData?.items ?? []

  const selected = selectedId ? analyses.find((a) => a.id === selectedId) : null

  const createMutation = useMutation({
    mutationFn: (data: CreateViralAnalysisRequest) => api.viralAnalyses.create(data),
    onSuccess: (newAnalysis) => {
      toast.success('分析任务已创建')
      queryClient.invalidateQueries({ queryKey: ['viral-analyses'] })
      setSelectedId(newAnalysis.id)
      setUrl('')
      pollAnalysis(newAnalysis.id)
    },
    onError: () => {
      toast.error('创建分析任务失败，请检查链接后重试')
    },
  })

  function pollAnalysis(id: string) {
    if (timerRef.current) clearInterval(timerRef.current)
    timerRef.current = setInterval(async () => {
      try {
        const result = await api.viralAnalyses.get(id)
        if (result.status === 'completed' || result.status === 'failed') {
          if (timerRef.current) clearInterval(timerRef.current)
          timerRef.current = null
          queryClient.invalidateQueries({ queryKey: ['viral-analyses'] })
        }
      } catch {
        if (timerRef.current) clearInterval(timerRef.current)
        timerRef.current = null
      }
    }, 3000)

    // Safety timeout: stop polling after 5 minutes
    setTimeout(() => {
      if (timerRef.current) clearInterval(timerRef.current)
      timerRef.current = null
    }, 300_000)
  }

  function handleAnalyze() {
    const trimmed = url.trim()
    if (!trimmed) {
      toast.error('请输入小红书笔记链接')
      return
    }
    createMutation.mutate({ source_type: 'note', source_url: trimmed })
  }

  return (
    <div className="flex h-full gap-4">
      {/* Left sidebar - History list */}
      <div className="hidden w-64 shrink-0 flex-col rounded-lg border border-border bg-card md:flex">
        <div className="border-b border-border px-3 py-3">
          <h3 className="text-sm font-medium text-foreground">分析历史</h3>
        </div>
        <ScrollArea className="flex-1">
          {listLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : analyses.length === 0 ? (
            <p className="px-3 py-8 text-center text-xs text-muted-foreground">
              暂无分析记录
            </p>
          ) : (
            <div className="space-y-0.5 p-1.5">
              {analyses.map((analysis) => (
                <button
                  key={analysis.id}
                  type="button"
                  onClick={() => setSelectedId(analysis.id)}
                  className={`w-full rounded-md px-2.5 py-2 text-left transition-colors ${
                    selectedId === analysis.id
                      ? 'bg-primary/10 text-foreground'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                  }`}
                >
                  <div className="flex items-center justify-between gap-1">
                    <span className="truncate text-xs font-medium">
                      {analysis.source_url
                        ? new URL(analysis.source_url).pathname.split('/').filter(Boolean).pop() || '笔记'
                        : '笔记'}
                    </span>
                    <Badge
                      variant={statusVariantMap[analysis.status] ?? 'outline'}
                      className="text-[10px] shrink-0"
                    >
                      {statusLabelMap[analysis.status] ?? analysis.status}
                    </Badge>
                  </div>
                  <span className="mt-0.5 block text-[11px] text-muted-foreground">
                    {formatDateTimeCN(analysis.created_at)}
                  </span>
                </button>
              ))}
            </div>
          )}
        </ScrollArea>
      </div>

      {/* Main content */}
      <div className="min-w-0 flex-1 space-y-4">
        {/* Input section */}
        <div className="flex gap-2">
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleAnalyze()
            }}
            placeholder="粘贴小红书笔记链接，例如 https://www.xiaohongshu.com/explore/..."
            className="flex-1"
          />
          <Button onClick={handleAnalyze} loading={createMutation.isPending}>
            <Search className="h-4 w-4" />
            分析
          </Button>
        </div>

        {/* Content area */}
        {!selected ? (
          <EmptyState
            icon={FlaskConical}
            title="爆文拆解"
            description="粘贴一篇小红书爆款笔记链接，AI 将从标题、封面、文案、标签、互动五个维度进行深度拆解，帮你掌握爆文规律。"
          />
        ) : selected.status === 'pending' || selected.status === 'analyzing' ? (
          <div className="flex flex-col items-center justify-center gap-3 py-16">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
            <p className="text-sm text-muted-foreground">
              {selected.status === 'analyzing' ? '正在深度分析中，请稍候...' : '任务排队中，即将开始分析...'}
            </p>
          </div>
        ) : selected.status === 'failed' ? (
          <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-6 text-center">
            <p className="text-sm font-medium text-destructive">分析失败</p>
            <p className="mt-1 text-sm text-muted-foreground">
              {selected.error_message || '请检查链接后重试'}
            </p>
          </div>
        ) : selected.analysis_result ? (
          <AnalysisReport analysis={selected.analysis_result} />
        ) : (
          <p className="py-16 text-center text-sm text-muted-foreground">分析结果为空</p>
        )}

        {/* Mobile history (shown on small screens) */}
        {analyses.length > 0 && (
          <div className="md:hidden">
            <div className="flex items-center gap-2 border-t border-border pt-4">
              <Clock className="h-4 w-4 text-muted-foreground" />
              <h3 className="text-sm font-medium text-muted-foreground">历史记录</h3>
            </div>
            <div className="mt-2 flex gap-2 overflow-x-auto pb-2">
              {analyses.slice(0, 10).map((analysis) => (
                <button
                  key={analysis.id}
                  type="button"
                  onClick={() => setSelectedId(analysis.id)}
                  className={`shrink-0 rounded-md border px-3 py-1.5 text-xs transition-colors ${
                    selectedId === analysis.id
                      ? 'border-primary bg-primary/10 text-foreground'
                      : 'border-border text-muted-foreground hover:bg-muted'
                  }`}
                >
                  {formatDateTimeCN(analysis.created_at)}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
