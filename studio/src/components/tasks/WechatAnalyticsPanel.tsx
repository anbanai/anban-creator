import { useMemo } from 'react'
import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Bookmark,
  CheckCircle2,
  ExternalLink,
  Eye,
  Loader2,
  MessageCircle,
  Share2,
  Star,
  Timer,
  UserPlus,
  UserRound,
} from 'lucide-react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import { api } from '@/lib/api'
import type { WechatAnalytics } from '@/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export default function WechatAnalyticsPanel({
  taskId,
}: {
  taskId: string
}) {
  const publicationQuery = useQuery({
    queryKey: ['wechat-publication', taskId],
    queryFn: () => api.tasks.getWechatPublication(taskId),
    enabled: Boolean(taskId),
    retry: false,
  })

  if (!publicationQuery.data || publicationQuery.data.status !== 'published') return null
  return <WechatAnalyticsContent taskId={taskId} />
}

function WechatAnalyticsContent({ taskId }: { taskId: string }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['wechat-analytics', taskId],
    queryFn: () => api.wechatAnalytics.getByTask(taskId),
    enabled: Boolean(taskId),
  })
  if (isLoading) {
    return (
      <Card>
        <CardContent className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          数据加载中
        </CardContent>
      </Card>
    )
  }
  if (isError || !data?.tracking) {
    return (
      <Card>
        <CardContent className="text-sm text-muted-foreground">文章已识别，微信数据将在次日可查询后显示。</CardContent>
      </Card>
    )
  }
  return <AnalyticsContent analytics={data} />
}

function AnalyticsContent({ analytics }: { analytics: WechatAnalytics }) {
  const tracking = analytics.tracking!
  const latest = analytics.metrics ?? analytics.latest
  const trend = analytics.trend ?? analytics.series ?? []
  const chartData = useMemo(
    () => trend.map((item) => ({
      date: item.stat_date || new Date(item.captured_at).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }),
      reads: item.read_users ?? item.int_page_read_user ?? 0,
      shares: item.share_users ?? item.share_count ?? 0,
      favorites: item.collection_users ?? item.add_to_fav_count ?? 0,
    })),
    [trend],
  )
  return (
    <Card>
      <CardHeader className="gap-2 sm:flex sm:flex-row sm:items-start sm:justify-between">
        <div>
          <CardTitle>公众号数据追踪</CardTitle>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Badge variant={tracking.status === 'failed' ? 'destructive' : tracking.status === 'tracking' ? 'default' : 'outline'}>
              {tracking.status === 'waiting_data' ? '等待微信次日数据' : tracking.status === 'tracking' ? '正在每日采集官方数据' : tracking.status}
            </Badge>
            <span className="text-xs text-muted-foreground">追踪至发表后 30 天</span>
          </div>
        </div>
        {tracking.article_url && (
          <Button size="sm" variant="outline" nativeButton={false} render={<a href={tracking.article_url} target="_blank" rel="noreferrer" />}>
            <ExternalLink className="h-4 w-4" />
            打开文章
          </Button>
        )}
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Metric icon={<Eye />} label="阅读人数" value={latest?.read_users ?? latest?.int_page_read_user} />
          <Metric icon={<Share2 />} label="分享人数" value={latest?.share_users ?? latest?.share_count} />
          <Metric icon={<Bookmark />} label="收藏人数" value={latest?.collection_users ?? latest?.add_to_fav_count} />
          <Metric icon={<Star />} label="点赞" value={latest?.like_users} />
          <Metric icon={<UserRound />} label="在看" value={latest?.zaikan_users} />
          <Metric icon={<MessageCircle />} label="评论" value={latest?.comment_count} />
          <Metric icon={<CheckCircle2 />} label="完读率" value={latest?.read_finish_rate} suffix="%" />
          <Metric icon={<Timer />} label="平均阅读时长" value={latest?.average_read_active_time} suffix="秒" />
          <Metric icon={<UserPlus />} label="阅读后关注" value={latest?.read_to_subscribe_users} />
        </div>
        {chartData.length > 0 && (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={{ top: 8, right: 12, left: -18, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis dataKey="date" tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                <YAxis allowDecimals={false} tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                <Tooltip />
                <Line type="monotone" dataKey="reads" name="阅读人数" stroke="#0ea5e9" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="shares" name="分享人数" stroke="#22c55e" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="favorites" name="收藏人数" stroke="#f59e0b" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function Metric({ icon, label, value, suffix = '' }: { icon: ReactNode; label: string; value?: number; suffix?: string }) {
  return (
    <div className="rounded-md border border-border bg-muted/20 p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">{icon}<span>{label}</span></div>
      <div className="mt-2 text-xl font-semibold tabular-nums">{value == null ? '--' : `${value}${suffix}`}</div>
    </div>
  )
}
