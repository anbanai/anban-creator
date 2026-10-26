import { useCallback, useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { MessagesSquare, QrCode, Unlink } from 'lucide-react'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import type { WechatPlatformConfig } from '@/types'
import { Button } from '@/components/common/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter, AlertDialogCancel, AlertDialogAction } from '@/components/ui/alert-dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { ProjectIdentity, type ProjectIdentityProject } from '@/components/agent-prompt/ProjectIdentity'

type Credentials = { appID: string; secret: string }
interface Props {
  project: ProjectIdentityProject
  onClose: () => void
}

interface EmbeddedProps {
  project: ProjectIdentityProject
  onStateChange: (dirty: boolean, busy: boolean) => void
  onClose?: () => void
  embedded?: boolean
}

function ChannelConfigEditor({ project, config, onClose, onStateChange, embedded = false }: Props & { config: WechatPlatformConfig; onStateChange: (dirty: boolean, busy: boolean) => void; embedded?: boolean }) {
  const queryClient = useQueryClient()
  const active = useRef(true)
  const authorizationWindow = useRef<Window | null>(null)
  useEffect(() => {
    active.current = true
    return () => {
      active.current = false
      authorizationWindow.current?.close()
      authorizationWindow.current = null
    }
  }, [])
  const [draft, setDraft] = useState<Credentials>(() => ({ appID: config.app_id, secret: '' }))
  const [saved, setSaved] = useState(draft)
  const [secretConfigured, setSecretConfigured] = useState(config.secret_configured)
  const [credentialsConflict, setCredentialsConflict] = useState(config.credentials_conflict)
  const [automaticDeliveryEnabled, setAutomaticDeliveryEnabled] = useState(config.automatic_delivery_enabled === true)
  const [savedAutomaticDeliveryEnabled, setSavedAutomaticDeliveryEnabled] = useState(config.automatic_delivery_enabled === true)
  const initialDeliveryMode = config.delivery_mode === 'direct_publish' ? 'direct_publish' : 'draft'
  const [deliveryMode, setDeliveryMode] = useState<'draft' | 'direct_publish'>(initialDeliveryMode)
  const [savedDeliveryMode, setSavedDeliveryMode] = useState<'draft' | 'direct_publish'>(initialDeliveryMode)
  const [disconnectOpen, setDisconnectOpen] = useState(false)
  const connection = config.connection
  const authorizationEnabled = config.authorization_enabled === true
  const hasAuthorizationRecord = connection?.authorization_record_exists === true
  const connected = connection?.connected === true
  const canCreateDraft = connection?.can_create_draft === true
  const showLegacyCredentials = !authorizationEnabled || !hasAuthorizationRecord
  const accountChanged = draft.appID.trim() !== saved.appID.trim()
  const requiresSecret = !secretConfigured || credentialsConflict || accountChanged
  const legacyCanSave = Boolean(draft.appID.trim()) && (!requiresSecret || Boolean(draft.secret.trim()))
    || (!draft.appID.trim() && automaticDeliveryEnabled !== savedAutomaticDeliveryEnabled)
  const canSave = (hasAuthorizationRecord ? automaticDeliveryEnabled !== savedAutomaticDeliveryEnabled : legacyCanSave)
    || deliveryMode !== savedDeliveryMode
  const credentialsReady = connected
    ? canCreateDraft
    : Boolean(draft.appID.trim()) && secretConfigured && !credentialsConflict && !accountChanged
  const save = useMutation({
    mutationFn: (credentials: Credentials) => api.projects.upsertWechatPlatformConfig(project.id, {
      ...(credentials.appID.trim() ? { app_id: credentials.appID.trim() } : {}),
      ...(credentials.secret.trim() ? { secret: credentials.secret.trim() } : {}),
      automatic_delivery_enabled: automaticDeliveryEnabled,
      delivery_mode: deliveryMode,
    }),
    onSuccess: (result, credentials) => {
      void queryClient.invalidateQueries({ queryKey: ['project-wechat-platform-config', project.id], refetchType: 'none' })
      if (!active.current) return
      toast.success(authorizationEnabled ? '公众号草稿箱设置已保存' : '公众号设置已保存')
      const clean = { appID: credentials.appID.trim(), secret: '' }
      setDraft(clean)
      setSaved(clean)
      setSecretConfigured(result.secret_configured)
      setCredentialsConflict(result.credentials_conflict)
      setAutomaticDeliveryEnabled(result.automatic_delivery_enabled === true)
      setSavedAutomaticDeliveryEnabled(result.automatic_delivery_enabled === true)
      const nextMode = result.delivery_mode === 'direct_publish' ? 'direct_publish' : 'draft'
      setDeliveryMode(nextMode)
      setSavedDeliveryMode(nextMode)
    },
    onError: (error) => {
      if (active.current) toast.error(getApiErrorMessage(error, '公众号设置保存失败，请重试'))
    },
  })
  const startAuthorization = useMutation({
    mutationFn: () => api.projects.startWechatAuthorization(project.id),
    onSuccess: (result) => {
      const popup = authorizationWindow.current
      authorizationWindow.current = null
      if (!popup || popup.closed) {
        if (active.current) toast.error('授权窗口已关闭，请重新发起扫码绑定')
        return
      }
      popup.location.replace(result.authorization_url)
    },
    onError: (error) => {
      authorizationWindow.current?.close()
      authorizationWindow.current = null
      if (active.current) toast.error(getApiErrorMessage(error, '无法发起公众号授权，请重试'))
    },
  })
  const disconnect = useMutation({
    mutationFn: () => api.projects.disconnectWechatAuthorization(project.id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['project-wechat-platform-config', project.id] })
      setDisconnectOpen(false)
      if (active.current) toast.success('已解除本系统中的公众号绑定')
    },
    onError: (error) => {
      if (active.current) toast.error(getApiErrorMessage(error, '解绑失败，请重试'))
    },
  })
  const dirty = draft.appID !== saved.appID || draft.secret !== saved.secret || automaticDeliveryEnabled !== savedAutomaticDeliveryEnabled || deliveryMode !== savedDeliveryMode
  useEffect(() => {
    onStateChange(dirty, save.isPending || startAuthorization.isPending || disconnect.isPending)
  }, [dirty, save.isPending, startAuthorization.isPending, disconnect.isPending, onStateChange])
  function update(value: Partial<Credentials>) {
    setDraft((current) => ({ ...current, ...value }))
  }

  function requestAuthorization() {
    const popup = window.open('about:blank', '_blank')
    if (!popup) {
      toast.error('浏览器拦截了授权窗口，请允许弹窗后重试')
      return
    }
    popup.opener = null
    authorizationWindow.current = popup
    startAuthorization.mutate()
  }

  const statusText = connected
    ? canCreateDraft ? '已连接' : '缺少草稿权限'
    : connection?.status === 'revoked' ? '授权已撤销'
      : hasAuthorizationRecord ? '未连接' : credentialsReady ? '旧账号可用' : '待绑定'
  const connectionMessage = connected
    ? canCreateDraft
      ? connection.can_formally_publish
        ? '文章与说说会共用此公众号。作品先存入草稿箱；正式发布需在任务详情确认。'
        : '草稿权限可用；账号未授予正式发布权限，仍可保存草稿。'
      : `该账号未授予草稿箱所需权限${connection?.missing_capabilities?.length ? `：${connection.missing_capabilities.join('、')}` : ''}。请重新扫码并授予相应权限。`
    : connection?.status === 'revoked'
      ? '微信已撤销授权，自动保存已暂停。请重新扫码绑定；系统不会回退到旧凭据。'
      : hasAuthorizationRecord
        ? '当前未连接公众号。重新扫码绑定后即可恢复草稿箱交付。'
        : '绑定后，生成结果可自动保存到草稿箱；不会发布或群发。'

  return (
    <>
    <form onSubmit={(event) => {
      event.preventDefault()
      if (!save.isPending && canSave) save.mutate(draft)
    }} className="space-y-4">
      <div className="flex min-w-0 items-center gap-3 rounded-md border p-3">
        <span role="img" aria-label="微信平台" className="flex size-10 shrink-0 items-center justify-center rounded-full bg-[#07C160]/10 text-[#07C160]">
          <MessagesSquare aria-hidden="true" className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">微信平台</p>
          <p className="text-xs text-muted-foreground">公众号长文与贴图内容共用此账号。</p>
        </div>
        <span className="shrink-0 text-xs text-muted-foreground" aria-live="polite">
          {authorizationEnabled ? statusText : credentialsConflict ? '账号待统一' : credentialsReady ? '已就绪' : '待完善'}
        </span>
      </div>
      {authorizationEnabled && (
        <section className="space-y-3 border-t pt-4" aria-label="公众号扫码授权">
          {connected && (
            <div className="flex min-w-0 items-center gap-3">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{connection.account_name || '已授权公众号'}</p>
                <p className="mt-1 text-xs text-muted-foreground">{canCreateDraft ? '草稿箱权限可用' : connectionMessage}</p>
              </div>
              <Button type="button" variant="ghost" size="icon" aria-label="解绑公众号" title="解绑公众号" disabled={disconnect.isPending} onClick={() => setDisconnectOpen(true)}>
                <Unlink aria-hidden="true" />
              </Button>
            </div>
          )}
          {!connected && <p role={connection?.status === 'revoked' ? 'alert' : 'status'} className="text-sm text-muted-foreground">{connectionMessage}</p>}
          {(!connected || (connected && (!canCreateDraft || !connection.can_formally_publish))) && (
            <Button type="button" variant="secondary" loading={startAuthorization.isPending} onClick={requestAuthorization}>
              <QrCode aria-hidden="true" />{connected ? '更新公众号权限' : hasAuthorizationRecord ? '重新扫码绑定公众号' : '扫码绑定公众号'}
            </Button>
          )}
          {connected && !canCreateDraft && <p role="alert" className="text-sm text-amber-700 dark:text-amber-300">{connectionMessage}</p>}
        </section>
      )}
      <fieldset className="space-y-2 border-t pt-4">
        <legend className="text-sm font-medium">默认交付方式</legend>
        <div className="grid grid-cols-2 gap-1 rounded-md border bg-muted/40 p-1" role="group" aria-label="默认交付方式">
          <Button type="button" variant={deliveryMode === 'draft' ? 'secondary' : 'ghost'} aria-pressed={deliveryMode === 'draft'} disabled={save.isPending} onClick={() => setDeliveryMode('draft')}>草稿箱</Button>
          <Button type="button" variant={deliveryMode === 'direct_publish' ? 'secondary' : 'ghost'} aria-pressed={deliveryMode === 'direct_publish'} disabled={save.isPending || (connected && (!canCreateDraft || !connection?.can_formally_publish))} onClick={() => setDeliveryMode('direct_publish')}>正式发布</Button>
        </div>
        <p className="text-xs text-muted-foreground">
          {connected && (!canCreateDraft || !connection?.can_formally_publish)
            ? canCreateDraft
              ? '当前账号未授予正式发布权限；重新扫码并授予后可选。'
              : '当前授权缺少草稿箱权限；重新扫码并授予相应权限后可选。'
            : '正式发布只会在任务详情中确认后提交；项目默认值不会自动发布。'}
        </p>
      </fieldset>
      <label className="flex items-center justify-between gap-4 border-t pt-4 text-sm">
        <span>
          <span className="block font-medium">{authorizationEnabled ? '自动存入公众号草稿箱' : '允许自动交付'}</span>
          <span className="mt-1 block text-xs text-muted-foreground">
            {authorizationEnabled ? '自动交付会先保存到公众号草稿箱；正式发布需在任务详情确认。' : '关闭后，生成的文章需要复制到公众号后台完成草稿。'}
          </span>
        </span>
        <Switch
          checked={automaticDeliveryEnabled}
          onCheckedChange={(checked) => {
            if (checked && hasAuthorizationRecord && (!connected || !canCreateDraft)) return
            setAutomaticDeliveryEnabled(checked)
          }}
          aria-label={authorizationEnabled ? '自动存入公众号草稿箱' : '允许自动交付'}
          disabled={save.isPending}
        />
      </label>
      {showLegacyCredentials && (
        <details open={!authorizationEnabled} className={authorizationEnabled ? 'border-t pt-4' : 'space-y-4 border-t pt-4'}>
          {authorizationEnabled && <summary className="cursor-pointer text-sm font-medium">使用存量 AppID 配置</summary>}
          <div className={authorizationEnabled ? 'mt-4 space-y-4' : 'space-y-4'}>
            {credentialsConflict && <p role="alert" className="text-sm text-amber-700 dark:text-amber-300">文章与贴图的旧账号配置不一致，请重新填写要统一使用的微信账号和密钥。</p>}
            <div className="space-y-2">
              <Label htmlFor="channel-app-id">微信 AppID</Label>
              <Input id="channel-app-id" value={draft.appID} required={!authorizationEnabled && !draft.appID} placeholder="wx..." disabled={save.isPending} onChange={(event) => update({ appID: event.target.value })} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="channel-secret">微信 AppSecret</Label>
              <Input id="channel-secret" type="password" autoComplete="new-password" value={draft.secret} placeholder={requiresSecret ? '请输入 AppSecret' : '留空则保持现有密钥'} required={requiresSecret} aria-describedby="channel-secret-help" disabled={save.isPending} onChange={(event) => update({ secret: event.target.value })} />
              <p id="channel-secret-help" className="text-xs leading-relaxed text-muted-foreground">
                {credentialsConflict ? '账号冲突时必须重新填写密钥，保存后文章和贴图将共用此账号。' : accountChanged ? '更换 AppID 时请填写对应的新密钥。' : requiresSecret ? '尚未配置密钥，请在公众号后台「设置与开发 → 基本配置」获取并填写。' : '密钥已安全保存；留空可继续使用现有密钥。'}
                {' '}保存后密钥不会回显。
              </p>
            </div>
          </div>
        </details>
      )}
      <div className="flex justify-end gap-2">
        {!embedded && <Button type="button" variant="secondary" disabled={save.isPending} onClick={onClose}>关闭</Button>}
        <Button type="submit" loading={save.isPending} disabled={!canSave}>{embedded ? '保存公众号设置' : '保存'}</Button>
      </div>
    </form>
    <AlertDialog open={disconnectOpen} onOpenChange={setDisconnectOpen}>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>解绑公众号？</AlertDialogTitle><AlertDialogDescription>这会清除本系统保存的绑定关系，不会撤销微信后台的授权。重新使用前需要再次扫码。</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={disconnect.isPending}>取消</AlertDialogCancel>
          <AlertDialogAction disabled={disconnect.isPending} onClick={(event) => { event.preventDefault(); disconnect.mutate() }}>
            {disconnect.isPending ? '正在解绑…' : '确认解绑'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
    </>
  )
}

export function WechatPlatformSettingsPanel({ project, onStateChange, onClose = () => {}, embedded = true }: EmbeddedProps) {
  const configs = useQuery({
    queryKey: ['project-wechat-platform-config', project.id],
    queryFn: () => api.projects.getWechatPlatformConfig(project.id),
    retry: false,
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  if (configs.isPending || (!configs.isFetchedAfterMount && !configs.isError)) return <p role="status">正在加载公众号设置…</p>
  if (configs.isError) return <div role="alert" className="space-y-3"><p>公众号设置加载失败：{getApiErrorMessage(configs.error, '请重试')}</p><Button variant="secondary" onClick={() => void configs.refetch()}>重试</Button></div>
  return <ChannelConfigEditor project={project} config={configs.data} onClose={onClose} onStateChange={onStateChange} embedded={embedded} />
}

export function ProjectChannelConfigDialog({ project, onClose }: Props) {
  const [editorState, setEditorState] = useState({ dirty: false, busy: false })
  const [discardOpen, setDiscardOpen] = useState(false)
  function requestClose() {
    if (editorState.busy) return
    if (editorState.dirty) setDiscardOpen(true)
    else onClose()
  }
  const onEditorStateChange = useCallback((dirty: boolean, busy: boolean) => setEditorState({ dirty, busy }), [])
  return (
    <><Dialog open onOpenChange={(open) => { if (!open) requestClose() }}>
      <DialogContent className="sm:max-w-lg" closeButtonDisabled={editorState.busy}>
        <DialogHeader>
          <DialogTitle>公众号设置</DialogTitle>
          <DialogDescription className="space-y-2">
            <ProjectIdentity project={project} compact showAgents={false} className="max-w-full" />
            <span className="block">管理此人设的公众号绑定与草稿箱交付。</span>
          </DialogDescription>
        </DialogHeader>
        <WechatPlatformSettingsPanel project={project} onStateChange={onEditorStateChange} onClose={requestClose} embedded={false} />
      </DialogContent>
    </Dialog>
    <AlertDialog open={discardOpen} onOpenChange={setDiscardOpen}>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>放弃未保存的公众号设置？</AlertDialogTitle><AlertDialogDescription>尚未保存的修改将丢失。</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter><AlertDialogCancel>继续编辑</AlertDialogCancel><AlertDialogAction onClick={onClose}>放弃修改</AlertDialogAction></AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
    </>
  )
}
