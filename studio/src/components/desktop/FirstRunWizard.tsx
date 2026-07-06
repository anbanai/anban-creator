import { useEffect, useState } from 'react'
import { Loader2, FolderOpen, Rocket, ChevronLeft, Cpu } from 'lucide-react'
import { toast } from 'sonner'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/common/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ReadinessChecklist } from '@/components/settings/ReadinessChecklist'
import {
  setLocalExecutorConfig,
  startLocalExecutor,
  setExecutorEnabled,
  getLocalExecutorStatus,
  pickDirectory,
} from '@/lib/tauri'
import {
  localExecutorStore,
  useLocalExecutorStatus,
  useLocalExecutorWizardOpen,
} from '@/lib/local-executor-store'

/**
 * Desktop-only first-run onboarding. Guides the user through the three things
 * the local executor needs (API key → Claude auth → workspace) then launches
 * the claim loop. Auto-opened by LocalExecutorLayer when the machine isn't
 * provisioned yet; re-openable from the status pill / settings.
 *
 * In a browser this is never mounted (the layer is a no-op there).
 */
const STEPS = ['欢迎', '配置', '完成'] as const

export default function FirstRunWizard() {
  const open = useLocalExecutorWizardOpen()
  const status = useLocalExecutorStatus()
  const [step, setStep] = useState(0)
  const [apiKey, setApiKey] = useState('')
  const [workspace, setWorkspace] = useState('')
  const [claudeKey, setClaudeKey] = useState('')
  const [saving, setSaving] = useState(false)
  const [starting, setStarting] = useState(false)

  // Reset to the welcome step each time the wizard is (re)opened, and prefill
  // the workspace from the last-known status.
  useEffect(() => {
    if (open) {
      setStep(0)
      setApiKey('')
      setClaudeKey('')
    }
  }, [open])

  useEffect(() => {
    if (status) setWorkspace(status.workspace || '')
  }, [status?.workspace])

  const handlePick = async () => {
    const dir = await pickDirectory()
    if (dir) setWorkspace(dir)
  }

  const refreshStatus = async () => {
    const s = await getLocalExecutorStatus()
    localExecutorStore.setStatus(s)
    return s
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const ok = await setLocalExecutorConfig({ apiKey, workspace, claudeApiKey: claudeKey })
      if (ok) {
        toast.success('配置已保存')
        setApiKey('')
        setClaudeKey('')
        await refreshStatus()
        setStep(2)
      } else {
        toast.error('保存失败，请检查输入')
      }
    } finally {
      setSaving(false)
    }
  }

  const handleLaunch = async () => {
    setStarting(true)
    try {
      const ok = await startLocalExecutor()
      if (ok) {
        await setExecutorEnabled(true)
        await refreshStatus()
        toast.success('本地执行器已启动')
        localExecutorStore.closeWizard()
      } else {
        toast.error('启动失败：依赖未就绪')
      }
    } finally {
      setStarting(false)
    }
  }

  const available = status?.available ?? false

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        // Closing via X / escape / backdrop suppresses auto-open for this
        // session (avoids the dialog snapping back open on the next status
        // poll). Programmatic close after a successful launch uses
        // closeWizard() which does NOT suppress.
        if (!o) localExecutorStore.dismissWizard()
      }}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Cpu className="h-4 w-4 text-cyan-500" /> 本机运行中心
          </DialogTitle>
          <DialogDescription>
            在本机接管任务执行：Claude Code、ffmpeg、本地命令与工作区状态会在这里统一预检。
          </DialogDescription>
        </DialogHeader>

        {/* Stepper */}
        <div className="flex items-center gap-1.5">
          {STEPS.map((label, i) => (
            <div key={label} className="flex flex-1 flex-col items-center gap-1">
              <div
                className={`h-1 w-full rounded-full transition-colors ${
                  i <= step ? 'bg-primary' : 'bg-muted'
                }`}
              />
              <span
                className={`text-[10px] ${
                  i === step ? 'font-medium text-foreground' : 'text-muted-foreground'
                }`}
              >
                {label}
              </span>
            </div>
          ))}
        </div>

        {/* Step 0 — welcome */}
        {step === 0 && (
          <div className="space-y-3">
            <p className="text-xs text-muted-foreground">
              本机运行让任务拥有真实文件系统与命令行，适合直播切片、设计上色、批量素材处理等需要本地环境的工作流。
            </p>
            <p className="text-xs text-muted-foreground">配置需要三样东西：</p>
            <ul className="space-y-1 text-xs text-foreground/80">
              <li>• <b>Anban Creator API Key</b>（在「设置 → 平台密钥」创建）</li>
              <li>• <b>Claude 鉴权</b>（当前版本需要 ANTHROPIC_API_KEY）</li>
              <li>• <b>本地工作区根目录</b>（任务的临时工作目录）</li>
            </ul>
            {available && (
              <p className="rounded-md border border-emerald-500/30 bg-emerald-500/5 px-2.5 py-1.5 text-xs text-emerald-600">
                依赖已就绪，可直接启动。
              </p>
            )}
          </div>
        )}

        {/* Step 1 — config */}
        {step === 1 && (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="wiz-api-key" className="text-xs">Anban Creator API Key</Label>
              <Input
                id="wiz-api-key"
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={status?.api_key_set ? '已配置，留空将保留' : '在「平台密钥」创建后粘贴'}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="wiz-workspace" className="text-xs">本地工作区根目录</Label>
              <div className="flex gap-2">
                <Input
                  id="wiz-workspace"
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
              <Label htmlFor="wiz-claude-key" className="text-xs">
                Anthropic API Key（当前版本必填）
              </Label>
              <Input
                id="wiz-claude-key"
                type="password"
                value={claudeKey}
                onChange={(e) => setClaudeKey(e.target.value)}
                placeholder={status?.claude_authenticated ? '已配置，留空将保留' : 'ANTHROPIC_API_KEY'}
              />
            </div>
          </div>
        )}

        {/* Step 2 — review + launch */}
        {step === 2 && (
          <div className="space-y-3">
            <ReadinessChecklist status={status} />
            {status && !available && status.reason && (
              <p className="text-xs text-amber-600">{status.reason}</p>
            )}
            {available && (
              <p className="text-xs text-muted-foreground">
                本机运行链路已就绪。启动后，新任务可以立即由这台电脑认领。
              </p>
            )}
          </div>
        )}

        <DialogFooter>
          {step === 0 && (
            <>
              <Button variant="ghost" onClick={() => localExecutorStore.dismissWizard()}>
                稍后再说
              </Button>
              <Button onClick={() => setStep(available ? 2 : 1)}>开始配置</Button>
            </>
          )}
          {step === 1 && (
            <>
              <Button variant="ghost" onClick={() => setStep(0)}>
                <ChevronLeft className="h-4 w-4" /> 上一步
              </Button>
              <Button onClick={handleSave} disabled={saving || (!apiKey && !claudeKey && !workspace)}>
                {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : '保存并继续'}
              </Button>
            </>
          )}
          {step === 2 && (
            <>
              <Button variant="ghost" onClick={() => setStep(1)}>
                <ChevronLeft className="h-4 w-4" /> 返回配置
              </Button>
              <Button onClick={handleLaunch} disabled={starting || !available}>
                {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Rocket className="h-4 w-4" />}
                启动本地执行器
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
