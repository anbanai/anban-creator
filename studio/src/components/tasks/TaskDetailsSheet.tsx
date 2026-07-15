import { useEffect, useState, type RefObject } from 'react'
import { AlertTriangle, Copy, Pause, Play, ReceiptText, RefreshCw, ScrollText } from 'lucide-react'
import { Streamdown } from 'streamdown'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import ReferenceUsageSummary from '@/components/tasks/ReferenceUsageSummary'
import { TaskConfigurationDetails } from '@/components/tasks/TaskConfigurationDetails'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { formatFullDateTimeCN } from '@/lib/labels'
import type { Project, Task, TaskFile } from '@/types'

type TaskDetailsTab = 'overview' | 'configuration' | 'materials' | 'logs'

export interface TaskDetailsSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
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
  const rows = [
    ['创建时间', formatFullDateTimeCN(task.created_at)],
    ['开始时间', formatFullDateTimeCN(task.started_at)],
    ['完成时间', formatFullDateTimeCN(task.completed_at)],
    ['来源', task.plan_id ? '计划任务' : '手动创建'],
    ['项目', projectName],
  ]

  return (
    <div className="flex flex-col gap-4">
      <dl className="flex flex-col border-y border-border">
        {rows.map(([label, value]) => (
          <div
            key={label}
            className="grid grid-cols-[6rem_minmax(0,1fr)] gap-3 border-b border-border py-3 last:border-b-0"
          >
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words text-sm text-foreground">{value}</dd>
          </div>
        ))}
      </dl>
      <div className="flex items-center justify-between gap-3 border-b border-border pb-3">
        <div className="flex flex-col gap-1">
          <p className="text-xs text-muted-foreground">积分消耗</p>
          <p className="text-sm font-medium tabular-nums">{netConsumedCredits.toLocaleString()}</p>
        </div>
        {showCreditDetails ? (
          <Button size="sm" variant="ghost" onClick={onOpenCreditDetails}>
            <ReceiptText data-icon="inline-start" />
            查看明细
          </Button>
        ) : null}
      </div>
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
    <div className="flex min-h-full flex-col gap-3">
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
        className="min-h-40 flex-1 overflow-y-auto border-y border-border py-3"
      >
        {logs.length === 0 ? (
          <Empty className="min-h-40 border-0 p-4">
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
  const [tab, setTab] = useState<TaskDetailsTab>('overview')

  useEffect(() => {
    setTab('overview')
  }, [props.task.id])

  const logMarkdown = props.logs.join('  \n')

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent
        side="right"
        className="gap-0 overflow-hidden p-0 data-[side=right]:w-full sm:max-w-xl"
      >
        <SheetHeader className="shrink-0 border-b border-border px-4 py-3 pr-12">
          <SheetTitle>任务详情</SheetTitle>
          <SheetDescription className="sr-only">任务概览、配置、参考素材和执行日志</SheetDescription>
        </SheetHeader>
        <Tabs
          value={tab}
          onValueChange={(value) => setTab(value as TaskDetailsTab)}
          className="min-h-0 flex-1 gap-0 overflow-hidden"
        >
          <TabsList
            variant="line"
            className="w-full shrink-0 justify-start overflow-x-auto border-b border-border px-4 py-2"
          >
            <TabsTrigger value="overview">概览</TabsTrigger>
            <TabsTrigger value="configuration">配置</TabsTrigger>
            <TabsTrigger value="materials">素材</TabsTrigger>
            <TabsTrigger value="logs">日志</TabsTrigger>
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
            <TaskConfigurationDetails task={props.task} project={props.project} />
          </TabsContent>
          <TabsContent value="materials" className="min-h-0 overflow-y-auto p-4">
            <ErrorBoundary
              key={`task-details-materials-${props.task.id}`}
              fallback={<CompactMaterialError />}
            >
              <ReferenceUsageSummary variant="compact" task={props.task} files={props.files} />
            </ErrorBoundary>
          </TabsContent>
          <TabsContent value="logs" className="min-h-0 overflow-y-auto p-4">
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
          </TabsContent>
        </Tabs>
      </SheetContent>
    </Sheet>
  )
}

export default TaskDetailsSheet
