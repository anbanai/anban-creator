import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Loader2, Play, Square, Trash2, Settings, Terminal, ClipboardCopy, ExternalLink, Activity } from 'lucide-react'
import { toast } from 'sonner'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { ReadinessChecklist } from '@/components/settings/ReadinessChecklist'
import {
  startLocalExecutor,
  stopLocalExecutor,
  setExecutorEnabled,
  getLocalExecutorStatus,
  copyLocalDiagnostics,
  openWorkspace,
} from '@/lib/tauri'
import {
  localExecutorStore,
  useLocalExecutorLogs,
  useLocalExecutorPanelOpen,
  useLocalExecutorStatus,
  type LogLevel,
} from '@/lib/local-executor-store'
import { localExecutorCreateHint } from '@/lib/local-executor-ux'
import { useAuth } from '@/contexts/AuthContext'

/**
 * Desktop-only live-log drawer. Surfaces the `local-run://event` stream the Rust
 * executor emits (lifecycle, task claims, agent stdout/stderr, errors) plus
 * start/stop controls. Without this the local executor runs invisibly — the
 * user had to open Settings and watch a 4s poll to know anything was happening.
 *
 * Wired to the event stream by LocalExecutorLayer; this component only renders.
 */
const LEVEL_CLASS: Record<LogLevel, string> = {
  info: 'text-foreground/80',
  warn: 'text-amber-600',
  error: 'text-red-600',
}

export default function LocalRunLogSheet() {
  const open = useLocalExecutorPanelOpen()
  const status = useLocalExecutorStatus()
  const logs = useLocalExecutorLogs()
  const navigate = useNavigate()
  const { user } = useAuth()
  const scrollRef = useRef<HTMLDivElement>(null)
  const [toggling, setToggling] = useState(false)

  // Keep the latest line in view as new events arrive (live tail).
  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [logs.length, open])

  const refreshStatus = async () => {
    const s = await getLocalExecutorStatus()
    localExecutorStore.setStatus(s)
  }

  const handleToggle = async () => {
    setToggling(true)
    try {
      if (status?.running) {
        await stopLocalExecutor()
        await setExecutorEnabled(false)
        toast.success('本地执行器已停止')
      } else {
        const ok = await startLocalExecutor()
        if (ok) {
          await setExecutorEnabled(true)
          toast.success('本地执行器已启动')
        } else {
          toast.error('无法启动：依赖未就绪，请先完成配置')
        }
      }
      await refreshStatus()
    } finally {
      setToggling(false)
    }
  }

  const handleCopyDiagnostics = async () => {
    const diagnostics = await copyLocalDiagnostics()
    if (!diagnostics) {
      toast.error('诊断信息不可用')
      return
    }
    await navigator.clipboard.writeText(diagnostics)
    toast.success('诊断信息已复制')
  }

  const handleOpenWorkspace = async () => {
    if (await openWorkspace()) toast.success('已打开工作区')
    else toast.error('无法打开工作区')
  }

  const running = status?.running ?? false
  const available = status?.available ?? false

  return (
    <Sheet open={open} onOpenChange={(o) => { if (!o) localExecutorStore.closePanel() }}>
      <SheetContent side="right" className="flex w-full flex-col p-0 sm:max-w-xl">
        <SheetHeader className="shrink-0 border-b">
          <div className="flex items-center justify-between gap-2 pr-8">
            <SheetTitle className="flex items-center gap-2">
              <Terminal className="h-4 w-4 text-cyan-500" /> Mission Control
              {running ? (
                <Badge variant="outline" className="text-[10px] text-emerald-600">运行中</Badge>
              ) : available ? (
                <Badge variant="outline" className="text-[10px]">已就绪</Badge>
              ) : (
                <Badge variant="outline" className="text-[10px] text-amber-600">未就绪</Badge>
              )}
            </SheetTitle>
            {user?.is_admin ? (
              <Button
                variant="ghost"
                size="icon-sm"
                title="打开设置"
                onClick={() => {
                  localExecutorStore.closePanel()
                  navigate('/settings')
                }}
              >
                <Settings className="h-4 w-4" />
              </Button>
            ) : null}
          </div>
          <SheetDescription className="sr-only">
            本地执行器实时日志与控制
          </SheetDescription>
        </SheetHeader>

        {/* Status summary + controls */}
        <div className="shrink-0 space-y-3 border-b px-4 py-3">
          <div className="rounded-md border border-cyan-500/20 bg-cyan-500/5 px-3 py-2">
            <p className="flex items-center gap-2 text-xs font-medium text-foreground">
              <Activity className="h-3.5 w-3.5 text-cyan-500" />
              {localExecutorCreateHint(status)}
            </p>
            {status?.current_task_id && (
              <button
                type="button"
                className="mt-1 text-xs text-primary hover:underline"
                onClick={() => {
                  localExecutorStore.closePanel()
                  navigate(`/tasks/${status.current_task_id}`)
                }}
              >
                查看当前任务 {status.current_task_id.slice(0, 8)}
              </button>
            )}
          </div>
          {status && !available && status.reason && (
            <p className="text-xs text-amber-600">{status.reason}</p>
          )}
          <ReadinessChecklist status={status} />
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant={running ? 'destructive' : 'default'}
              onClick={handleToggle}
              disabled={toggling || (!running && !available)}
            >
              {toggling ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : running ? (
                <Square className="h-4 w-4" />
              ) : (
                <Play className="h-4 w-4" />
              )}
              {running ? '停止' : '启动'}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={handleOpenWorkspace}
              disabled={!status?.workspace_valid}
              title="打开工作区"
            >
              <ExternalLink className="h-4 w-4" /> 工作区
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={handleCopyDiagnostics}
              title="复制诊断"
            >
              <ClipboardCopy className="h-4 w-4" /> 诊断
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => localExecutorStore.clearLogs()}
              disabled={logs.length === 0}
              title="清空日志"
            >
              <Trash2 className="h-4 w-4" /> 清空
            </Button>
          </div>
        </div>

        {/* Live log tail */}
        <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto bg-muted/20 p-3">
          {logs.length === 0 ? (
            <p className="py-8 text-center text-xs text-muted-foreground">
              暂无日志。{available ? '启动执行器后将在此显示认领与运行记录。' : '完成配置并启动后，认领与运行记录会在此实时显示。'}
            </p>
          ) : (
            <ul className="space-y-0.5 font-mono text-[11px] leading-relaxed">
              {logs.map((entry) => (
                <li key={entry.id} className="flex gap-2">
                  <span className="shrink-0 select-none text-muted-foreground/50">
                    {formatTime(entry.ts)}
                  </span>
                  <span className={`flex-1 break-all ${LEVEL_CLASS[entry.level]}`}>
                    {entry.taskId ? (
                      <span className="text-muted-foreground">[{entry.taskId.slice(0, 8)}] </span>
                    ) : null}
                    {entry.message}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function formatTime(ts: number): string {
  const d = new Date(ts)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function pad(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}
