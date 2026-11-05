import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import type { TopicDigestDetail } from '@/types/topic-radar'

const statuses: Record<string,string> = { queued: '排队中', dispatched: '已派发', running: '生成中', succeeded: '已完成', failed: '失败', pending: '等待发送', sending: '发送中', sent: '已发送', invalid: '配置无效' }
export function DigestEffects({ detail }: { detail: TopicDigestDetail }) {
  const queryClient = useQueryClient()
  const [scriptId,setScriptId] = useState('')
  const retry = useMutation({ mutationFn: (id:string) => api.topicRadar.retryScript(id), onSuccess: async () => { await queryClient.invalidateQueries({queryKey:['topic-radar']}); toast.success('脚本已重新排队') }, onError: () => toast.error('脚本重试失败') })
  const artifact = useQuery({queryKey:['topic-radar','script-artifact',scriptId], queryFn:() => api.topicRadar.scriptArtifact(scriptId), enabled:Boolean(scriptId)})
  const download = () => { if (!artifact.data) return; const url = URL.createObjectURL(new Blob([JSON.stringify(artifact.data,null,2)],{type:'application/json'})); const link=document.createElement('a');link.href=url;link.download=`topic-script-${scriptId}.json`;link.click(); URL.revokeObjectURL(url) }
  return <>
    {detail.scripts.length > 0 && <section className="space-y-3 border-t pt-5"><h2 className="font-semibold">短视频脚本</h2><p className="text-xs text-muted-foreground">脚本失败不会影响已完成的分析和自动入库。</p>{detail.scripts.map((job) => <article key={job.id} className="rounded-lg border p-4"><div className="flex items-start justify-between gap-4"><div><h3 className="font-medium">{detail.suggestions.find((item) => item.id === job.suggestion_id)?.title ?? '短视频脚本'}</h3><p className="mt-1 text-xs text-muted-foreground">{job.target_platform} · {job.duration_seconds} 秒 · {job.aspect_ratio} · {statuses[job.status] ?? job.status}</p></div>{job.status === 'failed' && <Button size="sm" variant="outline" disabled={retry.isPending} onClick={() => retry.mutate(job.id)}>重试脚本</Button>}{job.status === 'succeeded' && <Button size="sm" variant="outline" onClick={() => setScriptId(job.id)}>查看脚本</Button>}</div>{job.error && <p role="alert" className="mt-2 text-sm text-destructive">{job.error}</p>}</article>)}</section>}
    {detail.notifications.length > 0 && <section className="space-y-2 border-t pt-5"><h2 className="font-semibold">通知投递</h2>{detail.notifications.map((delivery) => <div key={delivery.id} className="rounded-lg border p-3 text-sm"><span>{statuses[delivery.status] ?? delivery.status}</span><span className="ml-2 text-xs text-muted-foreground">尝试 {delivery.attempt} 次</span>{delivery.next_attempt_at && delivery.status === 'failed' && <p className="mt-1 text-xs text-muted-foreground">下次重试：{new Date(delivery.next_attempt_at).toLocaleString()}</p>}{delivery.last_error && <p className="mt-1 text-destructive">{delivery.last_error}</p>}</div>)}</section>}
    <Dialog open={Boolean(scriptId)} onOpenChange={(open) => {if(!open)setScriptId('')}}><DialogContent className="sm:max-w-2xl"><DialogHeader><DialogTitle>{artifact.data?.title ?? '短视频脚本'}</DialogTitle><DialogDescription>已校验的脚本产物，可下载完整 JSON。</DialogDescription></DialogHeader>{artifact.isPending && <p role="status">加载脚本…</p>}{artifact.isError && <div role="alert"><p>脚本读取失败。</p><Button variant="outline" onClick={() => artifact.refetch()}>重新加载</Button></div>}{artifact.data && <div className="space-y-4"><p className="rounded-lg bg-muted p-3">{artifact.data.hook}</p>{artifact.data.scenes.map((scene,index) => <div key={index} className="space-y-2 border-b pb-3"><h3 className="font-medium">镜头 {index+1} · {scene.duration_seconds} 秒</h3><p><span className="text-muted-foreground">画面：</span>{scene.visual}</p><p><span className="text-muted-foreground">旁白：</span>{scene.voiceover}</p>{scene.on_screen_text && <p>字幕：{scene.on_screen_text}</p>}</div>)}<p>行动引导：{artifact.data.cta}</p><Button onClick={download}>下载脚本 JSON</Button></div>}</DialogContent></Dialog>
  </>
}
