import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Loader2, RefreshCw, MessageCircle, Unlink } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/common/button'
import { Badge } from '@/components/ui/badge'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { api } from '@/lib/api'
import type { WeChatBindStatus } from '@/lib/api/wechat'

/**
 * WeChat bot binding (wcfLink sidecar). The user binds their own WeChat by
 * scanning a QR; the server then pushes task success/failure notifications and
 * accepts commands via the 文件传输助手 self-chat. Always mounted (web + desktop)
 * so users can bind from either surface; self-reports availability via the
 * status endpoint when the sidecar is down or disabled.
 *
 * Mirrors the LocalExecutorSection layout (Card + status Badge + form actions).
 */
export default function WeChatBindingSection() {
  const queryClient = useQueryClient()
  const [sessionId, setSessionId] = useState('')
  const [qrUrl, setQrUrl] = useState('')
  const [starting, setStarting] = useState(false)
  const [unbinding, setUnbinding] = useState(false)
  const [confirmUnbind, setConfirmUnbind] = useState(false)
  const [savingProject, setSavingProject] = useState(false)

  const { data: status, isLoading, refetch, isFetching } = useQuery<WeChatBindStatus>({
    queryKey: ['wechat-status'],
    queryFn: () => api.wechat.status(),
  })

  // Poll the active login session until it confirms (or expires).
  const { data: poll } = useQuery<WeChatBindStatus>({
    queryKey: ['wechat-bind-poll', sessionId],
    queryFn: () => api.wechat.bindStatus(sessionId),
    enabled: !!sessionId,
    refetchInterval: (query) =>
      query.state.data?.status === 'active' || query.state.data?.status === 'expired' || query.state.data?.status === 'error'
        ? false
        : 2000,
  })

  useEffect(() => {
    if (!poll) return
    if (poll.status === 'active') {
      toast.success('微信绑定成功')
      setSessionId('')
      setQrUrl('')
      queryClient.invalidateQueries({ queryKey: ['wechat-status'] })
    } else if (poll.status === 'expired' || poll.status === 'error') {
      toast.error('二维码已过期或登录失败，请重新绑定')
      setSessionId('')
      setQrUrl('')
    }
  }, [poll?.status, queryClient])

  // Auto-reset the two-step unbind confirm if the user walks away.
  useEffect(() => {
    if (!confirmUnbind) return
    const t = setTimeout(() => setConfirmUnbind(false), 4000)
    return () => clearTimeout(t)
  }, [confirmUnbind])

  // Reset confirm state if binding status changes out from under us.
  useEffect(() => {
    if (status?.status !== 'active') setConfirmUnbind(false)
  }, [status?.status])

  const handleStart = async () => {
    setStarting(true)
    try {
      const res = await api.wechat.bindStart()
      setSessionId(res.session_id)
      setQrUrl(res.qrcode_url)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '启动绑定失败')
    } finally {
      setStarting(false)
    }
  }

  const handleUnbind = async () => {
    if (!confirmUnbind) {
      setConfirmUnbind(true)
      return
    }
    setUnbinding(true)
    try {
      await api.wechat.unbind()
      toast.success('已解绑微信')
      setConfirmUnbind(false)
      queryClient.invalidateQueries({ queryKey: ['wechat-status'] })
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
      await api.wechat.setDefaultProject(projectId)
      toast.success('默认项目已更新')
      queryClient.invalidateQueries({ queryKey: ['wechat-status'] })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '设置默认项目失败')
    } finally {
      setSavingProject(false)
    }
  }

  const available = status?.available ?? false
  const bound = status?.status === 'active'

  return (
    <Card>
      <CardContent className="space-y-4 pt-6">
        <div className="flex items-center justify-between">
          <div className="space-y-1">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
              <MessageCircle className="h-4 w-4" />
              微信通知与指令
              {bound ? (
                <Badge variant="outline" className="text-[10px] text-emerald-600">已绑定</Badge>
              ) : status?.status === 'pending' ? (
                <Badge variant="outline" className="text-[10px] text-amber-600">绑定中</Badge>
              ) : (
                <Badge variant="outline" className="text-[10px]">未绑定</Badge>
              )}
            </h2>
            <p className="text-xs text-muted-foreground">
              绑定自己的微信号后，可在微信（文件传输助手）收到任务成功/失败提醒，并发指令创建/查询/取消任务。
            </p>
          </div>
          <Button variant="secondary" size="sm" onClick={() => refetch()} disabled={isFetching}>
            <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </Button>
        </div>

        {isLoading ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /> 正在检查状态…
          </div>
        ) : !available ? (
          <p className="text-xs text-muted-foreground">
            微信通知服务未启用或不可用。请联系管理员开启 wcfLink 服务。
          </p>
        ) : qrUrl ? (
          <div className="space-y-3">
            <div className="flex flex-col items-center gap-2 rounded-md border border-border p-4">
              <img src={qrUrl} alt="微信登录二维码" className="h-48 w-48" />
              <p className="text-center text-xs text-muted-foreground">
                请用微信扫码登录。{poll?.status === 'scanned' ? '已扫描，请在手机确认…' : '等待扫描…'}
              </p>
            </div>
            <Button variant="secondary" size="sm" onClick={() => { setSessionId(''); setQrUrl('') }}>
              取消
            </Button>
          </div>
        ) : bound ? (
          <>
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-foreground">默认项目</p>
              <p className="text-xs text-muted-foreground">发指令创建任务时使用此项目（任务类型由项目平台决定）。</p>
              <DefaultProjectSelect
                value={status?.default_project_id ?? ''}
                disabled={savingProject}
                onChange={handleSetProject}
              />
            </div>

            <div className="flex items-center gap-2 border-t border-border pt-3">
              <Button variant="secondary" onClick={handleStart} disabled={starting}>
                {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                重新绑定
              </Button>
              <Button
                variant={confirmUnbind ? 'destructive' : 'outline'}
                onClick={handleUnbind}
                disabled={unbinding}
              >
                {unbinding ? <Loader2 className="h-4 w-4 animate-spin" /> : <Unlink className="h-4 w-4" />}
                {confirmUnbind ? '确认解绑' : '解绑'}
              </Button>
            </div>
          </>
        ) : (
          <div className="flex items-center gap-2">
            <Button onClick={handleStart} disabled={starting}>
              {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              绑定微信
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

/** Default-project picker. Loaded lazily so this section doesn't fetch projects
 * until the user is bound (the select only renders then). */
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
      <NativeSelectOption value="">{isLoading ? '加载中…' : '选择默认项目'}</NativeSelectOption>
      {projects?.map((p) => (
        <NativeSelectOption key={p.id} value={p.id}>
          {p.name}（{p.platform}）
        </NativeSelectOption>
      ))}
    </NativeSelect>
  )
}
