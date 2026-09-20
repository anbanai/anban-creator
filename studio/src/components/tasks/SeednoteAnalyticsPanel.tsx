import { useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  Bookmark,
  Eye,
  Heart,
  Link2,
  Loader2,
  MessageCircle,
  Share2,
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
import { queryKeys } from '@/lib/query-keys'
import { formatFullDateTimeCN } from '@/lib/labels'
import type { SeednoteAnalytics, SeednoteTrackingStatus } from '@/types'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import ContentSourceLink from '@/components/common/ContentSourceLink'

interface SeednoteAnalyticsPanelProps {
  taskId: string
}

const statusCopy: Record<SeednoteTrackingStatus, string> = {
  waiting_discovery: '历史记录等待关联',
  unresolved: '尚未关联公开笔记，请补充笔记链接或 ID',
  tracking: '正在每日采集公开数据',
  stopped: '数据变化已趋缓，已停止自动采集',
  failed: '暂时无法识别或采集这篇笔记',
}

const statusVariant: Record<SeednoteTrackingStatus, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  waiting_discovery: 'outline',
  unresolved: 'outline',
  tracking: 'default',
  stopped: 'secondary',
  failed: 'destructive',
}

const stopReasonLabel: Record<string, string> = {
  max_duration_reached: '已达到最长追踪周期',
  low_growth: '连续多天增量较低',
  discovery_timeout: '连续 7 天未能识别笔记',
  too_many_failures: '连续采集失败次数过多',
  manual_stop: '已手动停止',
}

const chartColors = {
  like_count: '#ef4444',
  collect_count: '#f59e0b',
  comment_count: '#0ea5e9',
  share_count: '#22c55e',
}

export default function SeednoteAnalyticsPanel({ taskId }: SeednoteAnalyticsPanelProps) {
  const queryClient = useQueryClient()
  const [publicationIdentity, setPublicationIdentity] = useState('')
  const bindMutation = useMutation({
    mutationFn: (value: string) => api.seednoteAnalytics.bind(
      taskId,
      value.startsWith('http') ? { note_url: value } : { note_id: value },
    ),
    onSuccess: async () => {
      setPublicationIdentity('')
      await queryClient.invalidateQueries({ queryKey: queryKeys.tasks.seednoteAnalytics(taskId) })
    },
  })
  const { data, error, isLoading, isError } = useQuery({
    queryKey: queryKeys.tasks.seednoteAnalytics(taskId),
    queryFn: () => api.seednoteAnalytics.getByTask(taskId),
    enabled: Boolean(taskId),
  })

  if (isLoading) {
    return (
      <Card>
        <CardContent className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          种草笔记公开数据加载中
        </CardContent>
      </Card>
    )
  }

  const status = (error as { response?: { status?: number } } | null)?.response?.status
  if (isError && status === 404) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>种草笔记数据</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          暂无公开数据，追踪尚未准备
        </CardContent>
      </Card>
    )
  }

  if (isError) {
    return (
      <Card>
        <CardContent className="flex items-center gap-2 text-sm text-amber-400">
          <AlertCircle className="h-4 w-4" />
          暂时无法加载种草笔记数据
        </CardContent>
      </Card>
    )
  }

  if (!data?.tracking) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>种草笔记数据</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm text-muted-foreground">
          <p>填写可访问的公开笔记链接后开始每日采集。</p>
          <SeednoteBindForm
            value={publicationIdentity}
            pending={bindMutation.isPending}
            error={bindMutation.error}
            onChange={setPublicationIdentity}
            onSubmit={(value) => bindMutation.mutate(value)}
          />
        </CardContent>
      </Card>
    )
  }

  return (
    <SeednoteAnalyticsContent
      analytics={data}
      publicationIdentity={publicationIdentity}
      bindPending={bindMutation.isPending}
      bindError={bindMutation.error}
      onIdentityChange={setPublicationIdentity}
      onBind={(value) => bindMutation.mutate(value)}
    />
  )
}

function SeednoteAnalyticsContent({
  analytics,
  publicationIdentity,
  bindPending,
  bindError,
  onIdentityChange,
  onBind,
}: {
  analytics: SeednoteAnalytics
  publicationIdentity: string
  bindPending: boolean
  bindError: Error | null
  onIdentityChange: (value: string) => void
  onBind: (value: string) => void
}) {
  const tracking = analytics.tracking!

  const status = tracking.status as SeednoteTrackingStatus
  const statusText = statusCopy[status] ?? tracking.status
  const variant = statusVariant[status] ?? 'outline'
  const chartData = useMemo(() => analytics.series.map((item) => ({
    date: new Date(item.captured_at).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }),
    like_count: item.like_count,
    collect_count: item.collect_count,
    comment_count: item.comment_count,
    share_count: item.share_count,
  })), [analytics.series])

  return (
    <Card>
      <CardHeader className="gap-3 sm:flex sm:flex-row sm:items-start sm:justify-between">
        <div>
          <CardTitle>种草笔记数据</CardTitle>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <Badge variant={variant}>{statusText}</Badge>
            {tracking.stop_reason && (
              <span className="text-xs text-muted-foreground">
                {stopReasonLabel[tracking.stop_reason] ?? tracking.stop_reason}
              </span>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_220px]">
          <div className="space-y-3">
            <div>
              <p className="text-sm font-medium text-foreground">{tracking.note_title || '等待绑定公开笔记'}</p>
              <ContentSourceLink url={tracking.note_url} className="mt-1" />
              <div className="mt-2 grid gap-2 text-xs text-muted-foreground sm:grid-cols-2">
                <span>最近采集：{formatDateTime(tracking.last_run_at)}</span>
                <span>下次采集：{formatDateTime(tracking.next_run_at)}</span>
                <span>采集次数：{tracking.run_count}</span>
                <span>关联时间：{formatDateTime(tracking.discovered_at)}</span>
              </div>
            </div>
            {status === 'unresolved' && (
              <SeednoteBindForm
                value={publicationIdentity}
                pending={bindPending}
                error={bindError}
                onChange={onIdentityChange}
                onSubmit={onBind}
              />
            )}
            {tracking.last_error && status !== 'unresolved' && (
              <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-300">
                {friendlyTrackingError(tracking.last_error)}
              </div>
            )}
          </div>
          {tracking.note_cover_url && (
            <img
              src={tracking.note_cover_url}
              alt=""
              className="h-36 w-full rounded-md object-cover ring-1 ring-border lg:h-32"
            />
          )}
        </div>

        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <MetricTile icon={<Heart />} label="点赞" value={analytics.latest?.like_count} delta={analytics.deltas?.like_count} />
          <MetricTile icon={<Bookmark />} label="收藏" value={analytics.latest?.collect_count} delta={analytics.deltas?.collect_count} />
          <MetricTile icon={<MessageCircle />} label="评论" value={analytics.latest?.comment_count} delta={analytics.deltas?.comment_count} />
          <MetricTile icon={<Share2 />} label="分享" value={analytics.latest?.share_count} delta={analytics.deltas?.share_count} />
          <MetricTile icon={<Eye />} label="曝光" value={analytics.latest?.view_count ?? null} unavailable={analytics.latest?.view_count == null} />
        </div>

        {chartData.length > 0 && (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={{ top: 8, right: 12, left: -18, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis dataKey="date" tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                <YAxis allowDecimals={false} tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                <Tooltip
                  contentStyle={{
                    backgroundColor: 'var(--background)',
                    color: 'var(--foreground)',
                    border: '1px solid var(--border)',
                    borderRadius: 'var(--radius)',
                    fontSize: '12px',
                  }}
                />
                <Line type="monotone" dataKey="like_count" name="点赞" stroke={chartColors.like_count} strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="collect_count" name="收藏" stroke={chartColors.collect_count} strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="comment_count" name="评论" stroke={chartColors.comment_count} strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="share_count" name="分享" stroke={chartColors.share_count} strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function SeednoteBindForm({
  value,
  pending,
  error,
  onChange,
  onSubmit,
}: {
  value: string
  pending: boolean
  error: Error | null
  onChange: (value: string) => void
  onSubmit: (value: string) => void
}) {
  return (
    <div className="space-y-2">
      <form
        className="flex flex-col gap-2 sm:flex-row"
        onSubmit={(event) => {
          event.preventDefault()
          const normalized = value.trim()
          if (normalized) onSubmit(normalized)
        }}
      >
        <Input
          aria-label="公开笔记链接或 ID"
          placeholder="公开笔记链接或 ID"
          value={value}
          onChange={(event) => onChange(event.target.value)}
        />
        <Button type="submit" disabled={!value.trim() || pending}>
          {pending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Link2 className="h-4 w-4" />}
          {pending ? '验证中' : '关联公开笔记'}
        </Button>
      </form>
      <ContentSourceLink url={value.trim().startsWith('http') ? value : undefined} noteId={value.trim()} />
      {error && <p className="text-xs text-destructive">链接无法读取，请确认笔记为公开状态。</p>}
    </div>
  )
}

function MetricTile({
  icon,
  label,
  value,
  delta,
  unavailable,
}: {
  icon: ReactNode
  label: string
  value?: number | null
  delta?: number
  unavailable?: boolean
}) {
  return (
    <div className="rounded-md border border-border bg-background/40 p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="[&_svg]:h-3.5 [&_svg]:w-3.5">{icon}</span>
        {label}
      </div>
      <div className="mt-2 flex min-h-7 items-end justify-between gap-2">
        <span className="text-lg font-semibold tabular-nums text-foreground">
          {unavailable ? '暂无公开数据' : formatNumber(value)}
        </span>
        {delta != null && (
          <span className="text-xs tabular-nums text-emerald-400">
            +{formatNumber(delta)}
          </span>
        )}
      </div>
    </div>
  )
}

function formatNumber(value?: number | null) {
  if (value == null) return '0'
  return value.toLocaleString('zh-CN')
}

function formatDateTime(value?: string | null) {
  return value ? formatFullDateTimeCN(value) : '--'
}

function friendlyTrackingError(error: string) {
  if (!error) return ''
  if (error.includes('profile URL')) return '项目主页链接缺失，请检查项目配置'
  if (error.includes('fetch')) return '公开页面暂时无法访问，系统会继续重试'
  if (error.includes('matched')) return '尚未从主页识别到对应笔记'
  return '公开数据采集暂时异常，系统会继续重试'
}
