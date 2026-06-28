import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Loader2, Play, Square, Trash2, Settings, Terminal } from 'lucide-react'
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
} from '@/lib/tauri'
import {
  localExecutorStore,
  useLocalExecutorLogs,
  useLocalExecutorPanelOpen,
  useLocalExecutorStatus,
  type LogLevel,
} from '@/lib/local-executor-store'

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

  const running = status?.running ?? false
  const available = status?.available ?? false

  return (
    <Sheet open={open} onOpenChange={(o) => { if (!o) localExecutorStore.closePanel() }}>
      <SheetContent side="right" className="w-full p-0 sm:max-w-md">
        <SheetHeader className="border-b">
          <div className="flex items-center justify-between gap-2 pr-8">
            <SheetTitle className="flex items-center gap-2">
              <Terminal className="h-4 w-4 text-primary" /> 本地执行
              {running ? (
                <Badge variant="outline" className="text-[10px] text-emerald-600">运行中</Badge>
              ) : available ? (
                <Badge variant="outline" className="text-[10px]">已就绪</Badge>
              ) : (
                <Badge variant="outline" className="text-[10px] text-amber-600">未就绪</Badge>
              )}
            </SheetTitle>
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
          </div>
          <SheetDescription className="sr-only">
            本地执行器实时日志与控制
          </SheetDescription>
        </SheetHeader>

        {/* Status summary + controls */}
        <div className="space-y-3 border-b px-4 py-3">
          {status && !available && status.reason && (
            <p className="text-xs text-amber-600">{status.reason}</p>
          )}
          <ReadinessChecklist status={status} />
          <div className="flex items-center gap-2">
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
              onClick={() => localExecutorStore.clearLogs()}
              disabled={logs.length === 0}
              title="清空日志"
            >
              <Trash2 className="h-4 w-4" /> 清空
            </Button>
          </div>
        </div>

        {/* Live log tail */}
        <div ref={scrollRef} className="flex-1 overflow-y-auto bg-muted/20 p-3">
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
