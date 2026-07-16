import { useEffect, type ReactNode, type RefObject } from 'react'
import {
  AlertTriangle,
  CalendarClock,
  Copy,
  FolderKanban,
  Images,
  Pause,
  Play,
  ReceiptText,
  RefreshCw,
  ScrollText,
  Settings2,
  type LucideIcon,
} from 'lucide-react'
import { Streamdown } from 'streamdown'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import ReferenceUsageSummary from '@/components/tasks/ReferenceUsageSummary'
import { TaskConfigurationDetails } from '@/components/tasks/TaskConfigurationDetails'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatFullDateTimeCN, statusBadgeVariant, taskStatusLabel } from '@/lib/labels'
import { cn } from '@/lib/utils'
import type { Project, Task, TaskFile } from '@/types'

export type TaskDetailsTab = 'overview' | 'configuration' | 'materials' | 'logs'

export interface TaskDetailsSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  selectedTab: TaskDetailsTab
  onTabChange: (tab: TaskDetailsTab) => void
  task: Task
  project?: Project
  files: TaskFile[]
  netConsumedCredits: number
  showCreditDetails: boolean
  onOpenCreditDetails: () => void
  logs: string[]
  sseError: string | null
  autoScrollLogs: boolean
  onToggleAutoScroll: () => void
  onCopyLogs: () => void
  onReconnectLogs: () => void
  logContainerRef: RefObject<HTMLDivElement | null>
}

interface TaskOverviewDetailsProps {
  task: Task
  project?: Project
  netConsumedCredits: number
  showCreditDetails: boolean
  onOpenCreditDetails: () => void
}

interface TaskLogDetailsProps {
  logs: string[]
  logMarkdown: string
  sseError: string | null
  autoScrollLogs: boolean
  onToggleAutoScroll: () => void
  onCopyLogs: () => void
  onReconnectLogs: () => void
  logContainerRef: RefObject<HTMLDivElement | null>
}

interface TaskDetailsSectionProps {
  label: string
  title: string
  icon: LucideIcon
  className?: string
  children: ReactNode
}

function TaskDetailsSection({
  label,
  title,
  icon: Icon,
  className,
  children,
}: TaskDetailsSectionProps) {
  return (
    <section
      aria-label={label}
      className={cn('rounded-lg border border-border bg-card/60 p-4', className)}
    >
      <div className="mb-4 flex shrink-0 items-center gap-2">
        <span className="rounded-md bg-primary/10 p-2 text-primary">
          <Icon aria-hidden="true" className="size-4" />
        </span>
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
      </div>
      {children}
    </section>
  )
}

function DetailRows({ rows }: { rows: Array<[string, string]> }) {
  return (
    <dl className="flex flex-col">
      {rows.map(([label, value]) => (
        <div
          key={label}
          className="grid grid-cols-[6rem_minmax(0,1fr)] gap-3 border-b border-border py-3 first:pt-0 last:border-b-0 last:pb-0"
        >
          <dt className="text-xs text-muted-foreground">{label}</dt>
          <dd className="min-w-0 break-words text-sm text-foreground">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function TaskOverviewDetails({
  task,
  project,
  netConsumedCredits,
  showCreditDetails,
  onOpenCreditDetails,
}: TaskOverviewDetailsProps) {
  const hasSnapshot = Boolean(task.project_snapshot?.platform)
  const projectName = hasSnapshot
    ? task.project_snapshot?.project_name || '—'
    : project?.name || '—'
  const timingRows: Array<[string, string]> = [
    ['创建时间', formatFullDateTimeCN(task.created_at)],
    ['开始时间', formatFullDateTimeCN(task.started_at)],
    ['完成时间', formatFullDateTimeCN(task.completed_at)],
    ['来源', task.plan_id ? '计划任务' : '手动创建'],
  ]

  return (
    <div className="flex flex-col gap-4">
      <TaskDetailsSection
        label="任务时间与来源"
        title="任务时间与来源"
        icon={CalendarClock}
      >
        <DetailRows rows={timingRows} />
      </TaskDetailsSection>
      <TaskDetailsSection label="项目与积分" title="项目与积分" icon={FolderKanban}>
        <div className="flex flex-col gap-3">
          <DetailRows rows={[['项目', projectName]]} />
          <div className="flex items-center justify-between gap-3">
            <div className="flex min-w-0 flex-col gap-1">
              <p className="text-xs text-muted-foreground">积分消耗</p>
              <p className="text-sm font-medium tabular-nums">
                {netConsumedCredits.toLocaleString()}
              </p>
            </div>
            {showCreditDetails ? (
              <Button size="sm" variant="ghost" onClick={onOpenCreditDetails}>
                <ReceiptText data-icon="inline-start" />
                查看明细
              </Button>
            ) : null}
          </div>
        </div>
      </TaskDetailsSection>
    </div>
  )
}

function CompactMaterialError() {
  return (
    <Alert>
      <AlertTriangle />
      <AlertTitle>参考素材摘要暂时无法显示</AlertTitle>
      <AlertDescription>任务状态与生成文件不受影响，可稍后重试。</AlertDescription>
    </Alert>
  )
}

function TaskLogDetails({
  logs,
  logMarkdown,
  sseError,
  autoScrollLogs,
  onToggleAutoScroll,
  onCopyLogs,
  onReconnectLogs,
  logContainerRef,
}: TaskLogDetailsProps) {
  useEffect(() => {
    if (!autoScrollLogs || !logContainerRef.current) return
    logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
  }, [autoScrollLogs, logContainerRef, logs])

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">{logs.length} 条</span>
        <div className="flex items-center gap-1">
          <Button
            size="xs"
            variant="ghost"
            aria-pressed={autoScrollLogs}
            onClick={onToggleAutoScroll}
          >
            {autoScrollLogs
              ? <Play data-icon="inline-start" />
              : <Pause data-icon="inline-start" />}
            {autoScrollLogs ? '跟随输出' : '暂停跟随'}
          </Button>
          <Button
            size="xs"
            variant="ghost"
            aria-label="复制日志"
            disabled={logs.length === 0}
            onClick={onCopyLogs}
          >
            <Copy data-icon="inline-start" />
            复制
          </Button>
        </div>
      </div>

      {sseError ? (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle>日志连接中断</AlertTitle>
          <AlertDescription className="flex min-w-0 flex-col items-stretch gap-2 sm:flex-row sm:items-center sm:justify-between">
            <span className="min-w-0 break-words">{sseError}</span>
            <Button
              size="xs"
              variant="ghost"
              className="shrink-0 self-start sm:self-auto"
              onClick={onReconnectLogs}
            >
              <RefreshCw data-icon="inline-start" />
              重新连接
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      <div
        ref={logContainerRef}
        className="min-h-28 flex-1 overflow-y-auto rounded-md bg-muted/30 p-3"
      >
        {logs.length === 0 ? (
          <Empty className="min-h-28 border-0 p-3">
            <EmptyHeader>
              <EmptyMedia variant="icon"><ScrollText /></EmptyMedia>
              <EmptyTitle>等待输出中...</EmptyTitle>
              <EmptyDescription>0 条日志</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Streamdown mode="streaming" className="prose prose-sm max-w-none dark:prose-invert">
            {logMarkdown}
          </Streamdown>
        )}
      </div>
    </div>
  )
}

export function TaskDetailsSheet(props: TaskDetailsSheetProps) {
  const logMarkdown = props.logs.join('  \n')
  const hasSnapshot = Boolean(props.task.project_snapshot?.platform)
  const projectName = hasSnapshot
    ? props.task.project_snapshot?.project_name || '未设置项目'
    : props.project?.name || '未设置项目'
  const taskTitle = props.task.title || props.task.topic || props.task.prompt

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent
        side="right"
        className="gap-0 overflow-hidden p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl"
      >
        <SheetHeader className="shrink-0 border-b border-border px-4 py-3 pr-12">
          <SheetTitle>任务详情</SheetTitle>
          <SheetDescription className="truncate text-xs" title={taskTitle}>
            {taskTitle}
          </SheetDescription>
          <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
            <Badge variant={statusBadgeVariant(props.task.status)}>
              {taskStatusLabel[props.task.status] || props.task.status}
            </Badge>
            <span className="min-w-0 truncate" title={projectName}>{projectName}</span>
          </div>
        </SheetHeader>
        <Tabs
          value={props.selectedTab}
          onValueChange={(value) => props.onTabChange(value as TaskDetailsTab)}
          className="min-h-0 flex-1 gap-0 overflow-hidden"
        >
          <TabsList
            variant="line"
            className="w-full shrink-0 justify-start overflow-x-auto border-b border-border px-4 py-2"
          >
            <TabsTrigger
              value="overview"
              className="data-active:text-primary data-active:after:bg-primary"
            >
              概览
            </TabsTrigger>
            <TabsTrigger
              value="configuration"
              className="data-active:text-primary data-active:after:bg-primary"
            >
              配置
            </TabsTrigger>
            <TabsTrigger
              value="materials"
              className="data-active:text-primary data-active:after:bg-primary"
            >
              素材
            </TabsTrigger>
            <TabsTrigger
              value="logs"
              className="data-active:text-primary data-active:after:bg-primary"
            >
              日志
            </TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="min-h-0 overflow-y-auto p-4">
            <TaskOverviewDetails
              task={props.task}
              project={props.project}
              netConsumedCredits={props.netConsumedCredits}
              showCreditDetails={props.showCreditDetails}
              onOpenCreditDetails={props.onOpenCreditDetails}
            />
          </TabsContent>
          <TabsContent value="configuration" className="min-h-0 overflow-y-auto p-4">
            <TaskDetailsSection label="创作配置详情" title="创作配置" icon={Settings2}>
              <TaskConfigurationDetails task={props.task} project={props.project} />
            </TaskDetailsSection>
          </TabsContent>
          <TabsContent value="materials" className="min-h-0 overflow-y-auto p-4">
            <TaskDetailsSection label="参考素材详情" title="参考素材" icon={Images}>
              <ErrorBoundary
                key={`task-details-materials-${props.task.id}`}
                fallback={<CompactMaterialError />}
              >
                <ReferenceUsageSummary variant="compact" task={props.task} files={props.files} />
              </ErrorBoundary>
            </TaskDetailsSection>
          </TabsContent>
          <TabsContent value="logs" className="flex min-h-0 flex-col overflow-hidden p-4">
            <TaskDetailsSection
              label="执行动态"
              title="执行动态"
              icon={ScrollText}
              className="flex min-h-0 flex-1 flex-col overflow-hidden"
            >
              <TaskLogDetails
                logs={props.logs}
                logMarkdown={logMarkdown}
                sseError={props.sseError}
                autoScrollLogs={props.autoScrollLogs}
                onToggleAutoScroll={props.onToggleAutoScroll}
                onCopyLogs={props.onCopyLogs}
                onReconnectLogs={props.onReconnectLogs}
                logContainerRef={props.logContainerRef}
              />
            </TaskDetailsSection>
          </TabsContent>
        </Tabs>
      </SheetContent>
    </Sheet>
  )
}

export default TaskDetailsSheet
