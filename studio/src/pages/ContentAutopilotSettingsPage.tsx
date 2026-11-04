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
  const updateProjects = (projectIDs: string[]) => {
    const selectedPlatforms = [...new Set((projects.data ?? []).filter(project => projectIDs.includes(project.id)).map(project => project.platform))]
    update({ project_ids: projectIDs, platforms: selectedPlatforms })
  }
  const validDailyBudget = !!form && Number.isSafeInteger(form.daily_budget_credits) && form.daily_budget_credits >= 0
  const canEnable = !!form?.project_ids?.length && validDailyBudget && form.daily_budget_credits > 0
  const validRunCap = !!form && Number.isSafeInteger(form.max_run_cost_credits) && form.max_run_cost_credits >= 0
  const windows = form?.publish_windows ?? []

  return <div className="mx-auto w-full max-w-4xl space-y-6 pb-10">
    <PageHeader title="自动生产" description="选好人设和每天的预算，系统会自动寻找选题并准备内容。">
      <Link to="/topics" className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border px-3.5 text-sm font-medium transition-colors hover:bg-muted"><ArrowLeft className="size-4" />返回选题</Link>
    </PageHeader>
    {policy.isPending && <p role="status" className="text-sm text-muted-foreground">正在加载策略…</p>}
    {policy.isError && <div role="alert" className="flex items-center gap-3 text-sm text-destructive"><span>策略加载失败。</span><Button variant="outline" size="sm" onClick={() => void policy.refetch()}>重试</Button></div>}
    {form && <>
      <section className="space-y-4 border-b border-border pb-5">
        <fieldset className="space-y-2"><legend className="mb-2 text-sm font-semibold">选择要自动服务的人设</legend>{(projects.data ?? []).length === 0 ? <p className="text-sm text-muted-foreground">还没有可用人设。先创建一个人设即可开始。</p> : <div className="grid gap-2 sm:grid-cols-2">{(projects.data ?? []).map(project => { const selected = form.project_ids?.includes(project.id) ?? false; const platformLabel = platforms.find(item => item.id === project.platform)?.label ?? project.platform; return <label key={project.id} className={`flex cursor-pointer items-center justify-between gap-3 rounded-md border p-3 text-sm ${selected ? 'border-primary bg-primary/5' : 'hover:bg-muted/50'}`}><span><span className="block font-medium">{project.name}</span><span className="mt-0.5 block text-xs text-muted-foreground">{platformLabel}</span></span><input type="checkbox" checked={selected} onChange={event => updateProjects(toggle(form.project_ids ?? [], project.id, event.target.checked))} aria-label={`选择人设 ${project.name}`} /></label> })}</div>}</fieldset>
        <p className="text-xs text-muted-foreground">内容形式会按所选人设自动匹配，无需重复选择。</p>
        <label className="grid max-w-sm gap-1.5 text-sm"><span className="font-medium">每天最多花费</span><span className="text-xs text-muted-foreground">达到上限后，系统会暂停当天的自动任务。</span><span className="relative"><Input type="number" min="1" step="1" value={form.daily_budget_credits || ''} onChange={event => update({ daily_budget_credits: Number(event.target.value) })} aria-label="每天最多花费（积分）" placeholder="填写积分上限" /><span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-muted-foreground">积分</span></span></label>
        <div className="rounded-md border p-4">
          <label className="flex items-center justify-between gap-3 text-sm"><span><span className="block font-medium">先保存为草稿</span><span className="mt-1 block text-xs text-muted-foreground">完成后由你检查和修改</span></span><Switch checked={form.draft_enabled} onCheckedChange={draft_enabled => update({ draft_enabled, ...(draft_enabled ? {} : { publish_enabled: false }) })} aria-label="允许自动创建草稿" /></label>
        </div>
        <label className="flex items-center justify-between gap-4 rounded-md border p-4"><span><span className="block font-medium">开启自动生产</span><span className="mt-1 block text-sm text-muted-foreground">系统会按选中的人设持续找题并准备内容</span></span><Switch checked={form.enabled} disabled={(!canEnable || !validRunCap) && !form.enabled} onCheckedChange={enabled => update({ enabled })} aria-label="启用自动研究与生产" /></label>
        {!form.project_ids?.length && <p className="text-xs text-muted-foreground">先选择至少一个人设。</p>}
        {form.daily_budget_credits <= 0 && <p className="text-xs text-muted-foreground">填写每天的积分上限后，才能开启自动生产。</p>}
        {!validDailyBudget && <p role="alert" className="text-xs text-destructive">每天的积分上限必须是非负整数。</p>}
        {form.enabled && !canEnable && <p role="alert" className="text-xs text-destructive">补齐人设和每天的积分上限，或先关闭自动生产，再保存设置。</p>}
      </section>
      <details className="rounded-md border px-4">
        <summary className="cursor-pointer py-3 text-sm font-medium">高级设置</summary>
        <div className="space-y-5 border-t py-4">
          <section className="grid gap-4 sm:grid-cols-2">
            <label className="space-y-1.5 text-sm"><span>单次任务上限（积分）</span><Input aria-label="单次任务上限（积分）" type="number" min="0" step="1" placeholder="默认使用每天的积分上限" value={form.max_run_cost_credits || ''} onChange={event => update({ max_run_cost_credits: Number(event.target.value) })} /><span className="block text-xs text-muted-foreground">留空或填 0 时，单次最多使用当天的预算上限。</span></label>
            <label className="space-y-1.5 text-sm"><span>通用最低推荐分</span><Input type="number" min="0" max="100" value={form.min_recommendation_score} onChange={event => update({ min_recommendation_score: Number(event.target.value) })} /></label>
            <label className="space-y-1.5 text-sm"><span>视频最低置信度</span><Input type="number" min="0" max="100" value={form.min_video_confidence} onChange={event => update({ min_video_confidence: Number(event.target.value) })} /></label>
            <label className="space-y-1.5 text-sm"><span>合规规则版本</span><Input value={form.compliance_version ?? ''} onChange={event => update({ compliance_version: event.target.value })} /></label>
            <label className="space-y-1.5 text-sm"><span>失败处理</span><select aria-label="失败处理" className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={form.failure_mode} onChange={event => update({ failure_mode: event.target.value as ContentAutopilotPolicy['failure_mode'] })}><option value="pause">暂停运行</option><option value="skip">跳过当前运行</option></select></label>
            <label className="flex items-center justify-between gap-3 text-sm"><span>优先生成视频</span><Switch checked={!!form.video_first} onCheckedChange={video_first => update({ video_first })} aria-label="视频优先" /></label>
          </section>
          <section className="space-y-3 border-t pt-4">
            <h2 className="text-sm font-semibold">发布方式</h2>
            {!form.draft_enabled && <p className="text-sm text-muted-foreground">开启“先保存为草稿”后，可进一步设置自动发布。</p>}
            {form.draft_enabled && <label className="flex items-center justify-between gap-3 rounded-md border p-3 text-sm"><span><span className="block font-medium">检查通过后自动发布</span><span className="mt-1 block text-xs text-muted-foreground">开启后，符合发布条件的内容会直接发布</span></span><Switch checked={form.publish_enabled} onCheckedChange={publish_enabled => update({ publish_enabled })} aria-label="自动发布" /></label>}
          </section>
          <fieldset className="grid gap-3 sm:grid-cols-2"><legend className="mb-2 text-sm font-medium">各平台最低推荐分</legend>{platforms.map(platform => <label key={platform.id} className="flex items-center justify-between gap-3 text-sm"><span>{platform.label}</span><Input aria-label={`${platform.label}最低推荐分`} type="number" min="0" max="100" className="w-24" value={form.platform_min_recommendation_scores?.[platform.id] ?? ''} onChange={event => update({ platform_min_recommendation_scores: { ...form.platform_min_recommendation_scores, [platform.id]: Number(event.target.value) } })} /></label>)}</fieldset>
          <section className="space-y-3 border-t pt-4">
            <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-sm font-semibold">允许发布时间</h2><Button type="button" variant="outline" size="sm" onClick={() => update({ publish_windows: [...windows, { start: '09:00', end: '21:00', timezone: 'Asia/Shanghai' }] })}><Plus />添加时段</Button></div>
            {windows.map((window: PublishWindow, index) => <div key={`${index}-${window.timezone}`} className="grid items-end gap-3 sm:grid-cols-[1fr_1fr_1.5fr_auto]">
          <label className="space-y-1 text-sm"><span>开始时间</span><Input aria-label={`发布时间窗口 ${index + 1}`} type="time" value={window.start} onChange={event => update({ publish_windows: windows.map((item, i) => i === index ? { ...item, start: event.target.value } : item) })} /></label>
          <label className="space-y-1 text-sm"><span>结束时间</span><Input aria-label={`结束时间窗口 ${index + 1}`} type="time" value={window.end} onChange={event => update({ publish_windows: windows.map((item, i) => i === index ? { ...item, end: event.target.value } : item) })} /></label>
          <label className="space-y-1 text-sm"><span>时区</span><Input aria-label={`时区窗口 ${index + 1}`} value={window.timezone} onChange={event => update({ publish_windows: windows.map((item, i) => i === index ? { ...item, timezone: event.target.value } : item) })} /></label>
          <Button variant="ghost" size="icon" aria-label={`移除发布时间窗口 ${index + 1}`} onClick={() => update({ publish_windows: windows.filter((_, i) => i !== index) })}><Trash2 /></Button>
            </div>)}
          </section>
        </div>
      </details>
      {!validRunCap && <p role="alert" className="text-sm text-destructive">请在高级设置中将单次任务上限填写为非负整数。</p>}
      {save.isError && <p role="alert" className="text-sm text-destructive">保存失败，已保留当前填写内容。</p>}
      {save.isSuccess && <p role="status" className="text-sm text-muted-foreground">设置已保存。</p>}
      <div className="flex justify-end"><Button disabled={save.isPending || policy.isPending || !validDailyBudget || !validRunCap || (form.enabled && !canEnable)} onClick={() => save.mutate({ ...form, max_run_cost_credits: form.max_run_cost_credits || form.daily_budget_credits })}>保存设置</Button></div>
    </>}
  </div>
}
