import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Play } from 'lucide-react'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { taskTypeLabelCN } from '@/lib/labels'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

const runLabels: Record<string, string> = { queued: '等待生产', running: '自动生产中', paused: '已暂停', failed: '失败', completed: '已完成', cancelled: '已取消' }
const stageLabels: Record<string, string> = { research: '研究', scoring: '评分', brief: '平台 Brief', produce: '内容生成', review: '质量检查', schedule: '排期', publish: '发布', observe: '数据复盘' }

export default function ContentLineagePanel({ assetID, runID, taskID }: { assetID: string; runID?: string; taskID?: string }) {
  const queryClient = useQueryClient()
  const resume = useMutation({
    mutationFn: () => api.contentAssets.setRunStatus(runID!, 'queued'),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['content-reuse-runs', runID] }),
        queryClient.invalidateQueries({ queryKey: ['task', taskID] }),
        queryClient.invalidateQueries({ queryKey: ['tasks'] }),
      ])
    },
  })
  const lineage = useQuery({ queryKey: ['content-asset-lineage', assetID], queryFn: () => api.contentAssets.getLineage(assetID) })
  const run = useQuery({ queryKey: ['content-reuse-runs', runID], queryFn: () => api.contentAssets.getRun(runID!), enabled: !!runID, refetchInterval: query => ['queued', 'running'].includes(query.state.data?.status ?? '') ? 5000 : false })
  const assetHref = `/content-assets?asset_id=${encodeURIComponent(assetID)}`
  return <section className="space-y-3 rounded-lg border border-border/70 bg-card p-4" aria-label="内容链路">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="font-medium">内容链路</h2>
      <Link className="text-sm text-primary hover:underline" to={`/content-assets?reuse_asset_id=${encodeURIComponent(assetID)}${taskID ? `&source_task_id=${encodeURIComponent(taskID)}` : ''}`}>跨平台复用</Link>
    </div>
    {lineage.isError ? <p role="alert">选题链路加载失败。<button type="button" onClick={() => void lineage.refetch()}>重试</button></p> : <Link className="text-sm text-primary hover:underline" to={assetHref}>{lineage.data?.asset.title ?? '查看选题资产'}</Link>}
    {run.data && <div className="space-y-1 text-sm text-muted-foreground">
      <p>{taskTypeLabelCN[run.data.target_platform] ?? run.data.target_platform} · {runLabels[run.data.status] ?? run.data.status} · {stageLabels[run.data.stage] ?? run.data.stage}</p>
      <p>生产成本：<span>{run.data.cost_credits} 积分</span></p>
      {run.data.error_message && <p role="status">{run.data.error_message}</p>}
      {run.data.status === 'paused' && run.data.error_code !== 'operation_reconciliation_required' && <Tooltip>
        <TooltipTrigger render={<Button variant="ghost" size="icon-md" aria-label="恢复运行" disabled={resume.isPending} aria-busy={resume.isPending} onClick={() => resume.mutate()} />}>
          <Play className="size-4" aria-hidden="true" />
        </TooltipTrigger>
        <TooltipContent>恢复运行</TooltipContent>
      </Tooltip>}
      {resume.isError && <p role="alert">恢复运行失败，请重试。</p>}
    </div>}
    {run.isError && <p role="alert">自动生产状态加载失败。</p>}
    {!!lineage.data?.links.length && <ul className="space-y-2 text-sm">
      {lineage.data.links.map(link => <li key={link.id} className="flex flex-wrap gap-2">
        <span className="text-muted-foreground">{{ origin: '来源', derivative: '派生', reference: '参考' }[link.relation] ?? link.relation} · {taskTypeLabelCN[link.platform ?? ''] ?? link.platform}</span>
        {['task', 'video_task'].includes(link.content_kind) ? <Link className="text-primary hover:underline" to={`/tasks/${encodeURIComponent(link.content_id)}`}>{link.angle || link.content_id}</Link> : <span>{link.angle || link.content_kind}</span>}
      </li>)}
    </ul>}
    {lineage.data?.summary && <div className="grid gap-2 border-t border-border pt-3 text-xs text-muted-foreground sm:grid-cols-3" aria-label="内容链路汇总">
      <span>运行 {lineage.data.summary.run_count} 次</span><span>成本 {lineage.data.summary.cost_credits} 积分</span><span>已发布 {lineage.data.summary.published_count} 条</span>
    </div>}
    {!!lineage.data?.platform_performance.length && <div className="space-y-2 border-t border-border pt-3" aria-label="平台表现">
      {lineage.data.platform_performance.map(performance => <div key={`${performance.project_id}:${performance.platform}`} className="text-xs">
        <div className="flex justify-between gap-2"><span>{performance.platform} · {performance.content_count} 条内容</span><span>{performance.analytics_revision ? `revision ${performance.analytics_revision}` : '暂无分析'}</span></div>
        {performance.missing?.length ? <p className="mt-1 text-muted-foreground">{performance.missing.join('、')}</p> : null}
      </div>)}
    </div>}
  </section>
}
