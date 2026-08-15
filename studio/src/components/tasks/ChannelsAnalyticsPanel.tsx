import { useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bookmark, ExternalLink, Heart, Loader2, MessageCircle, Share2 } from 'lucide-react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { formatFullDateTimeCN } from '@/lib/labels'
import { queryKeys } from '@/lib/query-keys'
import type { ChannelsAnalytics, ChannelsTrackingStatus } from '@/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

const statusCopy: Record<ChannelsTrackingStatus, string> = {
  tracking: '正在每日采集',
  stopped: '采集周期已结束',
  failed: '数据采集失败',
}

export default function ChannelsAnalyticsPanel({ taskId }: { taskId: string }) {
  const queryClient = useQueryClient()
  const [videoUrl, setVideoUrl] = useState('')
  const queryKey = queryKeys.tasks.channelsAnalytics(taskId)
  const { data, isLoading, isError } = useQuery({
    queryKey,
    queryFn: () => api.channelsAnalytics.getByTask(taskId),
    enabled: Boolean(taskId),
  })
  const bind = useMutation({
    mutationFn: (url: string) => api.channelsAnalytics.bind(taskId, url),
    onSuccess: async () => {
      setVideoUrl('')
      await queryClient.invalidateQueries({ queryKey })
    },
  })

  if (isLoading) {
    return <Card><CardContent className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />视频号数据加载中</CardContent></Card>
  }
  if (isError) {
    return <Card><CardContent className="text-sm text-destructive">暂时无法加载视频号数据。</CardContent></Card>
  }
  if (!data?.tracking) {
    return (
      <Card>
        <CardHeader><CardTitle>视频号数据</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-muted-foreground">填写正常的视频号公开视频链接，系统会立即获取首条数据，并在之后每日更新。</p>
          <BindForm value={videoUrl} pending={bind.isPending} error={bind.error} onChange={setVideoUrl} onSubmit={(url) => bind.mutate(url)} />
          <p className="text-xs text-muted-foreground">数据由世界树科技第三方接口提供，当前包含点赞、收藏、评论和转发，不包含播放量。</p>
        </CardContent>
      </Card>
    )
  }
  return <ChannelsAnalyticsContent analytics={data} />
}

function ChannelsAnalyticsContent({ analytics }: { analytics: ChannelsAnalytics }) {
  const tracking = analytics.tracking!
  const status = tracking.status as ChannelsTrackingStatus
  const chartData = useMemo(() => analytics.series.map((item) => ({
    date: new Date(item.captured_at).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }),
    likes: item.like_count,
    favorites: item.favorite_count,
    comments: item.comment_count,
    forwards: item.forward_count,
  })), [analytics.series])
  return (
    <Card>
      <CardHeader className="gap-3 sm:flex sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <CardTitle>视频号数据</CardTitle>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Badge variant={status === 'failed' ? 'destructive' : status === 'tracking' ? 'default' : 'outline'}>{statusCopy[status] ?? tracking.status}</Badge>
            <span className="text-xs text-muted-foreground">第三方来源：{tracking.provider_name}</span>
          </div>
        </div>
        <Button size="sm" variant="outline" nativeButton={false} render={<a href={tracking.video_url} target="_blank" rel="noreferrer" />}>
          <ExternalLink className="h-4 w-4" />打开视频
        </Button>
      </CardHeader>
      <CardContent className="space-y-5">
        <div>
          <p className="text-sm font-medium text-foreground">{tracking.video_title || '已关联视频号视频'}</p>
          {tracking.author_name && <p className="mt-1 text-xs text-muted-foreground">{tracking.author_name}</p>}
          <div className="mt-2 grid gap-2 text-xs text-muted-foreground sm:grid-cols-3">
            <span>最近采集：{formatDateTime(tracking.last_run_at)}</span>
            <span>下次采集：{formatDateTime(tracking.next_run_at)}</span>
            <span>采集次数：{tracking.run_count}</span>
          </div>
          {tracking.last_error && <p className="mt-2 text-xs text-amber-400">{tracking.last_error}</p>}
        </div>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Metric icon={<Heart />} label="点赞数" value={analytics.latest?.like_count} delta={analytics.deltas?.like_count} />
          <Metric icon={<Bookmark />} label="收藏数" value={analytics.latest?.favorite_count} delta={analytics.deltas?.favorite_count} />
          <Metric icon={<MessageCircle />} label="评论数" value={analytics.latest?.comment_count} delta={analytics.deltas?.comment_count} />
          <Metric icon={<Share2 />} label="转发数" value={analytics.latest?.forward_count} delta={analytics.deltas?.forward_count} />
        </div>
        {chartData.length > 0 && (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={{ top: 8, right: 12, left: -18, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis dataKey="date" tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                <YAxis allowDecimals={false} tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                <Tooltip contentStyle={{ backgroundColor: 'var(--background)', color: 'var(--foreground)', border: '1px solid var(--border)', borderRadius: 'var(--radius)', fontSize: '12px' }} />
                <Line type="monotone" dataKey="likes" name="点赞" stroke="#ef4444" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="favorites" name="收藏" stroke="#f59e0b" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="comments" name="评论" stroke="#0ea5e9" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="forwards" name="转发" stroke="#22c55e" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
        <p className="text-xs text-muted-foreground">当前接口不提供播放量；系统按绑定日起最多采集 14 天。</p>
      </CardContent>
    </Card>
  )
}

function BindForm({ value, pending, error, onChange, onSubmit }: { value: string; pending: boolean; error: Error | null; onChange: (value: string) => void; onSubmit: (value: string) => void }) {
  return (
    <div className="space-y-2">
      <form className="flex flex-col gap-2 sm:flex-row" onSubmit={(event) => { event.preventDefault(); const url = value.trim(); if (url) onSubmit(url) }}>
        <Input aria-label="视频号视频链接" placeholder="https://channels.weixin.qq.com/web/pages/feed?..." value={value} onChange={(event) => onChange(event.target.value)} />
        <Button type="submit" disabled={!value.trim() || pending}>{pending && <Loader2 className="h-4 w-4 animate-spin" />}{pending ? '获取中' : '关联并获取'}</Button>
      </form>
      {error && <p className="text-xs text-destructive">{getApiErrorMessage(error, '无法通过第三方接口获取该视频号视频。')}</p>}
    </div>
  )
}

function Metric({ icon, label, value, delta }: { icon: ReactNode; label: string; value?: number; delta?: number }) {
  return <div className="rounded-md border border-border bg-muted/20 p-3"><div className="flex items-center gap-2 text-xs text-muted-foreground">{icon}<span>{label}</span></div><div className="mt-2 flex items-baseline gap-2"><span className="text-xl font-semibold tabular-nums">{value ?? '--'}</span>{delta != null && delta !== 0 && <span className="text-xs text-emerald-500">{delta > 0 ? '+' : ''}{delta}</span>}</div></div>
}

function formatDateTime(value?: string | null) {
  return value ? formatFullDateTimeCN(value) : '--'
}
