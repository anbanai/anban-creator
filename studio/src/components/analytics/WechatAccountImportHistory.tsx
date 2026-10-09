import { useState } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ProjectIdentity } from '@/components/agent-prompt/ProjectIdentity'
import { wechatAccountAnalyticsImportApi, type WechatAccountAnalyticsBatch } from '@/lib/api/wechat-account-analytics-import'
import type { Project } from '@/types'

const PAGE_SIZE = 20

function formatDate(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })
}

function statusLabel(batch: WechatAccountAnalyticsBatch) {
  if (batch.revoked_at || batch.status === 'revoked') return '已撤销 · 不计入统计'
  if (batch.status === 'completed' || batch.status === 'active') return '已生效'
  if (batch.status === 'processing') return '处理中'
  if (batch.status === 'failed') return '失败'
  return batch.status || '未知状态'
}

export default function WechatAccountImportHistory({ project, onClose, onRevoked }: { project: Project; onClose: () => void; onRevoked?: () => void }) {
  const client = useQueryClient()
  const [page, setPage] = useState(0)
  const [target, setTarget] = useState<WechatAccountAnalyticsBatch>()
  const query = useQuery({
    queryKey: ['wechat-account-analytics', project.id, 'imports', page],
    queryFn: ({ signal }) => wechatAccountAnalyticsImportApi.listBatches(project.id, { offset: page * PAGE_SIZE, limit: PAGE_SIZE }, signal),
    placeholderData: keepPreviousData,
  })
  const revoke = useMutation({
    mutationFn: (batchId: string) => wechatAccountAnalyticsImportApi.revoke(project.id, batchId),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ['wechat-account-analytics', project.id] })
      setTarget(undefined)
      onRevoked?.()
    },
  })

  return <Dialog open onOpenChange={(open) => { if (!open && !revoke.isPending) onClose() }}>
    <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl" closeButtonDisabled={revoke.isPending}>
      <DialogHeader>
        <DialogTitle>公众号账号导入记录</DialogTitle>
        <DialogDescription className="flex items-center gap-2"><ProjectIdentity project={project} compact /> · 撤销后该批次不再计入账号统计，原文件和记录仍会保留。</DialogDescription>
      </DialogHeader>

      {query.isPending ? <p role="status" className="py-8 text-center text-muted-foreground">读取账号导入记录…</p>
        : query.isError ? <div role="alert" className="space-y-2"><p>{query.error.message}</p><Button variant="ghost" onClick={() => void query.refetch()}>重试</Button></div>
          : !query.data.items.length ? <p className="py-8 text-center text-muted-foreground">还没有账号统计导入记录</p>
            : <div className="divide-y">
              {query.data.items.map((batch) => {
                const revoked = Boolean(batch.revoked_at) || batch.status === 'revoked'
                const accountDays = (batch as WechatAccountAnalyticsBatch & { account_day_count?: number }).account_day_count
                return <div key={batch.id} className="flex flex-wrap items-center justify-between gap-3 py-4">
                  <div className="min-w-0 flex-1">
                    <p className="break-all text-sm font-medium">{batch.file_name || '未命名导入文件'}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      数据截至 {formatDate(batch.data_as_of_at)} · {statusLabel(batch)}{accountDays == null ? '' : ` · ${accountDays} 个账号日`}
                    </p>
                    {batch.received_at && <p className="mt-1 text-xs text-muted-foreground">导入于 {formatDate(batch.received_at)}</p>}
                  </div>
                  {!revoked && <Button size="sm" variant="ghost" disabled={revoke.isPending} onClick={() => { setTarget(batch); revoke.reset() }}>撤销本次导入</Button>}
                </div>
              })}
            </div>}

      {query.data && query.data.total > PAGE_SIZE && <div className="flex items-center justify-between border-t pt-3 text-sm">
        <Button variant="outline" disabled={page === 0 || revoke.isPending} onClick={() => setPage(page - 1)}>上一页</Button>
        <span>{page + 1} / {Math.ceil(query.data.total / PAGE_SIZE)}</span>
        <Button variant="outline" disabled={(page + 1) * PAGE_SIZE >= query.data.total || revoke.isPending} onClick={() => setPage(page + 1)}>下一页</Button>
      </div>}

      {target && <section aria-label="确认撤销账号统计导入" className="space-y-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4">
        <p className="break-all text-sm font-medium">撤销「{target.file_name || '未命名导入文件'}」？</p>
        <p className="text-sm text-muted-foreground">该批次将不再参与账号级趋势、汇总和来源分析；其他导入批次不受影响。</p>
        {revoke.error && <p role="alert" className="text-sm text-destructive">{revoke.error.message}</p>}
        <div className="flex justify-end gap-2"><Button variant="outline" disabled={revoke.isPending} onClick={() => setTarget(undefined)}>取消撤销</Button><Button variant="destructive" disabled={revoke.isPending} onClick={() => revoke.mutate(target.id)}>{revoke.isPending ? '正在撤销…' : '确认撤销'}</Button></div>
      </section>}
    </DialogContent>
  </Dialog>
}
