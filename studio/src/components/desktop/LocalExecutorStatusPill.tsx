import { Cpu } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import {
  localExecutorStore,
  useLocalExecutorStatus,
} from '@/lib/local-executor-store'

type Mode = 'running' | 'ready' | 'unready' | 'unknown'

function deriveMode(running?: boolean, available?: boolean): Mode {
  if (running) return 'running'
  if (available) return 'ready'
  if (available === false) return 'unready'
  return 'unknown'
}

const DOT_CLASS: Record<Mode, string> = {
  running: 'bg-emerald-500',
  ready: 'bg-muted-foreground/40',
  unready: 'bg-amber-500',
  unknown: 'bg-muted-foreground/20',
}

const LABEL: Record<Mode, string> = {
  running: '本地执行中',
  ready: '本地已就绪',
  unready: '本地未配置',
  unknown: '本地执行器',
}

/**
 * Always-visible desktop-only status indicator for the local executor, mounted
 * in the Sidebar bottom area. Makes the executor's state discoverable without a
 * trip to Settings: click opens the live-log drawer when ready, or the first-run
 * wizard when not yet configured. Renders nothing in a browser (the Sidebar
 * gates the mount on isDesktop()).
 */
export default function LocalExecutorStatusPill({ collapsed }: { collapsed: boolean }) {
  const status = useLocalExecutorStatus()
  const mode = deriveMode(status?.running, status?.available)

  const handleClick = () => {
    if (mode === 'unready' || mode === 'unknown') localExecutorStore.openWizard()
    else localExecutorStore.openPanel()
  }

  const button = (
    <button
      type="button"
      onClick={handleClick}
      aria-label={LABEL[mode]}
      className={cn(
        'group flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
        'text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-foreground',
        collapsed && 'justify-center px-0',
      )}
    >
      <span className="relative flex shrink-0 items-center">
        <Cpu className="h-4 w-4" />
        <span
          className={cn(
            'absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full ring-2 ring-sidebar',
            DOT_CLASS[mode],
            mode === 'running' && 'animate-pulse',
          )}
        />
      </span>
      {!collapsed && <span className="flex-1 text-left">{LABEL[mode]}</span>}
    </button>
  )

  if (collapsed) {
    return (
      <div className="pb-1">
        <Tooltip>
          <TooltipTrigger render={<span className="block" />}>{button}</TooltipTrigger>
          <TooltipContent side="right">{LABEL[mode]}</TooltipContent>
        </Tooltip>
      </div>
    )
  }

  return <div className="pb-1">{button}</div>
}
