import { useCallback, useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { MessagesSquare } from 'lucide-react'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import type { WechatPlatformConfig } from '@/types'
import { Button } from '@/components/common/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter, AlertDialogCancel, AlertDialogAction } from '@/components/ui/alert-dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ProjectIdentity, type ProjectIdentityProject } from '@/components/agent-prompt/ProjectIdentity'

type Credentials = { appID: string; secret: string }
interface Props {
  project: ProjectIdentityProject
  onClose: () => void
}

function ChannelConfigEditor({ project, config, onClose, onStateChange }: Props & { config: WechatPlatformConfig; onStateChange: (dirty: boolean, busy: boolean) => void }) {
  const queryClient = useQueryClient()
  const active = useRef(true)
  useEffect(() => {
    active.current = true
    return () => { active.current = false }
  }, [])
  const [draft, setDraft] = useState<Credentials>(() => ({ appID: config.app_id, secret: '' }))
  const [saved, setSaved] = useState(draft)
  const [secretConfigured, setSecretConfigured] = useState(config.secret_configured)
  const [credentialsConflict, setCredentialsConflict] = useState(config.credentials_conflict)
  const accountChanged = draft.appID.trim() !== saved.appID.trim()
  const requiresSecret = !secretConfigured || credentialsConflict || accountChanged
  const canSave = Boolean(draft.appID.trim()) && (!requiresSecret || Boolean(draft.secret.trim()))
  const credentialsReady = Boolean(draft.appID.trim()) && secretConfigured && !credentialsConflict && !accountChanged
  const save = useMutation({
    mutationFn: (credentials: Credentials) => api.projects.upsertWechatPlatformConfig(project.id, {
      app_id: credentials.appID.trim(),
      ...(credentials.secret.trim() ? { secret: credentials.secret.trim() } : {}),
    }),
    onSuccess: (result, credentials) => {
      void queryClient.invalidateQueries({ queryKey: ['project-wechat-platform-config', project.id], refetchType: 'none' })
      if (!active.current) return
      toast.success('自动发布设置已保存')
      const clean = { appID: credentials.appID.trim(), secret: '' }
      setDraft(clean)
      setSaved(clean)
      setSecretConfigured(result.secret_configured)
      setCredentialsConflict(result.credentials_conflict)
    },
    onError: (error) => {
      if (active.current) toast.error(getApiErrorMessage(error, '自动发布设置保存失败，请重试'))
    },
  })
  const dirty = draft.appID !== saved.appID || draft.secret !== saved.secret
  useEffect(() => { onStateChange(dirty, save.isPending) }, [dirty, save.isPending, onStateChange])
  function update(value: Partial<Credentials>) {
    setDraft((current) => ({ ...current, ...value }))
  }

  return (
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
          {credentialsConflict ? '账号待统一' : credentialsReady ? '已就绪' : '待完善'}
        </span>
      </div>
      {credentialsConflict && <p role="alert" className="text-sm text-amber-700 dark:text-amber-300">文章与贴图的旧账号配置不一致，请重新填写要统一使用的微信账号和密钥。</p>}
      <div className="space-y-2">
        <Label htmlFor="channel-app-id">微信 AppID</Label>
        <Input id="channel-app-id" value={draft.appID} required placeholder="wx..." disabled={save.isPending} onChange={(event) => update({ appID: event.target.value })} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="channel-secret">微信 AppSecret</Label>
        <Input id="channel-secret" type="password" autoComplete="new-password" value={draft.secret} placeholder={requiresSecret ? '请输入 AppSecret' : '留空则保持现有密钥'} required={requiresSecret} aria-describedby="channel-secret-help" disabled={save.isPending} onChange={(event) => update({ secret: event.target.value })} />
        <p id="channel-secret-help" className="text-xs leading-relaxed text-muted-foreground">
          {credentialsConflict ? '账号冲突时必须重新填写密钥，保存后文章和贴图将共用此账号。' : accountChanged ? '更换 AppID 时请填写对应的新密钥。' : requiresSecret ? '尚未配置密钥，请在公众号后台「设置与开发 → 基本配置」获取并填写。' : '密钥已安全保存；留空可继续使用现有密钥。'}
          {' '}保存后密钥不会回显。
        </p>
      </div>
      <DialogFooter>
        <Button type="button" variant="secondary" disabled={save.isPending} onClick={onClose}>关闭</Button>
        <Button type="submit" loading={save.isPending} disabled={!canSave}>保存</Button>
      </DialogFooter>
    </form>
  )
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
  const configs = useQuery({
    queryKey: ['project-wechat-platform-config', project.id],
    queryFn: () => api.projects.getWechatPlatformConfig(project.id),
    retry: false,
    staleTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  return (
    <><Dialog open onOpenChange={(open) => { if (!open) requestClose() }}>
      <DialogContent className="sm:max-w-lg" closeButtonDisabled={editorState.busy}>
        <DialogHeader>
          <DialogTitle>配置自动发布</DialogTitle>
          <DialogDescription className="space-y-2">
            <ProjectIdentity project={project} compact showAgents={false} className="max-w-full" />
            <span className="block">管理此项目的公众号自动发布账号。</span>
          </DialogDescription>
        </DialogHeader>
        {configs.isPending || (!configs.isFetchedAfterMount && !configs.isError) ? <p role="status">正在加载自动发布设置…</p> : configs.isError ? (
          <div role="alert" className="space-y-3">
            <p>自动发布设置加载失败：{getApiErrorMessage(configs.error, '请重试')}</p>
            <Button variant="secondary" onClick={() => void configs.refetch()}>重试</Button>
          </div>
        ) : <ChannelConfigEditor project={project} config={configs.data} onClose={requestClose} onStateChange={onEditorStateChange} />}
      </DialogContent>
    </Dialog>
    <AlertDialog open={discardOpen} onOpenChange={setDiscardOpen}>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>放弃未保存的自动发布设置？</AlertDialogTitle><AlertDialogDescription>尚未保存的修改将丢失。</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter><AlertDialogCancel>继续编辑</AlertDialogCancel><AlertDialogAction onClick={onClose}>放弃修改</AlertDialogAction></AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog></>
  )
}
