import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Copy, Loader2, MessageCircle, RefreshCw, Unlink } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { api } from '@/lib/api'
import type { IlinkBindCodeResult, IlinkStatus } from '@/lib/api/ilink'

export default function IlinkBindingSection() {
  const queryClient = useQueryClient()
  const [bindCode, setBindCode] = useState<IlinkBindCodeResult | null>(null)
  const [creating, setCreating] = useState(false)
  const [unbinding, setUnbinding] = useState(false)
  const [confirmUnbind, setConfirmUnbind] = useState(false)
  const [savingProject, setSavingProject] = useState(false)

  const { data: status, isLoading, refetch, isFetching } = useQuery<IlinkStatus>({
    queryKey: ['ilink-status'],
    queryFn: () => api.ilink.status(),
  })

  useEffect(() => {
    if (!confirmUnbind) return
    const t = setTimeout(() => setConfirmUnbind(false), 4000)
    return () => clearTimeout(t)
  }, [confirmUnbind])

  useEffect(() => {
    if (status?.status !== 'active') setConfirmUnbind(false)
  }, [status?.status])

  const handleCreateCode = async () => {
    setCreating(true)
    try {
      const res = await api.ilink.createBindCode()
      setBindCode(res)
      queryClient.invalidateQueries({ queryKey: ['ilink-status'] })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '生成绑定码失败')
    } finally {
      setCreating(false)
    }
  }

  const handleCopy = async () => {
    if (!bindCode?.bind_code) return
    await navigator.clipboard.writeText(bindCode.bind_code)
    toast.success('绑定码已复制')
  }

  const handleUnbind = async () => {
    if (!confirmUnbind) {
      setConfirmUnbind(true)
      return
    }
    setUnbinding(true)
    try {
      await api.ilink.unbind()
      toast.success('已解绑微信助手')
      setBindCode(null)
      setConfirmUnbind(false)
      queryClient.invalidateQueries({ queryKey: ['ilink-status'] })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '解绑失败')
    } finally {
      setUnbinding(false)
    }
  }

  const handleSetProject = async (projectId: string) => {
    if (!projectId) return
    setSavingProject(true)
    try {
      await api.ilink.setDefaultProject(projectId)
      toast.success('默认项目已更新')
      queryClient.invalidateQueries({ queryKey: ['ilink-status'] })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '设置默认项目失败')
    } finally {
      setSavingProject(false)
    }
  }

  const available = status?.available ?? false
  const bound = status?.bound || status?.status === 'active'
  const assistant = bindCode?.assistant ?? status?.assistant

  return (
    <Card>
      <CardContent className="space-y-4 pt-6">
        <div className="flex items-center justify-between gap-3">
          <div className="space-y-1">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
              <MessageCircle className="h-4 w-4" />
              微信助手
              {bound ? (
                <Badge variant="outline" className="text-[10px] text-emerald-600">已绑定</Badge>
              ) : status?.status === 'pending' ? (
                <Badge variant="outline" className="text-[10px] text-amber-600">绑定中</Badge>
              ) : (
                <Badge variant="outline" className="text-[10px]">未绑定</Badge>
              )}
            </h2>
            <p className="text-xs text-muted-foreground">
              添加平台微信助手后，可直接发消息创建任务，并及时接收任务成功或失败提醒。
            </p>
          </div>
          <Button aria-label="刷新微信绑定状态" variant="secondary" size="sm" onClick={() => refetch()} disabled={isFetching}>
            <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </Button>
        </div>

        {isLoading ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /> 正在检查状态...
          </div>
        ) : !available ? (
          <p className="text-xs text-muted-foreground">微信助手服务未启用或不可用。</p>
        ) : bound ? (
          <>
            {bindCode ? <BindCodePanel bindCode={bindCode.bind_code} onCopy={handleCopy} /> : null}
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-foreground">默认项目</p>
              <p className="text-xs text-muted-foreground">通过微信创建任务时使用此项目。</p>
              <DefaultProjectSelect
                value={status?.default_project_id ?? ''}
                disabled={savingProject}
                onChange={handleSetProject}
              />
            </div>

            <div className="flex items-center gap-2 border-t border-border pt-3">
              <Button variant="secondary" onClick={handleCreateCode} disabled={creating}>
                {creating ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                重新生成绑定码
              </Button>
              <Button variant={confirmUnbind ? 'destructive' : 'outline'} onClick={handleUnbind} disabled={unbinding}>
                {unbinding ? <Loader2 className="h-4 w-4 animate-spin" /> : <Unlink className="h-4 w-4" />}
                {confirmUnbind ? '确认解绑' : '解绑'}
              </Button>
            </div>
          </>
        ) : (
          <div className="space-y-3">
            {assistant ? (
              <div className="space-y-1 text-xs text-muted-foreground">
                <p className="font-medium text-foreground">{assistant.display_name || 'Anban 微信助手'}</p>
                {assistant.wechat_id ? <p>微信号：{assistant.wechat_id}</p> : null}
                {assistant.qrcode_url ? <img src={assistant.qrcode_url} alt="微信助手二维码" className="h-36 w-36 rounded border border-border" /> : null}
              </div>
            ) : null}
            {bindCode ? <BindCodePanel bindCode={bindCode.bind_code} onCopy={handleCopy} /> : null}
            <Button onClick={handleCreateCode} disabled={creating}>
              {creating ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              生成绑定码
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function BindCodePanel({ bindCode, onCopy }: { bindCode: string; onCopy: () => void }) {
  return (
    <div className="flex items-center justify-between rounded-md border border-border px-3 py-2">
      <div>
        <p className="text-xs text-muted-foreground">把绑定码发送给微信助手</p>
        <p className="font-mono text-xl font-semibold tracking-normal">{bindCode}</p>
      </div>
      <Button variant="secondary" size="sm" onClick={onCopy}>
        <Copy className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}

function DefaultProjectSelect({
  value,
  disabled,
  onChange,
}: {
  value: string
  disabled?: boolean
  onChange: (projectId: string) => void
}) {
  const { data: projects, isLoading } = useQuery({
    queryKey: ['projects', 'active'],
    queryFn: () => api.projects.list({ status: 'active' }),
  })

  return (
    <NativeSelect value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
      <NativeSelectOption value="" disabled>
        {isLoading ? '加载中...' : '选择默认项目'}
      </NativeSelectOption>
      {projects?.map((p) => (
        <NativeSelectOption key={p.id} value={p.id}>
          {p.name} ({p.platform})
        </NativeSelectOption>
      ))}
    </NativeSelect>
  )
}
