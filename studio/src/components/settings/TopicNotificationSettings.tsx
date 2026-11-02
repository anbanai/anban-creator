import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import type { TopicChannelSettings, TopicNotificationChannel } from '@/types/topic-radar'
const providerNames = { feishu:'飞书', wecom:'企业微信', dingtalk:'钉钉' }
const inputClass = 'h-9 rounded-md border bg-background px-2'

export default function TopicNotificationSettings() {
  const [open,setOpen] = useState(false)
  const [projectId,setProjectId] = useState('')
  const [editing,setEditing] = useState<{id?:string;form:TopicChannelSettings}|null>(null)
  const [deleting,setDeleting] = useState('')
  const queryClient = useQueryClient()
  const projectsQuery = useQuery({ queryKey:queryKeys.projects.list({status:'active'}),queryFn:() => api.projects.list({status:'active'}),enabled:open })
  const projects = projectsQuery.data ?? []
  const selectedProject = projects.some((project) => project.id === projectId) ? projectId : projects[0]?.id ?? ''
  const channelKey = ['topic-radar','channels',selectedProject]
  const channelsQuery = useQuery({ queryKey:channelKey, queryFn:() => api.topicRadar.channels(selectedProject), enabled:open && Boolean(selectedProject) })
  const refresh = () => queryClient.invalidateQueries({queryKey:channelKey})
  const save = useMutation({ mutationFn:async () => {
    const data = {...editing!.form}
    if (!data.webhook_url?.trim()) delete data.webhook_url
    return editing!.id ? api.topicRadar.updateChannel(editing!.id,data) : api.topicRadar.createChannel(selectedProject,data)
  }, onSuccess:async () => { setEditing(null);await refresh();toast.success('通知渠道已保存') },onError:() => toast.error('保存失败，请检查 HTTPS 地址和渠道配置') })
  const probe = useMutation({mutationFn:(id:string) => api.topicRadar.testChannel(id),onSuccess:() => toast.success('测试消息已发送'),onError:() => toast.error('测试失败，请检查机器人地址与平台配置')})
  const remove = useMutation({mutationFn:(id:string) => api.topicRadar.deleteChannel(id),onSuccess:async () => {setDeleting('');await refresh();toast.success('通知渠道已删除')},onError:() => toast.error('删除失败，请重试')})
  const edit = (channel:TopicNotificationChannel) => setEditing({id:channel.id,form:{name:channel.name,provider:channel.provider,enabled:channel.enabled,webhook_url:''}})
  const patch = (data:Partial<TopicChannelSettings>) => setEditing((value) => value ? {...value,form:{...value.form,...data}} : null)
  return <Card><div className="flex items-center justify-between border-b px-4 py-3"><div><h2 className="text-sm font-semibold">选题雷达通知</h2><p className="mt-1 text-xs text-muted-foreground">按项目配置飞书、企业微信和钉钉机器人。</p></div><Button variant="outline" size="sm" onClick={() => setOpen(!open)}>{open ? '收起渠道' : '管理通知渠道'}</Button></div>
    {open && <CardContent className="space-y-4 pt-4">
      {projectsQuery.isError && <p role="alert">项目加载失败，请稍后重试。</p>}
      <div className="flex flex-wrap gap-3"><label className="grid gap-1 text-xs">通知项目<select className={inputClass} value={selectedProject} onChange={(event) => {setProjectId(event.target.value);setDeleting('')}}>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label><Button className="self-end" disabled={!selectedProject} onClick={() => setEditing({form:{name:'选题日报',provider:'feishu',enabled:true,webhook_url:''}})}>添加通知渠道</Button></div>
      {channelsQuery.isLoading && <p role="status">正在加载渠道…</p>}
      {channelsQuery.isError && <p role="alert">渠道加载失败。</p>}
      {selectedProject && channelsQuery.data?.items.length === 0 && <p className="text-sm text-muted-foreground">暂无通知渠道。分析和自动入库不受影响。</p>}
      {channelsQuery.data?.items.map((channel) => <div key={channel.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3"><div><p className="text-sm font-medium">{channel.name}</p><p className="text-xs text-muted-foreground">{providerNames[channel.provider]} · {channel.enabled ? '启用' : '暂停'} · Webhook 已安全保存</p></div><div className="flex gap-2"><Button size="sm" variant="outline" onClick={() => edit(channel)}>编辑</Button><Button size="sm" variant="outline" disabled={probe.isPending} onClick={() => probe.mutate(channel.id)}>发送测试</Button>{deleting === channel.id ? <><Button size="sm" variant="destructive" disabled={remove.isPending} onClick={() => remove.mutate(channel.id)}>确认删除</Button><Button size="sm" variant="ghost" onClick={() => setDeleting('')}>取消</Button></> : <Button size="sm" variant="ghost" onClick={() => setDeleting(channel.id)}>删除</Button>}</div></div>)}
    </CardContent>}
    {editing && <Dialog open onOpenChange={(value) => {if(!value)setEditing(null)}}><DialogContent><DialogHeader><DialogTitle>{editing.id ? '编辑通知渠道' : '添加通知渠道'}</DialogTitle><DialogDescription>地址仅存于服务端；保存后不会回显。发送测试会向机器人发送一条测试消息。</DialogDescription></DialogHeader><form className="space-y-4" onSubmit={(event) => {event.preventDefault();save.mutate()}}><label className="grid gap-1">渠道名称<input className={inputClass} required maxLength={120} value={editing.form.name} onChange={(event) => patch({name:event.target.value})} /></label><label className="grid gap-1">平台<select className={inputClass} value={editing.form.provider} onChange={(event) => patch({provider:event.target.value as TopicNotificationChannel['provider']})}>{Object.entries(providerNames).map(([value,label]) => <option key={value} value={value}>{label}</option>)}</select></label><label className="grid gap-1">Webhook URL<input className={inputClass} type="password" autoComplete="new-password" required={!editing.id} value={editing.form.webhook_url ?? ''} placeholder={editing.id ? '留空保留现有地址' : 'https://…'} onChange={(event) => patch({webhook_url:event.target.value})} /></label><label className="flex gap-2"><input type="checkbox" checked={editing.form.enabled} onChange={(event) => patch({enabled:event.target.checked})} />启用通知</label><div className="flex justify-end gap-2"><Button variant="outline" type="button" onClick={() => setEditing(null)}>取消</Button><Button type="submit" disabled={save.isPending}>保存渠道</Button></div></form></DialogContent></Dialog>}
  </Card>
}
