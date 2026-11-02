import { useQueries, useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Radar } from 'lucide-react'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'

const statuses: Record<string, string> = { queued: '等待分析', preparing: '准备数据', dispatched: '等待执行', running: '正在分析', finalizing: '正在整理', succeeded: '分析完成', failed: '分析失败，可前往重试' }

export function TopicRadarSummary({ projectId }: { projectId: string | null }) {
  const subscriptions = useQuery({
    queryKey: queryKeys.topicRadar.subscriptions(projectId ?? ''),
    queryFn: () => api.topicRadar.subscriptions(projectId!),
    enabled: Boolean(projectId),
    staleTime: 30_000,
  })
  const histories = useQueries({ queries: (subscriptions.data?.items ?? []).map((subscription) => ({
    queryKey: queryKeys.topicRadar.digests(subscription.id),
    queryFn: () => api.topicRadar.digests(subscription.id),
    staleTime: 30_000,
  })) })
  const latest = histories.flatMap((history) => history.data?.items ?? []).sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
  const detail = useQuery({
    queryKey: ['topic-radar', 'digest', latest?.id],
    queryFn: () => api.topicRadar.digest(latest!.id),
    enabled: Boolean(latest),
    staleTime: 30_000,
    refetchInterval: (query) => {
      const status = query.state.data?.digest.status ?? latest?.status
      return status && !['succeeded', 'failed'].includes(status) ? 5_000 : false
    },
  })
  if (!projectId) return null
  const entry = `/topic-radar?project_id=${encodeURIComponent(projectId)}`
  const digest = detail.data?.digest ?? latest
  const failed = subscriptions.isError || histories.some((history) => history.isError) || detail.isError
  const loading = subscriptions.isPending || histories.some((history) => history.isPending)

  return <section aria-labelledby="topic-radar-summary-title" className="mx-auto w-full max-w-3xl rounded-xl border p-4">
    <div className="flex items-center justify-between gap-3">
      <h2 id="topic-radar-summary-title" className="flex items-center gap-2 text-sm font-semibold"><Radar className="size-4 text-primary" />今日选题</h2>
      <Link to={entry} className="text-xs text-primary hover:underline">打开选题雷达</Link>
    </div>
    <p className="mt-2 text-xs text-muted-foreground">推荐选题自动进入选题库，脚本与通知独立处理。</p>
    {failed ? <p role="status" className="mt-3 text-sm text-muted-foreground">暂时无法加载，打开选题雷达后可重试。</p>
      : loading ? <p role="status" className="mt-3 text-sm text-muted-foreground">加载选题摘要…</p>
      : !latest ? <p className="mt-3 text-sm text-muted-foreground">{subscriptions.data?.items.length ? '订阅已就绪，等待下一次分析。' : '为当前人设启用每日选题推荐。'}</p>
      : <div className="mt-3 space-y-2">
          <p className="text-xs text-muted-foreground">最近运行：{new Date(latest.created_at).toLocaleString()} · {statuses[digest?.status ?? ''] ?? digest?.status}</p>
          {detail.data?.suggestions.slice(0, 3).map((suggestion) => <p key={suggestion.id} className="truncate text-sm">{suggestion.title}</p>)}
          <div className="flex gap-4 text-xs text-primary"><Link to={`${entry}&digest_id=${encodeURIComponent(latest.id)}`} className="hover:underline">查看本次分析</Link><Link to={`/topic-pool?project_id=${encodeURIComponent(projectId)}`} className="hover:underline">查看选题库</Link></div>
        </div>}
  </section>
}
