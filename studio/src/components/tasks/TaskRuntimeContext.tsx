import { useId, useState } from 'react'
import {
  CheckCircle2,
  CircleDashed,
  Clipboard,
  CloudCog,
  FileCheck2,
  Info,
  Layers3,
  Loader2,
  Network,
  TerminalSquare,
  UserRound,
  XCircle,
  type LucideIcon,
} from 'lucide-react'
import type { Project, Task, TaskFile } from '@/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'
import { contentTypeDisplayName, progressStageLabel, taskFailurePresentation } from '@/lib/labels'

type RuntimeStatus = 'ready' | 'starting' | 'failed' | 'recoverable' | 'unknown'
type ContextKey = 'profile' | 'execution' | 'artifacts' | 'connectivity'

export interface TaskRuntimeContextProps {
  task: Task
  project?: Project
  files: TaskFile[]
  onResume?: () => void
}

interface ContextItem {
  key: ContextKey
  label: string
  value: string
  detail: string
  status: RuntimeStatus
  icon: LucideIcon
  action?: string
}

const statusCopy: Record<RuntimeStatus, { label: string; className: string; icon: LucideIcon }> = {
  ready: { label: '已就绪', className: 'text-emerald-600', icon: CheckCircle2 },
  starting: { label: '建立中', className: 'text-amber-600', icon: Loader2 },
  failed: { label: '需要处理', className: 'text-destructive', icon: XCircle },
  recoverable: { label: '可恢复', className: 'text-amber-600', icon: Info },
  unknown: { label: '等待确认', className: 'text-muted-foreground', icon: CircleDashed },
}

function runtimeStatus(task: Task): RuntimeStatus {
  if (task.status === 'pending') return 'starting'
  if (task.status === 'failed' && task.outcome?.diagnostic?.recoverable) return 'recoverable'
  if (task.status === 'failed') return 'failed'
  if (task.status === 'running') return 'starting'
  return 'ready'
}

function profileSummary(task: Task, project?: Project) {
  const snapshot = task.project_snapshot
  const projectName = snapshot?.project_name || project?.name || '未设置项目'
  const profile = task.agent_profile_snapshot
  const profileLabel = profile?.display_name || profile?.profile_id || task.execution_profile || '当前配置'
  return { projectName, profileLabel }
}

function makeItems(task: Task, project: Project | undefined, files: TaskFile[]): ContextItem[] {
  const { projectName, profileLabel } = profileSummary(task, project)
  const delivered = files.filter((file) => file.state === 'delivered' || file.state === 'retained')
  const failed = task.outcome?.diagnostic?.code?.startsWith('artifact_') || task.outcome?.diagnostic?.code === 'artifact_manifest_failed'
  const executionStatus = runtimeStatus(task)
  const diagnostic = taskFailurePresentation(task)
  const recoveryStage = task.outcome?.diagnostic?.resume_point
  return [
    {
      key: 'profile', label: '创作身份', value: projectName,
      detail: `${profileLabel} · 任务已冻结`, status: 'ready', icon: UserRound,
    },
    {
      key: 'execution', label: '执行环境', value: executionStatus === 'starting' ? '建立中' : executionStatus === 'failed' ? '未建立' : '已建立',
      detail: task.lifecycle?.execution_id ? `可${task.status === 'failed' || task.status === 'cancelled' ? '恢复' : '继续查看'}` : '工作区已绑定到本次任务',
      status: executionStatus, icon: CloudCog, action: recoveryStage ? `可从${progressStageLabel[recoveryStage] || recoveryStage}继续` : undefined,
    },
    {
      key: 'artifacts', label: '作品文件', value: `${delivered.length} 个已交付`,
      detail: failed ? '有文件需要处理' : delivered.length ? '产物清单已更新' : '等待产物',
      status: failed ? 'recoverable' : task.status === 'running' ? 'starting' : delivered.length ? 'ready' : 'unknown', icon: FileCheck2,
    },
    {
      key: 'connectivity', label: '连接状态', value: diagnostic ? '连接异常' : task.status === 'running' ? '模型和 MCP 连接中' : '模型和 MCP 正常',
      detail: diagnostic?.message || (task.last_heartbeat_at ? '最近心跳正常' : '暂无异常记录'),
      status: diagnostic ? 'failed' : task.status === 'running' ? 'starting' : 'ready', icon: Network,
    },
  ]
}

function CopyValue({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      aria-label={copied ? '已复制' : '复制'}
      onClick={() => {
        void navigator.clipboard.writeText(value).then(() => {
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1200)
        })
      }}
    >
      <Clipboard className="size-3.5" />
    </Button>
  )
}

function DetailRow({ label, value, copy }: { label: string; value: string; copy?: boolean }) {
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-3 border-b border-border py-2.5 last:border-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 items-center gap-1 break-words text-sm text-foreground">
        <span className="min-w-0 break-words">{value}</span>
        {copy ? <CopyValue value={value} /> : null}
      </dd>
    </div>
  )
}

function RuntimeDetail({ item, task, project, files, onResume }: { item: ContextItem; task: Task; project?: Project; files: TaskFile[]; onResume?: () => void }) {
  const { projectName, profileLabel } = profileSummary(task, project)
  const snapshot = task.project_snapshot
  const status = statusCopy[item.status]
  const StatusIcon = status.icon
  const artifactNames = files.slice(0, 12).map((file) => file.file_name).join('、') || '暂无'
  const technicalValue = item.key === 'execution' ? task.lifecycle?.execution_id : item.key === 'profile' ? task.agent_profile_fingerprint : item.key === 'artifacts' ? artifactNames : task.outcome?.diagnostic?.code
  return (
    <div className="space-y-4">
      <div className="flex items-start gap-3 rounded-lg border border-border bg-muted/25 p-3">
        <StatusIcon className={cn('mt-0.5 size-5 shrink-0', status.className, item.status === 'starting' && 'animate-spin')} />
        <div className="min-w-0">
          <p className="font-medium text-foreground">{item.value}</p>
          <p className="mt-1 text-sm text-muted-foreground">{item.detail}</p>
          {item.action ? <p className="mt-2 text-sm text-amber-700 dark:text-amber-400">{item.action}</p> : null}
        </div>
      </div>
      {item.key === 'profile' ? (
        <dl>
          <DetailRow label="项目" value={projectName} />
          <DetailRow label="执行配置" value={profileLabel} />
          <DetailRow label="平台" value={contentTypeDisplayName(snapshot?.platform || task.type)} />
          <DetailRow label="状态" value="任务继续执行时仍使用冻结版本" />
          <DetailRow label="指纹" value={task.agent_profile_fingerprint || '未记录'} copy={Boolean(task.agent_profile_fingerprint)} />
        </dl>
      ) : null}
      {item.key === 'execution' ? (
        <dl>
          <DetailRow label="执行 ID" value={task.lifecycle?.execution_id || '尚未建立'} copy={Boolean(task.lifecycle?.execution_id)} />
          <DetailRow label="工作区" value="已绑定到本次任务" />
          <DetailRow label="最近心跳" value={task.last_heartbeat_at ? new Date(task.last_heartbeat_at).toLocaleString('zh-CN') : '暂无记录'} />
          {onResume && (task.status === 'failed' || task.status === 'cancelled') ? <Button size="sm" onClick={onResume}>继续执行</Button> : null}
        </dl>
      ) : null}
      {item.key === 'artifacts' ? (
        <dl>
          <DetailRow label="文件" value={artifactNames} />
          <DetailRow label="已交付" value={`${files.filter((file) => file.state === 'delivered' || file.state === 'retained').length} 个`} />
          <DetailRow label="说明" value="文件路径和校验信息仅在技术详情中使用" />
        </dl>
      ) : null}
      {item.key === 'connectivity' ? (
        <dl>
          <DetailRow label="服务" value="模型、MCP 与任务执行服务" />
          <DetailRow label="诊断" value={technicalValue || '暂无诊断代码'} copy={Boolean(technicalValue)} />
          <DetailRow label="安全" value="凭据和完整 endpoint 已隐藏" />
        </dl>
      ) : null}
      {technicalValue ? (
        <Collapsible>
          <CollapsibleTrigger className="flex w-full items-center gap-2 text-left text-xs text-muted-foreground hover:text-foreground">
            <TerminalSquare className="size-3.5" /> 查看技术详情
          </CollapsibleTrigger>
          <CollapsibleContent className="mt-2 rounded-md bg-muted p-3 font-mono text-[11px] text-muted-foreground break-all">
            {technicalValue}
          </CollapsibleContent>
        </Collapsible>
      ) : null}
    </div>
  )
}

export function TaskRuntimeContext({ task, project, files, onResume }: TaskRuntimeContextProps) {
  const [selected, setSelected] = useState<ContextKey | null>(null)
  const titleId = useId()
  const items = makeItems(task, project, files)
  const selectedItem = items.find((item) => item.key === selected)
  return (
    <>
      <section aria-labelledby={titleId} className="rounded-lg border border-border bg-card">
        <div className="flex items-center justify-between border-b border-border px-3 py-2.5">
          <div className="flex items-center gap-2">
            <Layers3 className="size-4 text-muted-foreground" />
            <h2 id={titleId} className="text-sm font-semibold text-foreground">运行上下文</h2>
          </div>
          <Badge variant="outline" className="text-[11px] font-normal">任务专属</Badge>
        </div>
        <div className="grid divide-y divide-border sm:grid-cols-2 sm:divide-x sm:divide-y-0 xl:grid-cols-4">
          {items.map((item) => {
            const Icon = item.icon
            const status = statusCopy[item.status]
            const StatusIcon = status.icon
            const summaryDetail = item.status === 'failed' || item.status === 'recoverable' ? '点击查看详情' : item.detail
            return (
              <button key={item.key} type="button" aria-label={`${item.label}详情`} className="group min-w-0 px-3 py-3 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring" onClick={() => setSelected(item.key)}>
                <div className="flex items-center gap-1.5 text-xs text-muted-foreground"><Icon className="size-3.5" />{item.label}</div>
                <div className="mt-2 flex min-w-0 items-center gap-1.5"><StatusIcon className={cn('size-4 shrink-0', status.className, item.status === 'starting' && 'animate-spin')} /><span className="truncate text-sm font-medium text-foreground">{item.value}</span></div>
                <p className="mt-1 truncate text-xs text-muted-foreground" title={item.status === 'failed' || item.status === 'recoverable' ? item.detail : undefined}>{summaryDetail}</p>
              </button>
            )
          })}
        </div>
      </section>
      <Sheet open={Boolean(selectedItem)} onOpenChange={(open) => { if (!open) setSelected(null) }}>
        <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-md">
          {selectedItem ? <>
            <SheetHeader className="border-b border-border px-5 pb-4">
              <SheetTitle className="flex items-center gap-2"><selectedItem.icon className="size-5 text-primary" />{selectedItem.label}</SheetTitle>
              <SheetDescription>本次任务的运行信息和可执行操作</SheetDescription>
            </SheetHeader>
            <div className="px-5 pb-6"><RuntimeDetail item={selectedItem} task={task} project={project} files={files} onResume={onResume} /></div>
          </> : null}
        </SheetContent>
      </Sheet>
    </>
  )
}

export default TaskRuntimeContext
