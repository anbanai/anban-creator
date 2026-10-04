import { useCallback, useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { getApiErrorMessage } from '@/lib/http-client'
import type { ProjectChannelConfig } from '@/types'
import { Button } from '@/components/common/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter, AlertDialogCancel, AlertDialogAction } from '@/components/ui/alert-dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const channels = [
  { value: 'wechat-article', label: '公众号文章' },
  { value: 'wechat-picture', label: '公众号图文' },
] as const

type Channel = typeof channels[number]['value']
type Credentials = { appID: string; secret: string }
interface Props {
  project: { id: string; name: string }
  onClose: () => void
}

function ChannelConfigEditor({ project, configs, onClose, onStateChange }: Props & { configs: ProjectChannelConfig[]; onStateChange: (dirty: boolean, busy: boolean) => void }) {
  const queryClient = useQueryClient()
  const active = useRef(true)
  useEffect(() => {
    active.current = true
    return () => { active.current = false }
  }, [])
  const [channel, setChannel] = useState<Channel>('wechat-article')
  const [drafts, setDrafts] = useState<Record<Channel, Credentials>>(() => Object.fromEntries(
    channels.map(({ value }) => {
      const config = configs.find((item) => item.channel === value)?.config
      return [value, { appID: typeof config?.wechat_app_id === 'string' ? config.wechat_app_id : '', secret: '' }]
    }),
  ) as Record<Channel, Credentials>)
  const [saved, setSaved] = useState(drafts)
  const draft = drafts[channel]
  const save = useMutation({
    mutationFn: ({ channel, credentials }: { channel: Channel; credentials: Credentials }) => api.projects.upsertChannelConfig(project.id, channel, {
      wechat_app_id: credentials.appID.trim(),
      ...(credentials.secret.trim() ? { wechat_secret: credentials.secret.trim() } : {}),
    }),
    onSuccess: (_result, { channel: savedChannel, credentials }) => {
      void queryClient.invalidateQueries({ queryKey: ['project-channel-configs', project.id], refetchType: 'none' })
      if (!active.current) return
      toast.success('渠道配置已保存')
      const clean = { appID: credentials.appID.trim(), secret: '' }
      setDrafts((current) => ({ ...current, [savedChannel]: clean }))
      setSaved((current) => ({ ...current, [savedChannel]: clean }))
    },
    onError: (error) => {
      if (active.current) toast.error(getApiErrorMessage(error, '渠道配置保存失败，请重试'))
    },
  })
  const dirty = channels.some(({ value }) => drafts[value].appID !== saved[value].appID || drafts[value].secret !== saved[value].secret)
  useEffect(() => { onStateChange(dirty, save.isPending) }, [dirty, save.isPending, onStateChange])
  function update(value: Partial<Credentials>) {
    setDrafts((current) => ({ ...current, [channel]: { ...current[channel], ...value } }))
  }

  return (
    <form onSubmit={(event) => {
      event.preventDefault()
      if (!save.isPending && draft.appID.trim()) save.mutate({ channel, credentials: draft })
    }} className="space-y-4">
      <div className="flex gap-2" aria-label="发布渠道">
        {channels.map((item) => (
          <Button key={item.value} type="button" variant={channel === item.value ? 'default' : 'secondary'} aria-pressed={channel === item.value} disabled={save.isPending} onClick={() => setChannel(item.value)}>
            {item.label}
          </Button>
        ))}
      </div>
      <div className="space-y-2">
        <Label htmlFor="channel-app-id">微信 AppID</Label>
        <Input id="channel-app-id" value={draft.appID} required placeholder="wx..." disabled={save.isPending} onChange={(event) => update({ appID: event.target.value })} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="channel-secret">微信 AppSecret</Label>
        <Input id="channel-secret" type="password" autoComplete="new-password" value={draft.secret} placeholder="留空则保持原有密钥不变" disabled={save.isPending} onChange={(event) => update({ secret: event.target.value })} />
        <p className="text-xs text-muted-foreground">在公众号后台「设置与开发 → 基本配置」获取。凭证仅用于当前渠道，密钥保存后不回显。</p>
      </div>
      <DialogFooter>
        <Button type="button" variant="secondary" disabled={save.isPending} onClick={onClose}>关闭</Button>
        <Button type="submit" loading={save.isPending} disabled={!draft.appID.trim()}>保存</Button>
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
    queryKey: ['project-channel-configs', project.id],
    queryFn: () => api.projects.listChannelConfigs(project.id),
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
          <DialogTitle>渠道配置</DialogTitle>
          <DialogDescription>为「{project.name}」配置发布账号。项目可供不同创作任务共用。</DialogDescription>
        </DialogHeader>
        {configs.isPending || (!configs.isFetchedAfterMount && !configs.isError) ? <p role="status">正在加载渠道配置…</p> : configs.isError ? (
          <div role="alert" className="space-y-3">
            <p>渠道配置加载失败，请重试。</p>
            <Button variant="secondary" onClick={() => void configs.refetch()}>重试</Button>
          </div>
        ) : <ChannelConfigEditor project={project} configs={configs.data} onClose={requestClose} onStateChange={onEditorStateChange} />}
      </DialogContent>
    </Dialog>
    <AlertDialog open={discardOpen} onOpenChange={setDiscardOpen}>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>放弃未保存的渠道配置？</AlertDialogTitle><AlertDialogDescription>尚未保存的修改将丢失。</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter><AlertDialogCancel>继续编辑</AlertDialogCancel><AlertDialogAction onClick={onClose}>放弃修改</AlertDialogAction></AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog></>
  )
}
