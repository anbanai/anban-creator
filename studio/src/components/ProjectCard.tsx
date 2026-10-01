import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Archive, Brain, CalendarClock, Database, Lightbulb, Pause, Pencil, Play, RefreshCw, RotateCcw, UserRound } from 'lucide-react'
import type { Project, ProjectStats } from '@/types'
import { api } from '@/lib/api'
import { platformDisplayName } from '@/lib/labels'
import { renderPlatformIcon, platformBadgeVariant, platformBadgeClassName } from '@/lib/PlatformIcon'
import { PlatformAvatar } from '@/components/PlatformAvatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { TopicPoolDialog } from '@/components/TopicPoolDialog'
import { ProjectMemoryDialog } from '@/components/projects/ProjectMemoryDialog'
import { ImageAnalysisBadge } from '@/components/image-analysis/ImageAnalysisBadge'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

interface ProjectCardProps {
  project: Project
  stats?: ProjectStats
  onEdit?: (project: Project) => void
  onProfile?: (project: Project) => void
  archiving?: boolean
  restoring?: boolean
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
}

export function ProjectCard({ project, stats, onEdit, onProfile, archiving, restoring, onArchive, onRestore }: ProjectCardProps) {
  const [topicPoolOpen, setTopicPoolOpen] = useState(false)
  const [memoryOpen, setMemoryOpen] = useState(false)
  const [monthlyRerunOpen, setMonthlyRerunOpen] = useState(false)
  const [periodStart, setPeriodStart] = useState('')
  const [periodEnd, setPeriodEnd] = useState('')
  const queryClient = useQueryClient()
  const platform = project.platform ?? 'wechat'
  const platformLabel = platformDisplayName(platform)
  const platformBadge = platformBadgeVariant[platform] || ('secondary' as const)
  const positioning = project.instructions || project.positioning || ''
  const isArchived = project.status === 'archived'
  const unusedTopics = stats?.unused_topics
  const cardTone = isArchived
    ? 'border-border/60 bg-muted/30'
    : 'border-border bg-card hover:border-foreground/20 hover:shadow-sm'
  const feedbackEnabled = platform === 'wechat' || platform === 'seednote'
  const feedbackQuery = useQuery({
    queryKey: ['project-feedback', project.id],
    queryFn: ({ signal }) => api.projects.feedback(project.id, signal),
    enabled: feedbackEnabled,
    staleTime: 60_000,
  })
  const feedbackStateMutation = useMutation({
    mutationFn: (paused: boolean) => api.projects.setFeedbackPaused(project.id, paused),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['project-feedback', project.id] })
      toast.success('反馈调度状态已更新')
    },
    onError: () => toast.error('更新反馈调度状态失败'),
  })
  const rerunMutation = useMutation({
    mutationFn: (cadence: 'daily' | 'weekly' | 'monthly') => api.projects.rerunFeedback(
      project.id,
      cadence,
      cadence === 'monthly' ? { start: periodStart, end: periodEnd } : undefined,
    ),
    onSuccess: (result) => {
      setMonthlyRerunOpen(false)
      void queryClient.invalidateQueries({ queryKey: ['project-feedback', project.id] })
      toast.success(`已检查反馈队列：创建 ${result.created}，跳过 ${result.skipped}`)
    },
    onError: () => toast.error('反馈重跑失败'),
  })
  const formatTime = (value?: string | null) => value
    ? new Date(value).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    : '未运行'
  const runRerun = (cadence: 'daily' | 'weekly' | 'monthly') => {
    if (cadence === 'monthly') {
      setMonthlyRerunOpen(true)
      return
    }
    rerunMutation.mutate(cadence)
  }

  return (
    <div className={`flex h-full flex-col rounded-lg border p-4 transition-[border-color,box-shadow] ${cardTone}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <PlatformAvatar avatarUrl={project.avatar_url} name={project.name} platform={platform} size="lg" />
          <div className="min-w-0">
            <h3 className="truncate text-sm font-semibold text-foreground">{project.name}</h3>
            <Badge variant={platformBadge} className={`mt-1 text-[10px] font-normal ${platformBadgeClassName[platform] || ''}`}>
              {renderPlatformIcon(platform)}
              {platformLabel}
            </Badge>
          </div>
        </div>
        <div className="flex flex-col items-end gap-1">
          {isArchived && <Badge variant="outline">已归档</Badge>}
          <ImageAnalysisBadge analysis={project.image_analysis} />
        </div>
      </div>
      {positioning && (
        <p className="mt-3 line-clamp-2 text-sm text-muted-foreground">{positioning}</p>
      )}
      <div className="mt-auto pt-4">
        {stats && (
          <div className="flex min-h-8 items-center border-t border-border pt-3">
            <div className="flex gap-3 text-xs text-muted-foreground">
              <span>{stats.total_tasks} 个任务</span>
              {stats.total_tasks > 0 && <span>{stats.completed_tasks} 个已完成</span>}
            </div>
          </div>
        )}
        {feedbackEnabled && (
          <div className="mt-3 border-t border-border pt-3">
            {feedbackQuery.isPending ? (
              <div className="flex items-center gap-2 text-xs text-muted-foreground"><Database className="h-3.5 w-3.5" />加载反馈状态…</div>
            ) : feedbackQuery.isError ? (
              <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
                <span>反馈状态暂不可用</span>
                <Button variant="ghost" size="xs" onClick={() => void feedbackQuery.refetch()} aria-label="重新加载反馈状态"><RefreshCw />重试</Button>
              </div>
            ) : feedbackQuery.data ? (
              <div className="space-y-2 text-xs">
                <div className="flex items-center justify-between gap-2">
                  <span className="flex items-center gap-1.5 font-medium text-foreground"><Database className="h-3.5 w-3.5" />反馈闭环</span>
                  <Badge variant={feedbackQuery.data.feedback_paused ? 'outline' : 'secondary'}>
                    {feedbackQuery.data.feedback_paused ? '已暂停' : `数据 rev ${feedbackQuery.data.analytics.revision}`}
                  </Badge>
                </div>
                <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-muted-foreground">
                  <span>内容 {feedbackQuery.data.analytics.content_count}</span>
                  <span>观测 {feedbackQuery.data.analytics.valid_observation_count}</span>
                  <span>队列中 {feedbackQuery.data.queue.counts.queued + feedbackQuery.data.queue.counts.running}</span>
                  <span>已完成 {feedbackQuery.data.queue.counts.succeeded}</span>
                </div>
                <div className="flex items-center justify-between gap-2 text-muted-foreground">
                  <span>策略 {feedbackQuery.data.strategy.status === 'active' ? `rev ${feedbackQuery.data.strategy.revision}` : '暂无'}</span>
                  <span>上次 {formatTime(feedbackQuery.data.queue.last_success_at)}</span>
                </div>
                {feedbackQuery.data.queue.latest_skip && (
                  <p className="truncate text-muted-foreground" title={feedbackQuery.data.queue.latest_skip.reason}>最近跳过：{feedbackQuery.data.queue.latest_skip.reason}</p>
                )}
                <div className="flex flex-wrap gap-1">
                  <Button variant="ghost" size="xs" disabled={feedbackStateMutation.isPending} onClick={() => feedbackStateMutation.mutate(!feedbackQuery.data!.feedback_paused)}>
                    {feedbackQuery.data.feedback_paused ? <Play /> : <Pause />}
                    {feedbackQuery.data.feedback_paused ? '恢复' : '暂停'}
                  </Button>
                  <Button variant="ghost" size="xs" disabled={rerunMutation.isPending || feedbackQuery.data.feedback_paused} onClick={() => runRerun('daily')} title={`下次日周期：${formatTime(feedbackQuery.data.next_runs.daily)}`}>
                    <RefreshCw />日
                  </Button>
                  <Button variant="ghost" size="xs" disabled={rerunMutation.isPending || feedbackQuery.data.feedback_paused} onClick={() => runRerun('weekly')} title={`下次周周期：${formatTime(feedbackQuery.data.next_runs.weekly)}`}>
                    <CalendarClock />周
                  </Button>
                  <Button variant="ghost" size="xs" disabled={rerunMutation.isPending || feedbackQuery.data.feedback_paused} onClick={() => runRerun('monthly')} title={`下次月周期：${formatTime(feedbackQuery.data.next_runs.monthly)}`}>
                    <CalendarClock />月
                  </Button>
                </div>
              </div>
            ) : null}
          </div>
        )}
        <div className="mt-2 flex flex-wrap justify-end gap-1">
          {onEdit && (
            <Button variant="ghost" size="xs" onClick={() => onEdit(project)} aria-label={`编辑项目：${project.name}`}>
              <Pencil />
              编辑
            </Button>
          )}
          {onProfile && (platform === 'wechat' || platform === 'seednote' || platform === 'moments') && (
            <Button variant="ghost" size="xs" onClick={() => onProfile(project)} aria-label={`项目画像：${project.name}`}>
              <UserRound />
              项目画像
            </Button>
          )}
          <Button variant="ghost" size="xs" onClick={() => setMemoryOpen(true)} aria-label={`项目记忆：${project.name}`}>
            <Brain />
            记忆
          </Button>
          <Button
            variant="ghost"
            size="xs"
            onClick={() => setTopicPoolOpen(true)}
            aria-label={unusedTopics === undefined
              ? `选题池：${project.name}`
              : `选题池：${project.name}，剩余 ${unusedTopics} 个`}
          >
            <Lightbulb />
            选题池
            {unusedTopics !== undefined && (
              <span className="min-w-4 text-center tabular-nums text-foreground">{unusedTopics}</span>
            )}
          </Button>
          {!isArchived && onArchive && (
            <Button variant="ghost" size="xs" disabled={archiving} onClick={() => onArchive(project.id)} aria-label={`归档项目：${project.name}`}>
              <Archive />
              归档
            </Button>
          )}
          {isArchived && onRestore && (
            <Button variant="ghost" size="xs" disabled={restoring} onClick={() => onRestore(project.id)} aria-label={`恢复项目：${project.name}`}>
              <RotateCcw />
              恢复
            </Button>
          )}
        </div>
      </div>
      <TopicPoolDialog project={project} open={topicPoolOpen} onOpenChange={setTopicPoolOpen} />
      <ProjectMemoryDialog projectId={project.id} projectName={project.name} open={memoryOpen} onOpenChange={setMemoryOpen} />
      <Dialog open={monthlyRerunOpen} onOpenChange={setMonthlyRerunOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader><DialogTitle>重跑月度反馈</DialogTitle></DialogHeader>
          <div className="grid grid-cols-2 gap-3">
            <label className="space-y-1 text-sm"><span>开始日期</span><Input type="date" value={periodStart} onChange={(event) => setPeriodStart(event.target.value)} /></label>
            <label className="space-y-1 text-sm"><span>结束日期</span><Input type="date" value={periodEnd} onChange={(event) => setPeriodEnd(event.target.value)} /></label>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setMonthlyRerunOpen(false)}>取消</Button>
            <Button disabled={!periodStart || !periodEnd || rerunMutation.isPending} onClick={() => rerunMutation.mutate('monthly')}>检查并排队</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
