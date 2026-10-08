import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Archive, Settings2, Brain, CalendarClock, ChevronDown, ChevronUp, Database, Lightbulb, Pause, Play, RefreshCw, RotateCcw, SquarePen, UserRound } from 'lucide-react'
import type { Project, ProjectStats } from '@/types'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { TopicPoolDialog } from '@/components/TopicPoolDialog'
import { ProjectMemoryDialog } from '@/components/projects/ProjectMemoryDialog'
import { ImageAnalysisBadge } from '@/components/image-analysis/ImageAnalysisBadge'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'
import { MetricStrip, ProgressRing, StatusPill } from '@/components/workspace'

interface ProjectCardProps {
  project: Project
  stats?: ProjectStats
  onEdit?: (project: Project) => void
  onChannelConfig?: (project: Project) => void
  onProfile?: (project: Project) => void
  archiving?: boolean
  restoring?: boolean
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
}

export function ProjectCard({ project, stats, onEdit, onProfile, onChannelConfig, archiving, restoring, onArchive, onRestore }: ProjectCardProps) {
  const [topicPoolOpen, setTopicPoolOpen] = useState(false)
  const [memoryOpen, setMemoryOpen] = useState(false)
  const [monthlyRerunOpen, setMonthlyRerunOpen] = useState(false)
  const [feedbackOpen, setFeedbackOpen] = useState(false)
  const [periodStart, setPeriodStart] = useState('')
  const [periodEnd, setPeriodEnd] = useState('')
  const queryClient = useQueryClient()
  const platform = project.platform
  const isArchived = project.status === 'archived'
  const unusedTopics = stats?.unused_topics
  const completionRate = stats && stats.total_tasks > 0
    ? Math.round((stats.completed_tasks / stats.total_tasks) * 100)
    : null
  const cardTone = isArchived
    ? 'border-border/60 bg-muted/30'
    : 'border-border bg-card hover:border-foreground/20 hover:shadow-sm'
  const feedbackEnabled = platform === '' || platform === 'wechat' || platform === 'seednote'
  const feedbackQuery = useQuery({
    queryKey: ['project-feedback', project.id],
    queryFn: ({ signal }) => api.projects.feedback(project.id, signal),
    enabled: feedbackEnabled && feedbackOpen,
    staleTime: 60_000,
    retry: false,
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
          <h3 className="min-w-0 flex-1">
            <ProjectIdentity project={project} />
          </h3>
        </div>
        <div className="flex flex-col items-end gap-1">
          {isArchived && <StatusPill status="archived" label="已归档" />}
          <ImageAnalysisBadge analysis={project.image_analysis} />
        </div>
      </div>
      <div className="mt-auto pt-4">
        {stats && (
          <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-3 border-t border-border pt-3">
            {completionRate === null ? (
              <div className="flex size-16 items-center justify-center rounded-full border border-dashed border-border text-center text-[11px] leading-tight text-muted-foreground">
                暂无任务
              </div>
            ) : (
              <ProgressRing value={completionRate} label="任务完成率" size={64} />
            )}
            <div className="min-w-0 space-y-2">
              <MetricStrip
                testId="project-overview-strip"
                metrics={[
                  { label: '任务', value: stats.total_tasks },
                  { label: '已完成', value: stats.completed_tasks },
                  { label: '失败', value: stats.failed_tasks },
                ]}
              />
              <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground" aria-label="项目活动">
                {stats.running_tasks > 0 && <span>{stats.running_tasks} 个运行中</span>}
                {stats.pending_tasks > 0 && <span>{stats.pending_tasks} 个待执行</span>}
              </div>
            </div>
          </div>
        )}
        {feedbackEnabled && (
          <div className="mt-3 border-t border-border pt-3">
            {!feedbackOpen ? (
              <Button variant="ghost" size="xs" className="min-h-11" onClick={() => setFeedbackOpen(true)} aria-label="查看反馈闭环">
                <Database />反馈闭环<ChevronDown />
              </Button>
            ) : (
              <>
                <Button variant="ghost" size="xs" className="min-h-11" onClick={() => setFeedbackOpen(false)} aria-expanded="true">
                  <Database />反馈闭环<ChevronUp />
                </Button>
                {feedbackQuery.isPending ? (
                  <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground"><Database className="h-3.5 w-3.5" />加载反馈状态…</div>
                ) : feedbackQuery.isError ? (
                  <div className="mt-2 flex items-center justify-between gap-2 text-xs text-muted-foreground">
                    <span>反馈状态暂不可用</span>
                    <Button variant="ghost" size="xs" className="min-h-11" onClick={() => void feedbackQuery.refetch()} aria-label="重新加载反馈状态"><RefreshCw />重试</Button>
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
                      <Button variant="ghost" size="xs" className="min-h-11" disabled={feedbackStateMutation.isPending} onClick={() => feedbackStateMutation.mutate(!feedbackQuery.data!.feedback_paused)}>
                        {feedbackQuery.data.feedback_paused ? <Play /> : <Pause />}
                        {feedbackQuery.data.feedback_paused ? '恢复' : '暂停'}
                      </Button>
                      <Button variant="ghost" size="xs" className="min-h-11" disabled={rerunMutation.isPending || feedbackQuery.data.feedback_paused} onClick={() => runRerun('daily')} title={`下次日周期：${formatTime(feedbackQuery.data.next_runs.daily)}`}>
                        <RefreshCw />日
                      </Button>
                      <Button variant="ghost" size="xs" className="min-h-11" disabled={rerunMutation.isPending || feedbackQuery.data.feedback_paused} onClick={() => runRerun('weekly')} title={`下次周周期：${formatTime(feedbackQuery.data.next_runs.weekly)}`}>
                        <CalendarClock />周
                      </Button>
                      <Button variant="ghost" size="xs" className="min-h-11" disabled={rerunMutation.isPending || feedbackQuery.data.feedback_paused} onClick={() => runRerun('monthly')} title={`下次月周期：${formatTime(feedbackQuery.data.next_runs.monthly)}`}>
                        <CalendarClock />月
                      </Button>
                    </div>
                  </div>
                ) : null}
              </>
            )}
          </div>
        )}
        <div className="mt-2 flex flex-wrap justify-end gap-1" aria-label={`项目操作：${project.name}`}>
          {onEdit && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => onEdit(project)} aria-label={`编辑项目：${project.name}`} />}>
                <SquarePen />
              </TooltipTrigger>
              <TooltipContent>编辑</TooltipContent>
            </Tooltip>
          )}
          {onProfile && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => onProfile(project)} aria-label={`项目画像：${project.name}`} />}>
                <UserRound />
              </TooltipTrigger>
              <TooltipContent>项目画像</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => setTopicPoolOpen(true)} aria-label={unusedTopics === undefined ? `选题池：${project.name}` : `选题池：${project.name}，剩余 ${unusedTopics} 个`} />}>
              <Lightbulb />
            </TooltipTrigger>
            <TooltipContent>{unusedTopics === undefined ? '选题池' : `选题池：剩余 ${unusedTopics} 个`}</TooltipContent>
          </Tooltip>
          {onChannelConfig && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => onChannelConfig(project)} aria-label={`渠道配置：${project.name}`} />}>
                <Settings2 />
              </TooltipTrigger>
              <TooltipContent>渠道配置</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger render={<Button variant="ghost" size="icon-md" onClick={() => setMemoryOpen(true)} aria-label={`项目记忆：${project.name}`} />}>
              <Brain />
            </TooltipTrigger>
            <TooltipContent>项目记忆</TooltipContent>
          </Tooltip>
          {!isArchived && onArchive && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" disabled={archiving} onClick={() => onArchive(project.id)} aria-label={`归档项目：${project.name}`} />}>
                <Archive />
              </TooltipTrigger>
              <TooltipContent>归档</TooltipContent>
            </Tooltip>
          )}
          {isArchived && onRestore && (
            <Tooltip>
              <TooltipTrigger render={<Button variant="ghost" size="icon-md" disabled={restoring} onClick={() => onRestore(project.id)} aria-label={`恢复项目：${project.name}`} />}>
                <RotateCcw />
              </TooltipTrigger>
              <TooltipContent>恢复</TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>
      <TopicPoolDialog project={project} open={topicPoolOpen} onOpenChange={setTopicPoolOpen} />
      <ProjectMemoryDialog projectId={project.id} projectName={project.name} project={project} open={memoryOpen} onOpenChange={setMemoryOpen} />
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
