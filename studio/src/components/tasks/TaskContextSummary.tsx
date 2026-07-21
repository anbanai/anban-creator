import { useId } from 'react'
import {
  ChevronRight,
  ClipboardList,
  Paperclip,
  ScrollText,
  SlidersHorizontal,
  type LucideIcon,
} from 'lucide-react'
import type { TaskDetailsTab } from '@/components/tasks/TaskDetailsSheet'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { contentTypeLabel } from '@/lib/labels'
import { cn } from '@/lib/utils'
import type { Project, Task, TaskFile } from '@/types'

export interface TaskContextSummaryProps {
  task: Task
  project?: Project
  files: TaskFile[]
  logs: string[]
  progressDescription: string | null
  sseError: string | null
  onOpenTab: (tab: TaskDetailsTab) => void
}

interface SummaryItemProps {
  actionLabel: string
  detail: string
  detailSuffix?: string
  descriptionId: string
  icon: LucideIcon
  index: number
  label: string
  neutral?: boolean
  onClick: () => void
  value: string
}

const MARKDOWN_PREFIX = /^(?:[#>*_~`+-]+\s*)+/

function latestNormalizedLogLine(logs: string[]) {
  for (let logIndex = logs.length - 1; logIndex >= 0; logIndex -= 1) {
    const lines = logs[logIndex].split(/\r?\n/)
    for (let lineIndex = lines.length - 1; lineIndex >= 0; lineIndex -= 1) {
      const normalized = lines[lineIndex].trim().replace(MARKDOWN_PREFIX, '').trim()
      if (normalized) return Array.from(normalized).slice(0, 80).join('')
    }
  }
  return ''
}

function SummaryItem({
  actionLabel,
  detail,
  detailSuffix,
  descriptionId,
  icon: Icon,
  index,
  label,
  neutral = false,
  onClick,
  value,
}: SummaryItemProps) {
  return (
    <Button
      type="button"
      variant="ghost"
      aria-label={actionLabel}
      aria-describedby={descriptionId}
      className={cn(
        'h-24 min-w-0 flex-col items-start justify-start gap-1.5 overflow-hidden rounded-none px-3 py-2 text-left',
        index === 0 && 'border-r border-b border-r-border border-b-border lg:border-b-0',
        index === 1 && 'border-b border-b-border lg:border-r lg:border-r-border lg:border-b-0',
        index === 2 && 'border-r border-r-border',
      )}
      onClick={onClick}
    >
      <span className="flex w-full min-w-0 items-center gap-1.5 text-xs font-normal text-muted-foreground">
        <Icon data-icon="inline-start" />
        <span className="truncate">{label}</span>
      </span>
      <span id={descriptionId} className="flex w-full min-w-0 flex-col items-start gap-1">
        <span className="w-full truncate text-sm font-medium text-foreground" title={value}>
          {value}
        </span>
        <span
          className={cn(
            'flex w-full min-w-0 items-center gap-1 truncate text-xs font-normal text-muted-foreground',
            neutral && 'italic',
          )}
          title={detail}
        >
          <span className="min-w-0 truncate">{detail}</span>
          {detailSuffix ? (
            <>
              <span aria-hidden="true">·</span>
              <span className="shrink-0">{detailSuffix}</span>
            </>
          ) : null}
        </span>
      </span>
    </Button>
  )
}

function taskLogState(task: Task, sseError: string | null) {
  if (task.status === 'running') return sseError ? '连接中断' : '实时'
  if (task.status === 'pending') return '等待执行'
  return '已结束'
}

export function TaskContextSummary({
  task,
  project,
  files,
  logs,
  progressDescription,
  sseError,
  onOpenTab,
}: TaskContextSummaryProps) {
  const descriptionIdPrefix = useId()
  const snapshot = task.project_snapshot
  const hasSnapshot = Boolean(snapshot?.platform)
  const projectName = hasSnapshot ? snapshot?.project_name || '—' : project?.name || '—'
  const platform = hasSnapshot ? snapshot?.platform || task.type : project?.platform || task.type
  const visualStyle = hasSnapshot
    ? snapshot?.visual_style
    : task.overrides?.visual_style || project?.visual_style
  const imageRatio = task.image_ratio || (hasSnapshot ? snapshot?.image_ratio : project?.image_ratio)
  const configurationDetail = [visualStyle, imageRatio].filter(Boolean).join(' · ') || '未设置'
  const attachmentCount = task.input_attachments?.length ?? 0
  const hasReferenceSummary = files.some((file) => file.file_name === 'reference-usage-summary.json')
  const progress = progressDescription?.trim()
  const logDetail = task.status === 'running' && progress
    ? progress
    : latestNormalizedLogLine(logs) || '暂无日志'
  const logState = taskLogState(task, sseError)

  return (
    <Card role="region" aria-label="任务上下文" size="sm">
      <CardHeader className="border-b border-border">
        <CardTitle>
          <h2>任务上下文</h2>
        </CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <div
          data-testid="task-context-grid"
          className="grid grid-cols-2 gap-0 lg:grid-cols-4"
        >
          <SummaryItem
            actionLabel="打开任务概览"
            descriptionId={`${descriptionIdPrefix}-overview`}
            icon={ClipboardList}
            index={0}
            label="任务概览"
            value={projectName}
            detail={task.plan_id ? '计划任务' : '手动创建'}
            detailSuffix={`固定价 ${task.billing_price_credits.toLocaleString()} 积分`}
            onClick={() => onOpenTab('overview')}
          />
          <SummaryItem
            actionLabel="打开创作配置"
            descriptionId={`${descriptionIdPrefix}-configuration`}
            icon={SlidersHorizontal}
            index={1}
            label="创作配置"
            value={contentTypeLabel[platform] || platform}
            detail={configurationDetail}
            neutral={configurationDetail === '未设置'}
            onClick={() => onOpenTab('configuration')}
          />
          <SummaryItem
            actionLabel="打开参考素材"
            descriptionId={`${descriptionIdPrefix}-materials`}
            icon={Paperclip}
            index={2}
            label="参考素材"
            value={`${attachmentCount} 项输入`}
            detail={hasReferenceSummary ? '已生成使用结论' : '仅任务输入'}
            onClick={() => onOpenTab('materials')}
          />
          <SummaryItem
            actionLabel="打开执行日志"
            descriptionId={`${descriptionIdPrefix}-logs`}
            icon={ScrollText}
            index={3}
            label="执行日志"
            value={`${logs.length} 条 · ${logState}`}
            detail={logDetail}
            onClick={() => onOpenTab('logs')}
          />
        </div>
      </CardContent>
      <CardFooter className="justify-end">
        <Button type="button" variant="ghost" size="xs" onClick={() => onOpenTab('overview')}>
          更多详情
          <ChevronRight data-icon="inline-end" />
        </Button>
      </CardFooter>
    </Card>
  )
}

export default TaskContextSummary
