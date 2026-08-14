import { useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bookmark, ExternalLink, Eye, Loader2, Share2, UserRound } from 'lucide-react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'

import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import { formatFullDateTimeCN } from '@/lib/labels'
import type { WechatAnalytics, WechatTrackingStatus } from '@/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

const statusCopy: Record<WechatTrackingStatus, string> = {
  waiting_data: '等待微信次日数据',
  tracking: '正在每日采集官方数据',
  stopped: '官方可查询窗口已结束',
  failed: '官方数据采集失败',
}

export default function WechatAnalyticsPanel({ taskId }: { taskId: string }) {
  const queryClient = useQueryClient()
  const [articleUrl, setArticleUrl] = useState('')
  const queryKey = ['wechat-analytics', taskId]
  const { data, isLoading, isError } = useQuery({
    queryKey,
    queryFn: () => api.wechatAnalytics.getByTask(taskId),
    enabled: Boolean(taskId),
  })
  const bind = useMutation({
    mutationFn: (url: string) => api.wechatAnalytics.bind(taskId, url),
    onSuccess: async () => {
      setArticleUrl('')
      await queryClient.invalidateQueries({ queryKey })
    },
  })

  if (isLoading) {
    return <Card><CardContent className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />公众号数据加载中</CardContent></Card>
  }
  if (isError) {
    return <Card><CardContent className="text-sm text-destructive">暂时无法加载公众号数据。</CardContent></Card>
  }
  if (!data?.tracking) {
    return (
      <Card>
        <CardHeader><CardTitle>公众号文章数据</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-muted-foreground">填写本项目公众号发布的文章链接，系统将通过微信官方接口校验并采集。</p>
          <BindForm value={articleUrl} pending={bind.isPending} error={bind.error} onChange={setArticleUrl} onSubmit={(url) => bind.mutate(url)} />
        </CardContent>
      </Card>
    )
  }
  return <WechatAnalyticsContent analytics={data} />
}

function WechatAnalyticsContent({ analytics }: { analytics: WechatAnalytics }) {
  const tracking = analytics.tracking!
  const status = tracking.status as WechatTrackingStatus
  const chartData = useMemo(() => analytics.series.map((item) => ({
    date: item.stat_date || new Date(item.captured_at).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }),
    reads: item.int_page_read_count,
    readers: item.int_page_read_user,
    shares: item.share_count,
    favorites: item.add_to_fav_count,
  })), [analytics.series])
  return (
    <Card>
      <CardHeader className="gap-3 sm:flex sm:flex-row sm:items-start sm:justify-between">
        <div>
          <CardTitle>公众号文章数据</CardTitle>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Badge variant={status === 'failed' ? 'destructive' : status === 'tracking' ? 'default' : 'outline'}>{statusCopy[status] ?? tracking.status}</Badge>
            <span className="text-xs text-muted-foreground">发布日期 {tracking.published_date}</span>
          </div>
        </div>
        <Button size="sm" variant="outline" nativeButton={false} render={<a href={tracking.article_url} target="_blank" rel="noreferrer" />}>
          <ExternalLink className="h-4 w-4" />打开文章
        </Button>
      </CardHeader>
      <CardContent className="space-y-5">
        <div>
          <p className="text-sm font-medium text-foreground">{tracking.article_title || '已关联公众号文章'}</p>
          <div className="mt-2 grid gap-2 text-xs text-muted-foreground sm:grid-cols-3">
            <span>最近采集：{formatDateTime(tracking.last_run_at)}</span>
            <span>下次采集：{formatDateTime(tracking.next_run_at)}</span>
            <span>采集次数：{tracking.run_count}</span>
          </div>
          {tracking.last_error && <p className="mt-2 text-xs text-amber-400">{tracking.last_error}</p>}
        </div>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Metric icon={<Eye />} label="阅读次数" value={analytics.latest?.int_page_read_count} delta={analytics.deltas?.int_page_read_count} />
          <Metric icon={<UserRound />} label="阅读人数" value={analytics.latest?.int_page_read_user} delta={analytics.deltas?.int_page_read_user} />
          <Metric icon={<Share2 />} label="分享次数" value={analytics.latest?.share_count} delta={analytics.deltas?.share_count} />
          <Metric icon={<Bookmark />} label="收藏次数" value={analytics.latest?.add_to_fav_count} delta={analytics.deltas?.add_to_fav_count} />
        </div>
        {chartData.length > 0 && (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={{ top: 8, right: 12, left: -18, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis dataKey="date" tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                <YAxis allowDecimals={false} tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                <Tooltip contentStyle={{ backgroundColor: 'var(--background)', color: 'var(--foreground)', border: '1px solid var(--border)', borderRadius: 'var(--radius)', fontSize: '12px' }} />
                <Line type="monotone" dataKey="reads" name="阅读次数" stroke="#0ea5e9" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="shares" name="分享次数" stroke="#22c55e" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="favorites" name="收藏次数" stroke="#f59e0b" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function BindForm({ value, pending, error, onChange, onSubmit }: { value: string; pending: boolean; error: Error | null; onChange: (value: string) => void; onSubmit: (value: string) => void }) {
  return (
    <div className="space-y-2">
      <form className="flex flex-col gap-2 sm:flex-row" onSubmit={(event) => { event.preventDefault(); const url = value.trim(); if (url) onSubmit(url) }}>
        <Input aria-label="公众号文章链接" placeholder="https://mp.weixin.qq.com/s/..." value={value} onChange={(event) => onChange(event.target.value)} />
        <Button type="submit" disabled={!value.trim() || pending}>{pending && <Loader2 className="h-4 w-4 animate-spin" />}{pending ? '验证中' : '关联文章'}</Button>
      </form>
      {error && <p className="text-xs text-destructive">{getApiErrorMessage(error, '无法通过当前项目的公众号官方接口识别该文章。')}</p>}
    </div>
  )
}

function Metric({ icon, label, value, delta }: { icon: ReactNode; label: string; value?: number; delta?: number }) {
  return <div className="rounded-md border border-border bg-muted/20 p-3"><div className="flex items-center gap-2 text-xs text-muted-foreground">{icon}<span>{label}</span></div><div className="mt-2 flex items-baseline gap-2"><span className="text-xl font-semibold tabular-nums">{value ?? '--'}</span>{delta != null && delta !== 0 && <span className="text-xs text-emerald-500">{delta > 0 ? '+' : ''}{delta}</span>}</div></div>
}

function formatDateTime(value?: string | null) {
  return value ? formatFullDateTimeCN(value) : '--'
}
