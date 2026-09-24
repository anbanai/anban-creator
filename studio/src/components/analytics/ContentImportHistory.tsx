import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api } from '@/lib/api'
import { invalidateContentAnalytics } from './ContentImportDialog'
import type { Project } from '@/types'

export default function ContentImportHistory({ project, onClose, onRevoked }: { project: Project; onClose: () => void; onRevoked?: () => void }) {
  const client = useQueryClient()
  const [page, setPage] = useState(0)
  const [target, setTarget] = useState<{ id: string; name: string }>()
  const query = useQuery({ queryKey: ['content-analytics-history', project.id, page], queryFn: async () => {
    const result = project.platform === 'article' ? await api.wechatAnalyticsImport.listBatches(project.id, { offset: page * 20, limit: 20 }) : await api.seednoteImport.listBatches(project.id, { offset: page * 20, limit: 20 })
    return { total: result.total, items: result.items.map((batch) => ({ id: batch.id, name: batch.file_name, date: batch.data_as_of_at, revoked: Boolean(batch.revoked_at) || batch.status === 'revoked', count: 'matched_rows' in batch ? batch.matched_rows : batch.resolved_rows })) }
  } })
  const revoke = useMutation({ mutationFn: async (id: string) => { if (project.platform === 'article') await api.wechatAnalyticsImport.revoke(project.id, id); else await api.seednoteImport.revoke(project.id, id) }, onSuccess: async () => { await invalidateContentAnalytics(client, project.id); setTarget(undefined); onRevoked?.() } })
  return <Dialog open onOpenChange={(open) => { if (!open && !revoke.isPending) onClose() }}><DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl" closeButtonDisabled={revoke.isPending}><DialogHeader><DialogTitle>导入记录</DialogTitle><DialogDescription>{project.name} · 撤销后该批次不再计入统计，文件和历史记录仍会保留。</DialogDescription></DialogHeader>
    {query.isPending ? <p role="status" className="py-8 text-center text-muted-foreground">读取导入记录…</p> : query.isError ? <div role="alert">{query.error.message}<Button variant="ghost" onClick={() => void query.refetch()}>重试</Button></div> : !query.data.items.length ? <p className="py-8 text-center text-muted-foreground">还没有导入记录</p> : <div className="divide-y">{query.data.items.map((batch) => <div key={batch.id} className="flex flex-wrap items-center justify-between gap-3 py-4"><div className="min-w-0 flex-1"><p className="break-all text-sm font-medium">{batch.name}</p><p className="mt-1 text-xs text-muted-foreground">{new Date(batch.date).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })} · {batch.revoked ? '已撤销 · 不计入统计' : `${batch.count} 条已导入`}</p></div>{!batch.revoked && <Button size="sm" variant="ghost" disabled={revoke.isPending} onClick={() => { setTarget({ id: batch.id, name: batch.name }); revoke.reset() }}>撤销本次导入</Button>}</div>)}</div>}
    {query.data && query.data.total > 20 && <div className="flex items-center justify-between text-sm"><Button variant="outline" disabled={!page || revoke.isPending} onClick={() => setPage(page - 1)}>上一页</Button><span>{page + 1} / {Math.ceil(query.data.total / 20)}</span><Button variant="outline" disabled={(page + 1) * 20 >= query.data.total || revoke.isPending} onClick={() => setPage(page + 1)}>下一页</Button></div>}
    {target && <section aria-label="确认撤销导入" className="space-y-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4"><p className="break-all text-sm font-medium">撤销「{target.name}」？</p><p className="text-sm text-muted-foreground">本批次将不再计入统计。其他批次和内容状态不变；如需更正关联，可撤销后重新导入。</p>{revoke.error && <p role="alert" className="text-sm text-destructive">{revoke.error.message}</p>}<div className="flex justify-end gap-2"><Button variant="outline" disabled={revoke.isPending} onClick={() => setTarget(undefined)}>取消撤销</Button><Button variant="destructive" disabled={revoke.isPending} onClick={() => revoke.mutate(target.id)}>{revoke.isPending ? '正在撤销…' : '确认撤销'}</Button></div></section>}
  </DialogContent></Dialog>
}
