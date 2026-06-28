import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Loader2, RefreshCw, FolderOpen, Play, Square, CheckCircle2, XCircle } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/common/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  getLocalExecutorStatus,
  setLocalExecutorConfig,
  setExecutorEnabled,
  startLocalExecutor,
  stopLocalExecutor,
  pickDirectory,
  type LocalExecutorStatus,
} from '@/lib/tauri'

/**
 * Desktop-only local-executor configuration. Renders only inside the Tauri
 * shell (the parent gates on isDesktop()). Lets the user supply the AnbanWriter
 * API key + workspace + Claude credentials, then start/stop the background
 * claim loop. In the browser this section is never mounted.
 */
export default function LocalExecutorSection() {
  const queryClient = useQueryClient()
  const [apiKey, setApiKey] = useState('')
  const [workspace, setWorkspace] = useState('')
  const [claudeKey, setClaudeKey] = useState('')
  const [saving, setSaving] = useState(false)
  const [toggling, setToggling] = useState(false)

  const { data: status, isLoading, refetch, isFetching } = useQuery<LocalExecutorStatus | null>({
    queryKey: ['local-executor-status'],
    queryFn: () => getLocalExecutorStatus(),
    refetchInterval: 4000,
  })

  // Prefill form fields from the latest status (workspace + auth hints).
  useEffect(() => {
    if (status) setWorkspace(status.workspace || '')
  }, [status?.workspace])

  const handlePick = async () => {
    const dir = await pickDirectory()
    if (dir) setWorkspace(dir)
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const ok = await setLocalExecutorConfig({ apiKey, workspace, claudeApiKey: claudeKey })
      if (ok) {
        toast.success('本地执行器配置已保存')
        setApiKey('')
        setClaudeKey('')
        await refetch()
      } else {
        toast.error('保存失败')
      }
    } finally {
      setSaving(false)
    }
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
      await refetch()
    } finally {
      setToggling(false)
      queryClient.invalidateQueries({ queryKey: ['local-executor-status'] })
    }
  }

  const ready = status?.available ?? false

  return (
    <Card>
      <CardContent className="space-y-4 pt-6">
        <div className="flex items-center justify-between">
          <div className="space-y-1">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
              本地执行器
              {status?.running ? (
                <Badge variant="outline" className="text-[10px] text-emerald-600">运行中</Badge>
              ) : (
                <Badge variant="outline" className="text-[10px]">已停止</Badge>
              )}
            </h2>
            <p className="text-xs text-muted-foreground">
              在本机运行任务（Claude Code + ffmpeg / 本地命令）。未就绪时新任务自动走云端。
            </p>
          </div>
          <Button variant="secondary" size="sm" onClick={() => refetch()} disabled={isFetching}>
            <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </Button>
        </div>

        {/* Readiness checklist */}
        {isLoading ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /> 正在检查依赖…
          </div>
        ) : (
          <ul className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
            <Readiness ok={status?.api_key_set} label="AnbanWriter API Key" />
            <Readiness ok={status?.claude_authenticated} label="Claude 鉴权" />
            <Readiness ok={status?.workspace_set} label="本地工作区" />
            <Readiness ok={status?.agent_present} label="abwriter-agent" />
            <Readiness ok={status?.node_present} label="Node 运行时" />
            <Readiness ok={status?.claude_present} label="claude-code" />
            <Readiness ok={status?.plugin_present} label="claudecode 插件" />
            <Readiness ok={status?.ffmpeg_present} label="ffmpeg（可选）" optional />
          </ul>
        )}
        {status && !ready && status.reason && (
          <p className="text-xs text-amber-600">{status.reason}</p>
        )}

        {/* Config form */}
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="le-api-key" className="text-xs">AnbanWriter API Key</Label>
            <Input
              id="le-api-key"
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={status?.api_key_set ? '已配置（输入可覆盖）' : '在「平台密钥」创建后粘贴'}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="le-workspace" className="text-xs">本地工作区根目录</Label>
            <div className="flex gap-2">
              <Input
                id="le-workspace"
                value={workspace}
                onChange={(e) => setWorkspace(e.target.value)}
                placeholder="如 /Users/me/anban-tasks"
              />
              <Button variant="secondary" size="icon" onClick={handlePick} title="选择目录">
                <FolderOpen className="h-4 w-4" />
              </Button>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="le-claude-key" className="text-xs">Anthropic API Key（可选，否则用 Claude OAuth）</Label>
            <Input
              id="le-claude-key"
              type="password"
              value={claudeKey}
              onChange={(e) => setClaudeKey(e.target.value)}
              placeholder={status?.claude_authenticated ? '已配置（输入可覆盖）' : 'ANTHROPIC_API_KEY'}
            />
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button onClick={handleSave} disabled={saving || (!apiKey && !claudeKey && !workspace)}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : '保存配置'}
          </Button>
          <Button
            variant={status?.running ? 'destructive' : 'default'}
            onClick={handleToggle}
            disabled={toggling || (!status?.running && !ready)}
          >
            {status?.running ? <Square className="h-4 w-4" /> : <Play className="h-4 w-4" />}
            {status?.running ? '停止' : '启动'}
          </Button>
        </div>
      </CardContent>
    </Card>
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
    <li className={`flex items-center gap-1.5 ${optional ? 'text-muted-foreground/60' : 'text-amber-600'}`}>
      <XCircle className="h-3.5 w-3.5" /> {label}
    </li>
  )
}
