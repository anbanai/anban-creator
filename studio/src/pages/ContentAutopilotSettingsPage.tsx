import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Plus, Trash2 } from 'lucide-react'
import PageHeader from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import type { ContentAutopilotPolicy } from '@/types'

type PublishWindow = { start: string; end: string; timezone: string }
const platforms = [
  { id: 'wechat', label: '公众号' },
  { id: 'seednote', label: '小红书' },
  { id: 'hypit', label: '视频口播' },
  { id: 'montage', label: '剪辑视频' },
  { id: 'whiteboard-animation', label: '白板视频' },
]

export default function ContentAutopilotSettingsPage() {
  const client = useQueryClient()
  const policy = useQuery({ queryKey: ['content-autopilot-policy'], queryFn: () => api.contentAssets.getPolicy() })
  const projects = useQuery({ queryKey: ['projects', 'reuse'], queryFn: () => api.projects.list({ status: 'active' }) })
  const [form, setForm] = useState<ContentAutopilotPolicy | null>(null)
  useEffect(() => {
    if (!policy.data || form) return
    setForm({
      ...policy.data,
      project_ids: policy.data.project_ids ?? [],
      platforms: policy.data.platforms ?? [],
      platform_min_recommendation_scores: policy.data.platform_min_recommendation_scores ?? {},
      publish_windows: policy.data.publish_windows?.length ? policy.data.publish_windows : [{ start: '09:00', end: '21:00', timezone: 'Asia/Shanghai' }],
    })
  }, [policy.data, form])
  const save = useMutation({
    mutationFn: (value: ContentAutopilotPolicy) => api.contentAssets.updatePolicy(value),
    onSuccess: value => { setForm(value); void client.invalidateQueries({ queryKey: ['content-autopilot-policy'] }) },
  })
  const update = (patch: Partial<ContentAutopilotPolicy>) => setForm(current => current ? { ...current, ...patch } : current)
  const toggle = <T extends string>(values: T[], value: T, enabled: boolean) => enabled ? [...new Set([...values, value])] : values.filter(item => item !== value)
  const windows = form?.publish_windows ?? []

  return <div className="mx-auto w-full max-w-4xl space-y-6 pb-10">
    <PageHeader title="自动生产设置" description="为选题研究、内容生成和发布配置工作区策略。">
      <Link to="/content-assets" className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border px-3.5 text-sm font-medium transition-colors hover:bg-muted"><ArrowLeft className="size-4" />返回选题资产</Link>
    </PageHeader>
    {policy.isPending && <p role="status" className="text-sm text-muted-foreground">正在加载策略…</p>}
    {policy.isError && <div role="alert" className="flex items-center gap-3 text-sm text-destructive"><span>策略加载失败。</span><Button variant="outline" size="sm" onClick={() => void policy.refetch()}>重试</Button></div>}
    {form && <>
      <section className="space-y-4 border-b border-border pb-5">
        <div className="grid gap-4 sm:grid-cols-2">
          <label className="flex items-center justify-between gap-3 text-sm"><span>启用自动研究与生产</span><Switch checked={form.enabled} onCheckedChange={enabled => update({ enabled })} aria-label="启用自动研究与生产" /></label>
          <label className="flex items-center justify-between gap-3 text-sm"><span>视频优先</span><Switch checked={!!form.video_first} onCheckedChange={video_first => update({ video_first })} aria-label="视频优先" /></label>
          <label className="flex items-center justify-between gap-3 text-sm"><span>允许自动创建草稿</span><Switch checked={form.draft_enabled} onCheckedChange={draft_enabled => update({ draft_enabled })} aria-label="允许自动创建草稿" /></label>
          <label className="flex items-center justify-between gap-3 text-sm"><span>允许正式发布</span><Switch checked={form.publish_enabled} onCheckedChange={publish_enabled => update({ publish_enabled })} aria-label="自动发布" /></label>
          <label className="space-y-1.5 text-sm"><span>每日预算（积分）</span><Input type="number" min="0" value={form.daily_budget_credits} onChange={event => update({ daily_budget_credits: Number(event.target.value) })} /></label>
          <label className="space-y-1.5 text-sm"><span>单个选题成本上限（积分）</span><Input type="number" min="0" value={form.max_run_cost_credits} onChange={event => update({ max_run_cost_credits: Number(event.target.value) })} /></label>
          <label className="space-y-1.5 text-sm"><span>通用最低推荐分</span><Input type="number" min="0" max="100" value={form.min_recommendation_score} onChange={event => update({ min_recommendation_score: Number(event.target.value) })} /></label>
          <label className="space-y-1.5 text-sm"><span>视频最低置信度</span><Input type="number" min="0" max="100" value={form.min_video_confidence} onChange={event => update({ min_video_confidence: Number(event.target.value) })} /></label>
          <label className="space-y-1.5 text-sm"><span>合规规则版本</span><Input value={form.compliance_version ?? ''} onChange={event => update({ compliance_version: event.target.value })} /></label>
          <label className="space-y-1.5 text-sm"><span>失败处理</span><select aria-label="失败处理" className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={form.failure_mode} onChange={event => update({ failure_mode: event.target.value as ContentAutopilotPolicy['failure_mode'] })}><option value="pause">暂停运行</option><option value="skip">跳过当前运行</option></select></label>
        </div>
      </section>
      <section className="space-y-3 border-b border-border pb-5">
        <h2 className="text-sm font-semibold">自动生产人设与平台</h2>
        <div className="grid gap-5 sm:grid-cols-2">
          <fieldset className="space-y-2"><legend className="mb-2 text-sm text-muted-foreground">人设</legend>{(projects.data ?? []).map(project => <label key={project.id} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.project_ids?.includes(project.id) ?? false} onChange={event => update({ project_ids: toggle(form.project_ids ?? [], project.id, event.target.checked) })} /><span>{project.name} · {project.platform}</span></label>)}</fieldset>
          <fieldset className="space-y-2"><legend className="mb-2 text-sm text-muted-foreground">平台</legend>{platforms.map(platform => <label key={platform.id} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.platforms?.includes(platform.id) ?? false} onChange={event => update({ platforms: toggle(form.platforms ?? [], platform.id, event.target.checked) })} /><span>{platform.label}</span></label>)}</fieldset>
        </div>
        <fieldset className="grid gap-3 sm:grid-cols-2"><legend className="mb-2 text-sm text-muted-foreground">各平台最低推荐分</legend>{platforms.map(platform => <label key={platform.id} className="flex items-center justify-between gap-3 text-sm"><span>{platform.label}</span><Input aria-label={`${platform.label}最低推荐分`} type="number" min="0" max="100" className="w-24" value={form.platform_min_recommendation_scores?.[platform.id] ?? ''} onChange={event => update({ platform_min_recommendation_scores: { ...form.platform_min_recommendation_scores, [platform.id]: Number(event.target.value) } })} /></label>)}</fieldset>
      </section>
      <section className="space-y-3 border-b border-border pb-5">
        <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-sm font-semibold">允许发布时间窗口</h2><Button variant="outline" size="sm" onClick={() => update({ publish_windows: [...windows, { start: '09:00', end: '21:00', timezone: 'Asia/Shanghai' }] })}><Plus />添加窗口</Button></div>
        {windows.map((window: PublishWindow, index) => <div key={`${index}-${window.timezone}`} className="grid items-end gap-3 sm:grid-cols-[1fr_1fr_1.5fr_auto]">
          <label className="space-y-1 text-sm"><span>开始时间</span><Input aria-label={`发布时间窗口 ${index + 1}`} type="time" value={window.start} onChange={event => update({ publish_windows: windows.map((item, i) => i === index ? { ...item, start: event.target.value } : item) })} /></label>
          <label className="space-y-1 text-sm"><span>结束时间</span><Input aria-label={`结束时间窗口 ${index + 1}`} type="time" value={window.end} onChange={event => update({ publish_windows: windows.map((item, i) => i === index ? { ...item, end: event.target.value } : item) })} /></label>
          <label className="space-y-1 text-sm"><span>时区</span><Input aria-label={`时区窗口 ${index + 1}`} value={window.timezone} onChange={event => update({ publish_windows: windows.map((item, i) => i === index ? { ...item, timezone: event.target.value } : item) })} /></label>
          <Button variant="ghost" size="icon" aria-label={`移除发布时间窗口 ${index + 1}`} onClick={() => update({ publish_windows: windows.filter((_, i) => i !== index) })}><Trash2 /></Button>
        </div>)}
      </section>
      {save.isError && <p role="alert" className="text-sm text-destructive">保存失败，已保留当前填写内容。</p>}
      {save.isSuccess && <p role="status" className="text-sm text-muted-foreground">策略已保存，版本 {save.data.revision}。</p>}
      <div className="flex justify-end"><Button disabled={save.isPending || policy.isPending} onClick={() => save.mutate(form)}>保存设置</Button></div>
    </>}
  </div>
}
