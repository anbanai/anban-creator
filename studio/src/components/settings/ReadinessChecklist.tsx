import { CheckCircle2, XCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { LocalExecutorStatus } from '@/lib/tauri'

/**
 * Local-executor dependency checklist. Shared by the settings section, the
 * first-run wizard and the live-log drawer so the readiness grid renders
 * identically everywhere. Mirrors the markup previously inlined in
 * LocalExecutorSection (green ✓ for met deps, amber ✗ for missing, dimmed for
 * optional ones).
 */
export function ReadinessChecklist({
  status,
  className,
}: {
  status?: LocalExecutorStatus | null
  className?: string
}) {
  return (
    <ul className={cn('grid grid-cols-2 gap-x-4 gap-y-1 text-xs', className)}>
      <Readiness ok={status?.api_key_set} label="Anban Creator API Key" />
      <Readiness ok={status?.claude_authenticated} label="Claude 鉴权" />
      <Readiness ok={status?.workspace_set} label="本地工作区" />
      <Readiness ok={status?.agent_present} label="abwriter-agent" />
      <Readiness ok={status?.node_present} label="Node 运行时" />
      <Readiness ok={status?.claude_present} label="claude-code" />
      <Readiness ok={status?.plugin_present} label="claudecode 插件" />
      <Readiness ok={status?.ffmpeg_present} label="ffmpeg（可选）" optional />
    </ul>
  )
}

function Readiness({ ok, label, optional }: { ok?: boolean; label: string; optional?: boolean }) {
  if (ok) {
    return (
      <li className="flex items-center gap-1.5 text-muted-foreground">
        <CheckCircle2 className="h-3.5 w-3.5 text-emerald-600" /> {label}
      </li>
    )
  }
  return (
    <li
      className={cn(
        'flex items-center gap-1.5',
        optional ? 'text-muted-foreground/60' : 'text-amber-600',
      )}
    >
      <XCircle className="h-3.5 w-3.5" /> {label}
    </li>
  )
}
