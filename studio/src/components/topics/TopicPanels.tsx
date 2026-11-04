import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SubscriptionSettings } from '@/components/topic-radar/SubscriptionSettings'
import { DigestEffects } from '@/components/topic-radar/DigestEffects'
import ContentLineagePanel from '@/components/tasks/ContentLineagePanel'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { invalidateTopics, topicCreationHref, isTopicScript, topicOutputHref } from '@/lib/topics'
import type { TopicDigestDetail } from '@/types/topic-radar'
import type { ContentAsset, Project } from '@/types'
const selectClass = 'h-10 rounded-md border bg-background px-3 text-sm'
const active = ['queued', 'pending', 'preparing', 'dispatched', 'running', 'finalizing']
const statusLabel: Record<string, string> = { queued: '排队中', pending: '等待中', preparing: '准备中', dispatched: '已派发', running: '生成中', finalizing: '整理结果', succeeded: '已完成', completed: '已完成', failed: '失败', cancelled: '已取消', paused: '已暂停' }
export function discoveryNeedsPolling(detail?: TopicDigestDetail) {
  return !!detail && (active.includes(detail.digest.status)
    || detail.scripts.some(script => active.includes(script.status))
    || detail.suggestions.some(item => ['pending', 'processing'].includes(item.adoption_status))
    || detail.adoptions.some(item => ['pending', 'processing'].includes(item.status))
    || detail.notifications.some(item => ['pending', 'sending'].includes(item.status) || (item.status === 'failed' && !!item.next_attempt_at)))
}
type UpdateParams = (values: Record<string, string | undefined>, replace?: boolean) => void
export function TopicDiscovery({ projectId, settings, params, update }: { projectId: string; settings: boolean; params: URLSearchParams; update: UpdateParams }) {
  const client = useQueryClient()
  const subscriptionId = params.get('subscription_id') ?? ''
  const subscriptions = useQuery({ queryKey: queryKeys.topicRadar.subscriptions(projectId), queryFn: () => api.topicRadar.subscriptions(projectId), enabled: !!projectId })
  const subscription = subscriptions.data?.items.find(item => item.id === subscriptionId) ?? subscriptions.data?.items[0]
  const [editing, setEditing] = useState(false)
  const runId = params.get('run') ?? ''
  const digests = useQuery({ queryKey: queryKeys.topicRadar.digests(subscription?.id ?? ''), queryFn: () => api.topicRadar.digests(subscription!.id), enabled: !!subscription, refetchInterval: query => query.state.data?.items.some(item => active.includes(item.status)) ? 3000 : false })
  const digest = useQuery({ queryKey: ['topic-radar', 'detail', runId], queryFn: () => api.topicRadar.digest(runId), enabled: !!runId, refetchInterval: query => discoveryNeedsPolling(query.state.data) ? 3000 : false })
  useEffect(() => {
    if (digest.data?.digest.status === 'succeeded') {
      for (const key of ['content-assets', 'projects', 'project-stats']) void client.invalidateQueries({ queryKey: [key] })
    }
  }, [digest.data?.digest.status, digest.data?.suggestions.map(item => item.adoption_status).join('|'), digest.data?.scripts.map(item => `${item.id}:${item.status}:${item.task_id ?? ''}`).join('|'), client])
  const start = useMutation({ mutationFn: async () => {
    const selected = subscription ?? await api.topicRadar.createSubscription(projectId, { name: '每日选题', enabled: false, timezone: 'Asia/Shanghai', schedule: { hour: 9, minute: 0 }, script_policy: { enabled: false } })
    if (settings) return { subscription: selected }
    return { digest: await api.topicRadar.run(selected.id, { request_key: crypto.randomUUID() }) }
  }, onSuccess: async result => { await invalidateTopics(client); if (result.digest) update({ run: result.digest.id }); else setEditing(true) }, onError: () => toast.error('启动失败，请稍后重试') })
  const retry = useMutation({ mutationFn: () => api.topicRadar.retryDigest(runId), onSuccess: async result => { update({ run: result.id }); await invalidateTopics(client) }, onError: () => toast.error('重新查找失败') })
  return <section className="space-y-4">
    {!projectId && <p className="text-sm text-muted-foreground">选择人设后，按画像寻找适合的选题。</p>}
    {subscriptions.isError && <p role="alert">自动化配置加载失败。<Button onClick={() => void subscriptions.refetch()}>重试</Button></p>}
    {(subscriptions.data?.items.length ?? 0) > 1 && <select aria-label="选题计划" className={selectClass} value={subscription?.id ?? ''} onChange={event => update({ subscription_id: event.target.value, run: undefined })}>{subscriptions.data?.items.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select>}
    {subscription && <p className="text-sm text-muted-foreground">{subscription.name} · {subscription.enabled ? '定时开启' : '手动运行'}{subscription.next_run_at ? ` · 下次 ${new Date(subscription.next_run_at).toLocaleString()}` : ''}</p>}
    <Button disabled={!projectId || start.isPending || subscriptions.isFetching || subscriptions.isError} onClick={() => settings && subscription ? setEditing(true) : start.mutate()}>{settings ? '配置自动找选题' : start.isPending ? '正在启动…' : '开始找选题'}</Button>
    {editing && subscription && <SubscriptionSettings subscription={subscription} onClose={() => setEditing(false)} onSaved={() => invalidateTopics(client)} onDeleted={async () => { setEditing(false); update({ subscription_id: undefined }); await invalidateTopics(client) }} />}
    {runId && <div aria-live="polite" className="space-y-3 rounded-lg border p-4">{digest.isPending ? '正在读取进度…' : digest.isError ? <p role="alert">进度读取失败。<Button onClick={() => void digest.refetch()}>重试</Button></p> : <><p>{statusLabel[digest.data?.digest.status ?? ''] ?? digest.data?.digest.status} · {digest.data?.digest.candidate_count ?? 0} 个候选</p>{digest.data?.digest.error && <p role="alert">{digest.data.digest.error}</p>}{digest.data?.digest.status === 'failed' && <Button disabled={retry.isPending} onClick={() => retry.mutate()}>重新查找</Button>}{digest.data?.digest.status === 'succeeded' && <Button onClick={() => update({ detail: undefined, run: undefined, settings: undefined })}>查看入库选题</Button>}{digest.data && <details><summary>本次证据与执行记录</summary>{digest.data.suggestions.map(item => <article key={item.id} className="border-b py-3"><h3>{item.title}</h3><p className="text-sm text-muted-foreground">{item.source_summary}</p>{item.source_refs?.map((source, index) => <p key={index}>{source.title || source.label}</p>)}</article>)}<DigestEffects detail={digest.data} /></details>}</>}</div>}
    <details><summary className="cursor-pointer font-medium">查找历史</summary>{digests.isError && <p role="alert">历史加载失败。</p>}{digests.data?.items.map(item => <button key={item.id} className="block w-full border-b py-3 text-left text-sm" onClick={() => update({ run: item.id })}>{new Date(item.created_at).toLocaleString()} · {statusLabel[item.status] ?? item.status} · {item.candidate_count} 个</button>)}</details>
  </section>
}
export function TopicDetail({ asset, projectId, project, params, update }: { asset: ContentAsset; projectId: string; project?: Project; params: URLSearchParams; update: UpdateParams }) {
  const client = useQueryClient()
  const detail = useQuery({ queryKey: queryKeys.topics.detail(asset.asset_id), queryFn: () => api.contentAssets.get(asset.asset_id), refetchInterval: query => query.state.data?.asset.outputs?.some(item => active.includes(item.status)) ? 3000 : false })
  const [lineageOpen, setLineageOpen] = useState(false)
  const [platform, setPlatform] = useState('douyin')
  const [duration, setDuration] = useState(60)
  const [aspect, setAspect] = useState('9:16')
  const scriptId = params.get('script_id') ?? ''
  const script = useQuery({ queryKey: ['topic-radar', 'script', scriptId], queryFn: () => api.topicRadar.script(scriptId), enabled: !!scriptId, refetchInterval: query => active.includes(query.state.data?.status ?? '') ? 3000 : false })
  useEffect(() => { if (script.data && !active.includes(script.data.status)) void invalidateTopics(client) }, [script.data?.status, client])
  const requestScript = useMutation({ mutationFn: () => api.contentAssets.requestScript(asset.asset_id, { project_id: projectId, target_platform: platform, duration_seconds: duration, aspect_ratio: aspect }), onSuccess: async job => { update({ script_id: job.id }); await invalidateTopics(client) }, onError: () => toast.error('脚本任务提交失败') })
  const retryScript = useMutation({ mutationFn: () => api.topicRadar.retryScript(scriptId), onSuccess: async () => { await invalidateTopics(client) }, onError: () => toast.error('脚本重试失败') })
  const score = useMutation({ mutationFn: () => api.contentAssets.score(asset.asset_id, projectId), onSuccess: () => invalidateTopics(client), onError: () => toast.error('评分失败') })
  const reuse = useMutation({ mutationFn: () => api.contentAssets.createReuseRun(asset.asset_id, { target_project_id: projectId, target_platform: project?.platform ?? '', idempotency_key: crypto.randomUUID(), ...(params.get('source_task_id') ? { source_content_kind: 'task', source_content_id: params.get('source_task_id')! } : {}) }), onSuccess: async run => { update({ reuse_run: run.id }); await invalidateTopics(client) }, onError: () => toast.error('复用启动失败') })
  const reuseOpen = params.get('detail') === 'reuse'
  const runs = useQuery({ queryKey: ['content-reuse-runs', asset.asset_id], queryFn: () => api.contentAssets.listRuns({ asset_id: asset.asset_id }), enabled: reuseOpen, refetchInterval: query => query.state.data?.items.some(run => active.includes(run.status)) ? 3000 : false })
  const currentAsset = detail.data?.asset ?? asset
  return <div className="space-y-5">
    <section><h3 className="text-xs text-muted-foreground">切入角度</h3><p className="mt-2 leading-7">{asset.core_question || asset.title}</p></section><section><h3 className="text-xs text-muted-foreground">推荐理由</h3><p className="mt-2 text-sm leading-6">{asset.semantic_kernel || detail.data?.evidence?.[0]?.notes || '结合人设定位，选择最适合受众的问题展开。'}</p></section>
    {projectId ? <Link className="inline-flex rounded-md bg-primary px-4 py-2 text-primary-foreground" to={topicCreationHref(asset, projectId)}>开始创作</Link> : <p className="text-sm text-muted-foreground">选择创作人设后即可开始创作。</p>}
    <section aria-label="人设使用情况"><h3 className="mb-2 font-medium">人设使用情况</h3>{currentAsset.project_uses?.length ? currentAsset.project_uses.map(use => <p key={use.project_id} className="py-1 text-sm">{use.project_name} · {use.status === 'used' ? '已使用' : '未使用'}{use.task_id && <Link className="ml-3 underline" to={`/tasks/${use.task_id}`}>查看作品</Link>}</p>) : <p className="text-sm text-muted-foreground">尚未用于创作</p>}</section>
    <section aria-label="创作结果"><h3 className="mb-2 font-medium">创作结果</h3>{currentAsset.outputs?.length ? currentAsset.outputs.map(output => <div key={output.job_id || output.task_id} className="border-b py-3 text-sm">{topicOutputHref(asset.asset_id, output) ? <Link className="underline" to={topicOutputHref(asset.asset_id, output)!}>{isTopicScript(output) ? '脚本' : '作品'} · {output.title || asset.title} · {statusLabel[output.status] ?? output.status}</Link> : <span>{output.title || asset.title} · {statusLabel[output.status] ?? output.status}</span>}{output.error && <p role="alert">{output.error}</p>}</div>) : <p className="text-sm text-muted-foreground">还没有创作结果</p>}</section>
    <details open={params.get('detail') === 'script'}><summary className="cursor-pointer font-medium" onClick={event => { event.preventDefault(); update({ detail: params.get('detail') === 'script' ? undefined : 'script' }, true) }}>生成短视频脚本</summary><form className="mt-3 grid gap-3" onSubmit={event => { event.preventDefault(); requestScript.mutate() }}><label>发布平台<select className={`${selectClass} ml-2`} value={platform} onChange={event => setPlatform(event.target.value)}><option value="douyin">抖音</option><option value="channels">视频号</option><option value="bilibili">B站</option><option value="xiaohongshu">小红书</option></select></label><label>时长（秒）<Input type="number" min={15} max={300} value={duration} onChange={event => setDuration(Number(event.target.value))} /></label><label>画幅<select className={`${selectClass} ml-2`} value={aspect} onChange={event => setAspect(event.target.value)}><option>9:16</option><option>16:9</option><option>1:1</option></select></label><Button disabled={!projectId || requestScript.isPending || duration < 15 || duration > 300}>提交脚本任务</Button></form>{script.data && <p className="mt-3">脚本：{statusLabel[script.data.status] ?? script.data.status}{script.data.task_id && <Link className="ml-2 underline" to={`/tasks/${script.data.task_id}`}>查看脚本结果</Link>}{script.data.error && <span role="alert"> · {script.data.error}</span>}{script.data.status === 'failed' && <Button disabled={retryScript.isPending} onClick={() => retryScript.mutate()}>重试脚本</Button>}</p>}{script.isError && <p role="alert">脚本状态读取失败</p>}</details>
    <details><summary className="cursor-pointer font-medium">来源证据</summary>{detail.data?.evidence?.map(item => <article key={item.id} className="border-b py-3 text-sm"><p>{item.title || item.source_type}</p><p>{item.notes}</p>{item.url && /^https?:\/\//i.test(item.url) && <a href={item.url} target="_blank" rel="noreferrer" className="underline">查看来源</a>}</article>)}</details>
    <details><summary className="cursor-pointer font-medium">评分与内容链路</summary><Button variant="outline" className="my-3" disabled={!projectId || score.isPending} onClick={() => score.mutate()}>更新平台评分</Button>{detail.data?.scores?.map(item => <p key={item.id} className="py-2 text-sm">{item.platform} · {item.status === 'scored' ? `${item.score.toFixed(0)} 分` : item.status === 'exploratory' ? '探索信号' : '数据不足'} · revision {item.analytics_revision}</p>)}{detail.data?.links?.map(item => <p key={item.id} className="py-2 text-sm">{item.relation} · {item.platform} · {item.angle}{item.content_kind === 'task' && <Link className="ml-2 underline" to={`/tasks/${item.content_id}`}>查看内容</Link>}</p>)}</details>
    <details onToggle={event => setLineageOpen(event.currentTarget.open)}><summary className="cursor-pointer font-medium">平台表现与内容链路</summary>{lineageOpen && <ContentLineagePanel assetID={asset.asset_id} />}</details>
    <details open={reuseOpen}><summary className="cursor-pointer font-medium" onClick={event => { event.preventDefault(); update({ detail: reuseOpen ? undefined : 'reuse' }, true) }}>跨平台复用与运行历史</summary><Button className="my-3" disabled={!projectId || reuse.isPending} onClick={() => reuse.mutate()}>开始复用</Button>{runs.isError && <p role="alert">复用历史读取失败</p>}{runs.data?.items.map(run => <details key={run.id} open={params.get('reuse_run') === run.id} className="border-b py-3"><summary onClick={event => { event.preventDefault(); update({ reuse_run: params.get('reuse_run') === run.id ? undefined : run.id }, true) }}>{run.target_platform} · {statusLabel[run.status] ?? run.status} · {new Date(run.created_at).toLocaleString()}</summary>{params.get('reuse_run') === run.id && <ReuseRun id={run.id} />}</details>)}</details>
  </div>
}
function ReuseRun({ id }: { id: string }) {
  const client = useQueryClient()
  const run = useQuery({ queryKey: ['content-reuse-runs', 'detail', id], queryFn: () => api.contentAssets.getRun(id), refetchInterval: query => active.includes(query.state.data?.status ?? '') ? 3000 : false })
  const change = useMutation({ mutationFn: (status: 'paused' | 'queued' | 'cancelled') => api.contentAssets.setRunStatus(id, status), onSuccess: () => invalidateTopics(client), onError: () => toast.error('运行更新失败') })
  return <div className="space-y-2 py-3 text-sm">{run.isError && <p role="alert">运行读取失败</p>}<p>{run.data?.stage} · 已用 {run.data?.cost_credits ?? 0} 积分</p>{run.data?.error_message && <p role="alert">{run.data.error_message}</p>}<div className="flex gap-2">{active.includes(run.data?.status ?? '') && <Button size="sm" disabled={change.isPending} onClick={() => change.mutate('paused')}>暂停</Button>}{run.data?.status === 'paused' && <Button size="sm" disabled={change.isPending} onClick={() => change.mutate('queued')}>恢复</Button>}{[...active, 'paused'].includes(run.data?.status ?? '') && <Button size="sm" variant="outline" disabled={change.isPending} onClick={() => change.mutate('cancelled')}>取消运行</Button>}</div>{run.data?.stage_executions?.map(stage => <p key={stage.id}>{stage.stage} · {stage.status}{stage.task_id && <Link className="ml-2 underline" to={`/tasks/${stage.task_id}`}>查看结果</Link>}</p>)}<details><summary>审计记录</summary>{run.data?.audit_events?.map(event => <p key={event.id}>{event.from_state} → {event.to_state} · {event.reason_code}</p>)}</details></div>
}
