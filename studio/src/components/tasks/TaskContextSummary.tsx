import { useId } from 'react'
import {
  ClipboardList,
  Paperclip,
  SlidersHorizontal,
  type LucideIcon,
} from 'lucide-react'
import type { TaskDetailsTab } from '@/components/tasks/TaskDetailsSheet'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { Project, Task, TaskFile } from '@/types'

export interface TaskContextSummaryProps {
  compact?: boolean
  layout?: 'grid' | 'sidebar'
  showBillingDetail?: boolean
  task: Task
  project?: Project
  files: TaskFile[]
  onOpenTab: (tab: TaskDetailsTab) => void
}

interface SummaryItemProps {
  compact?: boolean
  layout?: 'grid' | 'sidebar'
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

function SummaryItem({
  compact,
  layout = 'grid',
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
        'min-h-11 min-w-0 flex-col items-start justify-start gap-1.5 overflow-hidden rounded-none px-3 py-2 text-left',
        compact ? 'h-auto' : 'h-24',
        layout === 'sidebar'
          ? index < 2 && 'border-b border-b-border'
          : cn(
              index === 0 && 'border-r border-b border-r-border border-b-border lg:border-b-0',
              index === 1 && 'border-b border-b-border lg:border-r lg:border-r-border lg:border-b-0',
              index === 2 && 'border-r border-r-border lg:border-r-0',
            ),
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

export function TaskContextSummary({
  compact = false,
  layout = 'grid',
  showBillingDetail = true,
  task,
  project,
  files,
  onOpenTab,
}: TaskContextSummaryProps) {
  const descriptionIdPrefix = useId()
  const snapshot = task.project_snapshot
  const hasSnapshot = Boolean(snapshot?.platform)
  const visualStyle = hasSnapshot
    ? snapshot?.visual_style
    : task.overrides?.visual_style || project?.visual_style
  const imageRatio = task.image_ratio || (hasSnapshot ? snapshot?.image_ratio : project?.image_ratio)
  const configurationDetail = imageRatio ? `画幅 ${imageRatio}` : '未指定画幅'
  const attachmentCount = task.input_attachments?.length ?? 0
  const hasReferenceSummary = files.some((file) => file.file_name === 'reference-usage-summary.json')

  return (
    <Card role="region" aria-label="关键事实" size="sm" className={compact ? 'gap-2 border-0 bg-transparent shadow-none ring-0' : undefined}>
      {(!compact || layout === 'sidebar') && <CardHeader className={cn('border-b border-border', compact && 'px-3 pb-3')}>
        <CardTitle>
          <h2>关键事实</h2>
        </CardTitle>
      </CardHeader>}
      <CardContent className="p-0">
        <div
          data-testid="task-context-grid"
          className={cn('grid gap-0', layout === 'sidebar' ? 'grid-cols-1' : 'grid-cols-2 lg:grid-cols-3')}
        >
          <SummaryItem
            compact={compact}
            layout={layout}
            actionLabel="打开任务概览"
            descriptionId={`${descriptionIdPrefix}-overview`}
            icon={ClipboardList}
            index={0}
            label="任务来源"
            value={task.plan_id ? '计划任务' : '手动创建'}
            detail="创建方式"
            detailSuffix={showBillingDetail ? `累计扣费 ${(task.billing_total_credits ?? task.billing_price_credits).toLocaleString()} 积分` : undefined}
            onClick={() => onOpenTab('overview')}
          />
          <SummaryItem
            compact={compact}
            layout={layout}
            actionLabel="打开创作配置"
            descriptionId={`${descriptionIdPrefix}-configuration`}
            icon={SlidersHorizontal}
            index={1}
            label="生成设置"
            value={visualStyle || '未设置风格'}
            detail={configurationDetail}
            neutral={!visualStyle && !imageRatio}
            onClick={() => onOpenTab('configuration')}
          />
          <SummaryItem
            compact={compact}
            layout={layout}
            actionLabel="打开参考素材"
            descriptionId={`${descriptionIdPrefix}-materials`}
            icon={Paperclip}
            index={2}
            label="参考素材"
            value={`${attachmentCount} 项输入`}
            detail={hasReferenceSummary ? '已生成使用结论' : '仅任务输入'}
            onClick={() => onOpenTab('materials')}
          />
        </div>
      </CardContent>
    </Card>
  )
}

export default TaskContextSummary
