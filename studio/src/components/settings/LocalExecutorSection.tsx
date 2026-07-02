import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Loader2, RefreshCw, FolderOpen, Play, Square } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/common/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { ReadinessChecklist } from '@/components/settings/ReadinessChecklist'
import {
  getLocalExecutorStatus,
  setLocalExecutorConfig,
  setExecutorEnabled,
  startLocalExecutor,
  stopLocalExecutor,
  pickDirectory,
  getApiBase,
  setApiBase,
  type LocalExecutorStatus,
} from '@/lib/tauri'
import { applyApiBase } from '@/lib/http-client'

/**
 * Desktop-only local-executor configuration. Renders only inside the Tauri
 * shell (the parent gates on isDesktop()). Lets the user supply the Anban Creator
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
  const [apiBaseInput, setApiBaseInput] = useState('')
  const [savingBase, setSavingBase] = useState(false)

  const { data: status, isLoading, refetch, isFetching } = useQuery<LocalExecutorStatus | null>({
    queryKey: ['local-executor-status'],
    queryFn: () => getLocalExecutorStatus(),
    refetchInterval: 4000,
  })

  // Prefill form fields from the latest status (workspace + auth hints).
  useEffect(() => {
    if (status) setWorkspace(status.workspace || '')
  }, [status?.workspace])

  // Load the current cloud API base once so self-hosters can view/change it.
  useEffect(() => {
    void getApiBase().then((b) => {
      if (b) setApiBaseInput(b)
    })
  }, [])

  const handlePick = async () => {
    const dir = await pickDirectory()
    if (dir) setWorkspace(dir)
  }

  const handleSaveBase = async () => {
    setSavingBase(true)
    try {
      const ok = await setApiBase(apiBaseInput)
      if (ok) {
        applyApiBase(apiBaseInput)
        toast.success('云端地址已更新')
      } else {
        toast.error('保存失败')
      }
    } finally {
      setSavingBase(false)
    }
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
          <ReadinessChecklist status={status} />
        )}
        {status && !ready && status.reason && (
          <p className="text-xs text-amber-600">{status.reason}</p>
        )}

        {/* Config form */}
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="le-api-key" className="text-xs">Anban Creator API Key</Label>
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

        {/* Cloud API base — self-hosters point this at their own server. */}
        <div className="space-y-1.5 border-t border-border pt-3">
          <Label htmlFor="le-api-base" className="text-xs">
            云端 API 地址（自部署可改）
          </Label>
          <div className="flex gap-2">
            <Input
              id="le-api-base"
              value={apiBaseInput}
              onChange={(e) => setApiBaseInput(e.target.value)}
              placeholder="https://api.anbanai.com/api/v1"
            />
            <Button variant="secondary" onClick={handleSaveBase} disabled={savingBase || !apiBaseInput.trim()}>
              {savingBase ? <Loader2 className="h-4 w-4 animate-spin" /> : '应用'}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
